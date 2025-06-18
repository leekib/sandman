package session

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/sandman/gpu-ssh-gateway/internal/docker"
	"github.com/sandman/gpu-ssh-gateway/internal/gpu"
	"github.com/sandman/gpu-ssh-gateway/internal/sshpiper"
	"github.com/sandman/gpu-ssh-gateway/internal/store"
)

type CreateRequest struct {
	UserID          string `json:"user_id" binding:"required"`
	TTLMinutes      int    `json:"ttl_minutes"`
	MIGProfile      string `json:"mig_profile"`
	MIGInstanceUUID string `json:"mig_instance_uuid,omitempty"`
	Image           string `json:"image,omitempty"`
}

type CreateResponse struct {
	SessionID     string    `json:"session_id"`
	ContainerID   string    `json:"container_id"`
	SSHUser       string    `json:"ssh_user"`
	SSHHost       string    `json:"ssh_host"`
	SSHPort       int       `json:"ssh_port"`
	SSHPrivateKey string    `json:"ssh_private_key,omitempty"`
	GPUUUID       string    `json:"gpu_uuid"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type Service struct {
	store         store.Store
	dockerClient  *docker.Client
	gpuManager    *gpu.Manager
	piperManager  *sshpiper.Manager
	workspaceRoot string
}

func NewService(
	store store.Store,
	dockerClient *docker.Client,
	gpuManager *gpu.Manager,
	piperManager *sshpiper.Manager,
	workspaceRoot string,
) *Service {
	return &Service{
		store:         store,
		dockerClient:  dockerClient,
		gpuManager:    gpuManager,
		piperManager:  piperManager,
		workspaceRoot: workspaceRoot,
	}
}

func (s *Service) CreateSession(req CreateRequest) (*CreateResponse, error) {
	// 기존 세션 확인
	existingSession, err := s.store.GetSessionByUserID(req.UserID)
	if err == nil && existingSession != nil {
		return nil, fmt.Errorf("사용자 %s의 세션이 이미 존재합니다", req.UserID)
	}

	// 기본값 설정
	if req.TTLMinutes <= 0 {
		req.TTLMinutes = 60 // 기본 1시간
	}
	if req.MIGProfile == "" && req.MIGInstanceUUID == "" {
		req.MIGProfile = "3g.20gb" // 기본 프로파일
	}

	// GPU 할당 - UUID 지정 여부에 따라 다른 방식 사용
	var migInstance *gpu.MIGInstance
	if req.MIGInstanceUUID != "" {
		// 특정 UUID로 할당
		migInstance, err = s.gpuManager.AllocateMIGByUUID(req.MIGInstanceUUID, req.UserID)
		if err != nil {
			return nil, fmt.Errorf("지정된 GPU 인스턴스 할당 실패: %v", err)
		}
	} else {
		// 프로파일로 할당 (기존 방식)
		migInstance, err = s.gpuManager.AllocateMIG(req.MIGProfile, req.UserID)
		if err != nil {
			return nil, fmt.Errorf("GPU 할당 실패: %v", err)
		}
	}

	// 워크스페이스 디렉토리 경로
	workspaceDir := filepath.Join(s.workspaceRoot, req.UserID)

	// 컨테이너 생성
	containerConfig := docker.ContainerConfig{
		UserID:       req.UserID,
		GPUUUID:      migInstance.UUID,
		WorkspaceDir: workspaceDir,
		Image:        req.Image,
	}

	containerInfo, err := s.dockerClient.CreateContainer(containerConfig)
	if err != nil {
		// GPU 할당 롤백
		s.gpuManager.ReleaseMIG(migInstance.UUID, req.UserID)
		return nil, fmt.Errorf("컨테이너 생성 실패: %v", err)
	}

	// SSHPiper 라우팅 추가
	if err := s.piperManager.AddRoute(req.UserID, containerInfo.IP); err != nil {
		// 리소스 정리
		s.dockerClient.RemoveContainer(containerInfo.ID)
		s.gpuManager.ReleaseMIG(migInstance.UUID, req.UserID)
		return nil, fmt.Errorf("SSH 라우팅 설정 실패: %v", err)
	}

	// 세션 정보 저장
	now := time.Now()
	expiresAt := now.Add(time.Duration(req.TTLMinutes) * time.Minute)

	session := &store.Session{
		ID:          uuid.New().String(),
		UserID:      req.UserID,
		ContainerID: containerInfo.ID,
		ContainerIP: containerInfo.IP,
		GPUUUID:     migInstance.UUID,
		MIGProfile:  migInstance.Profile.Name, // 실제 할당된 프로파일 사용
		TTLMinutes:  req.TTLMinutes,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Metadata: map[string]string{
			"image":        containerInfo.Image,
			"workspace":    workspaceDir,
			"ssh_password": containerConfig.SSHPassword,
		},
	}

	if err := s.store.CreateSession(session); err != nil {
		// 리소스 정리
		s.piperManager.RemoveRoute(req.UserID)
		s.dockerClient.RemoveContainer(containerInfo.ID)
		s.gpuManager.ReleaseMIG(migInstance.UUID, req.UserID)
		return nil, fmt.Errorf("세션 저장 실패: %v", err)
	}

	log.Printf("✅ 세션 생성 완료: %s (사용자: %s, GPU: %s)", session.ID, req.UserID, migInstance.UUID)

	return &CreateResponse{
		SessionID:     session.ID,
		ContainerID:   containerInfo.ID,
		SSHUser:       req.UserID,
		SSHHost:       "ssh.gw", // 실제 환경에서는 설정 가능하게
		SSHPort:       22,
		SSHPrivateKey: containerInfo.SSHPrivateKey,
		GPUUUID:       migInstance.UUID,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
	}, nil
}

func (s *Service) GetSession(sessionID string) (*store.Session, error) {
	return s.store.GetSession(sessionID)
}

func (s *Service) GetSessionByUserID(userID string) (*store.Session, error) {
	return s.store.GetSessionByUserID(userID)
}

func (s *Service) DeleteSession(sessionID string) error {
	session, err := s.store.GetSession(sessionID)
	if err != nil {
		return err
	}

	return s.cleanupSession(session)
}

func (s *Service) DeleteSessionByUserID(userID string) error {
	session, err := s.store.GetSessionByUserID(userID)
	if err != nil {
		return err
	}

	return s.cleanupSession(session)
}

func (s *Service) cleanupSession(session *store.Session) error {
	log.Printf("🧹 세션 정리 시작: %s (사용자: %s)", session.ID, session.UserID)

	// SSH 라우팅 제거
	if err := s.piperManager.RemoveRoute(session.UserID); err != nil {
		log.Printf("⚠️ SSH 라우팅 제거 실패: %v", err)
	}

	// 컨테이너 중지 및 제거
	if err := s.dockerClient.StopContainer(session.ContainerID); err != nil {
		log.Printf("⚠️ 컨테이너 중지 실패: %v", err)
	}

	if err := s.dockerClient.RemoveContainer(session.ContainerID); err != nil {
		log.Printf("⚠️ 컨테이너 제거 실패: %v", err)
	}

	// GPU 해제
	if session.GPUUUID != "" {
		if err := s.gpuManager.ReleaseMIG(session.GPUUUID, session.UserID); err != nil {
			log.Printf("⚠️ GPU 해제 실패: %v", err)
		}
	}

	// 데이터베이스에서 제거
	if err := s.store.DeleteSession(session.ID); err != nil {
		log.Printf("⚠️ 세션 데이터베이스 제거 실패: %v", err)
		return err
	}

	log.Printf("✅ 세션 정리 완료: %s", session.ID)
	return nil
}

func (s *Service) CleanupExpiredSessions() error {
	expiredSessions, err := s.store.ListExpiredSessions()
	if err != nil {
		return err
	}

	for _, session := range expiredSessions {
		log.Printf("⏰ 만료된 세션 정리: %s (사용자: %s)", session.ID, session.UserID)
		if err := s.cleanupSession(session); err != nil {
			log.Printf("⚠️ 만료된 세션 정리 실패: %v", err)
		}
	}

	if len(expiredSessions) > 0 {
		log.Printf("✅ %d개의 만료된 세션 정리 완료", len(expiredSessions))
	}

	return nil
}

func (s *Service) ListAllSessions() ([]*store.Session, error) {
	return s.store.ListAllSessions()
}

// DeleteAllSessions deletes all active sessions
func (s *Service) DeleteAllSessions() error {
	log.Printf("🧹 모든 세션 삭제 시작...")

	// 모든 세션 조회
	sessions, err := s.store.ListAllSessions()
	if err != nil {
		return fmt.Errorf("세션 목록 조회 실패: %v", err)
	}

	if len(sessions) == 0 {
		log.Printf("삭제할 세션이 없습니다")
		return nil
	}

	// 각 세션을 순차적으로 정리
	deletedCount := 0
	failedCount := 0

	for _, session := range sessions {
		log.Printf("세션 정리 중: %s (사용자: %s)", session.ID, session.UserID)
		if err := s.cleanupSession(session); err != nil {
			log.Printf("⚠️ 세션 정리 실패: %s - %v", session.ID, err)
			failedCount++
		} else {
			deletedCount++
		}
	}

	log.Printf("✅ 모든 세션 삭제 완료 - 성공: %d, 실패: %d", deletedCount, failedCount)

	if failedCount > 0 {
		return fmt.Errorf("일부 세션 삭제 실패: %d개", failedCount)
	}

	return nil
}
