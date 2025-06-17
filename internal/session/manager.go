package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sandman/gpu-orchestrator/internal/docker"
	"github.com/sandman/gpu-orchestrator/internal/gpu"
	"github.com/sandman/gpu-orchestrator/internal/sshpiper"
	"github.com/sandman/gpu-orchestrator/internal/store"
	"github.com/sirupsen/logrus"
)

// CreateRequest 세션 생성 요청
type CreateRequest struct {
	UserID       string `json:"user_id" binding:"required"`
	MIGProfile   string `json:"mig_profile" binding:"required"`
	TTLMinutes   int    `json:"ttl_minutes"`
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// SessionResponse 세션 응답
type SessionResponse struct {
	SessionID   string `json:"session_id"`
	ContainerID string `json:"container_id"`
	SSHUser     string `json:"ssh_user"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	GPUUUID     string `json:"gpu_uuid"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	Status      string `json:"status"`
}

// Manager 세션 매니저
type Manager struct {
	mu              sync.RWMutex
	store           *store.Store
	dockerClient    *docker.Client
	gpuManager      *gpu.Manager
	sshpiperManager *sshpiper.Manager
	log             *logrus.Entry
}

// NewManager 새 세션 매니저 생성
func NewManager(store *store.Store, dockerClient *docker.Client, gpuManager *gpu.Manager, sshpiperManager *sshpiper.Manager) *Manager {
	return &Manager{
		store:           store,
		dockerClient:    dockerClient,
		gpuManager:      gpuManager,
		sshpiperManager: sshpiperManager,
		log:             logrus.WithField("component", "session-manager"),
	}
}

// CreateSession 새 세션 생성
func (m *Manager) CreateSession(req *CreateRequest) (*SessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 기존 세션 확인
	existingSession, err := m.store.GetSessionByUserID(req.UserID)
	if err == nil && existingSession.Status == "running" {
		return nil, fmt.Errorf("사용자 %s의 활성 세션이 이미 존재합니다", req.UserID)
	}

	// TTL 기본값 설정
	ttl := req.TTLMinutes
	if ttl <= 0 {
		ttl = 60 // 기본 1시간
	}

	// MIG GPU 할당
	migInstance, err := m.gpuManager.AllocateMIG(req.MIGProfile)
	if err != nil {
		return nil, fmt.Errorf("MIG GPU 할당 실패: %v", err)
	}

	sessionID := uuid.New().String()
	createdAt := time.Now()

	// 세션 정보 저장 (컨테이너 생성 전)
	session := &store.Session{
		ID:        sessionID,
		UserID:    req.UserID,
		GPUUUID:   migInstance.UUID,
		CreatedAt: createdAt,
		TTL:       ttl,
		Status:    "creating",
	}

	if err := m.store.CreateSession(session); err != nil {
		// MIG 해제
		m.gpuManager.ReleaseMIG(migInstance.UUID)
		return nil, fmt.Errorf("세션 저장 실패: %v", err)
	}

	// 컨테이너 생성
	containerConfig := &docker.ContainerConfig{
		UserID:       req.UserID,
		GPUUUID:      migInstance.UUID,
		WorkspaceDir: req.WorkspaceDir,
	}

	containerInfo, err := m.dockerClient.CreateContainer(containerConfig)
	if err != nil {
		// 롤백
		m.store.DeleteSession(sessionID)
		m.gpuManager.ReleaseMIG(migInstance.UUID)
		return nil, fmt.Errorf("컨테이너 생성 실패: %v", err)
	}

	// 세션 정보 업데이트
	session.ContainerID = containerInfo.ID
	session.ContainerIP = containerInfo.IP
	session.Status = "running"

	if err := m.store.UpdateSession(session); err != nil {
		// 롤백
		m.dockerClient.RemoveContainer(containerInfo.ID)
		m.store.DeleteSession(sessionID)
		m.gpuManager.ReleaseMIG(migInstance.UUID)
		return nil, fmt.Errorf("세션 업데이트 실패: %v", err)
	}

	// SSHPiper 라우트 추가
	sshPassword := "temp-password" // 실제로는 컨테이너에서 생성된 패스워드 사용
	if err := m.sshpiperManager.AddRoute(req.UserID, containerInfo.IP, sshPassword); err != nil {
		m.log.Warnf("SSHPiper 라우트 추가 실패: %v", err)
		// SSH 라우팅 실패는 치명적이지 않으므로 계속 진행
	}

	m.log.Infof("세션 생성 완료: %s (사용자: %s, GPU: %s)", sessionID, req.UserID, migInstance.UUID)

	expiresAt := createdAt.Add(time.Duration(ttl) * time.Minute)

	return &SessionResponse{
		SessionID:   sessionID,
		ContainerID: containerInfo.ID,
		SSHUser:     req.UserID,
		SSHHost:     "ssh.gw", // 실제 SSH 게이트웨이 호스트
		SSHPort:     22,
		GPUUUID:     migInstance.UUID,
		CreatedAt:   createdAt.Format(time.RFC3339),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
		Status:      "running",
	}, nil
}

// GetSession 세션 정보 조회
func (m *Manager) GetSession(sessionID string) (*SessionResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, err := m.store.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("세션을 찾을 수 없음: %v", err)
	}

	expiresAt := session.CreatedAt.Add(time.Duration(session.TTL) * time.Minute)

	return &SessionResponse{
		SessionID:   session.ID,
		ContainerID: session.ContainerID,
		SSHUser:     session.UserID,
		SSHHost:     "ssh.gw",
		SSHPort:     22,
		GPUUUID:     session.GPUUUID,
		CreatedAt:   session.CreatedAt.Format(time.RFC3339),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
		Status:      session.Status,
	}, nil
}

// DeleteSession 세션 삭제
func (m *Manager) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := m.store.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("세션을 찾을 수 없음: %v", err)
	}

	return m.cleanupSession(session)
}

// cleanupSession 세션 정리
func (m *Manager) cleanupSession(session *store.Session) error {
	var errors []string

	// 컨테이너 정리
	if session.ContainerID != "" {
		if err := m.dockerClient.StopContainer(session.ContainerID); err != nil {
			errors = append(errors, fmt.Sprintf("컨테이너 중지 실패: %v", err))
		}
		if err := m.dockerClient.RemoveContainer(session.ContainerID); err != nil {
			errors = append(errors, fmt.Sprintf("컨테이너 삭제 실패: %v", err))
		}
	}

	// MIG GPU 해제
	if session.GPUUUID != "" {
		if err := m.gpuManager.ReleaseMIG(session.GPUUUID); err != nil {
			errors = append(errors, fmt.Sprintf("MIG GPU 해제 실패: %v", err))
		}
	}

	// SSHPiper 라우트 제거
	if err := m.sshpiperManager.RemoveRoute(session.UserID); err != nil {
		errors = append(errors, fmt.Sprintf("SSHPiper 라우트 제거 실패: %v", err))
	}

	// 데이터베이스에서 세션 삭제
	if err := m.store.DeleteSession(session.ID); err != nil {
		errors = append(errors, fmt.Sprintf("세션 DB 삭제 실패: %v", err))
	}

	m.log.Infof("세션 정리 완료: %s (사용자: %s)", session.ID, session.UserID)

	if len(errors) > 0 {
		return fmt.Errorf("세션 정리 중 오류 발생: %v", errors)
	}

	return nil
}

// GetExpiredSessions 만료된 세션들 조회
func (m *Manager) GetExpiredSessions() ([]*store.Session, error) {
	return m.store.GetExpiredSessions()
}

// CleanupExpiredSessions 만료된 세션들 정리
func (m *Manager) CleanupExpiredSessions() error {
	sessions, err := m.GetExpiredSessions()
	if err != nil {
		return fmt.Errorf("만료된 세션 조회 실패: %v", err)
	}

	if len(sessions) == 0 {
		return nil
	}

	m.log.Infof("만료된 세션 정리 시작: %d개 세션", len(sessions))

	var errors []string
	for _, session := range sessions {
		if err := m.cleanupSession(session); err != nil {
			errors = append(errors, fmt.Sprintf("세션 %s 정리 실패: %v", session.ID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("일부 세션 정리 실패: %v", errors)
	}

	m.log.Infof("만료된 세션 정리 완료: %d개 세션", len(sessions))
	return nil
}

// GetAllSessions 모든 세션 조회
func (m *Manager) GetAllSessions() ([]*SessionResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions, err := m.store.GetAllSessions()
	if err != nil {
		return nil, fmt.Errorf("세션 목록 조회 실패: %v", err)
	}

	responses := make([]*SessionResponse, len(sessions))
	for i, session := range sessions {
		expiresAt := session.CreatedAt.Add(time.Duration(session.TTL) * time.Minute)
		responses[i] = &SessionResponse{
			SessionID:   session.ID,
			ContainerID: session.ContainerID,
			SSHUser:     session.UserID,
			SSHHost:     "ssh.gw",
			SSHPort:     22,
			GPUUUID:     session.GPUUUID,
			CreatedAt:   session.CreatedAt.Format(time.RFC3339),
			ExpiresAt:   expiresAt.Format(time.RFC3339),
			Status:      session.Status,
		}
	}

	return responses, nil
}

// GetSessionStats 세션 통계 조회
func (m *Manager) GetSessionStats() (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions, err := m.store.GetAllSessions()
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"total_sessions":   len(sessions),
		"running_sessions": 0,
		"stopped_sessions": 0,
		"failed_sessions":  0,
	}

	for _, session := range sessions {
		switch session.Status {
		case "running":
			stats["running_sessions"] = stats["running_sessions"].(int) + 1
		case "stopped":
			stats["stopped_sessions"] = stats["stopped_sessions"].(int) + 1
		case "failed":
			stats["failed_sessions"] = stats["failed_sessions"].(int) + 1
		}
	}

	return stats, nil
}
