package sshpiper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"text/template"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// PipeConfig SSHPiper 설정
type PipeConfig struct {
	Pipers []Piper `yaml:"pipers"`
}

// Piper 개별 파이프 설정
type Piper struct {
	MatchUser      string `yaml:"match_user"`
	TargetHost     string `yaml:"target_host"`
	TargetPort     int    `yaml:"target_port"`
	TargetUser     string `yaml:"target_user,omitempty"`
	TargetPassword string `yaml:"target_password,omitempty"`
}

// SessionRoute 세션 라우팅 정보
type SessionRoute struct {
	UserID      string
	ContainerIP string
	SSHPassword string
}

// Manager SSHPiper 매니저
type Manager struct {
	configDir string
	log       *logrus.Entry
	routes    map[string]*SessionRoute // userID -> route
}

// NewManager 새 SSHPiper 매니저 생성
func NewManager(configDir string) *Manager {
	return &Manager{
		configDir: configDir,
		log:       logrus.WithField("component", "sshpiper-manager"),
		routes:    make(map[string]*SessionRoute),
	}
}

// AddRoute 세션 라우트 추가
func (m *Manager) AddRoute(userID, containerIP, sshPassword string) error {
	m.routes[userID] = &SessionRoute{
		UserID:      userID,
		ContainerIP: containerIP,
		SSHPassword: sshPassword,
	}

	return m.updateConfig()
}

// RemoveRoute 세션 라우트 제거
func (m *Manager) RemoveRoute(userID string) error {
	delete(m.routes, userID)
	return m.updateConfig()
}

// updateConfig pipe.yaml 설정 업데이트
func (m *Manager) updateConfig() error {
	// 설정 파일 경로
	configFile := filepath.Join(m.configDir, "pipe.yaml")

	// 파이프 설정 생성
	pipers := make([]Piper, 0, len(m.routes))
	for _, route := range m.routes {
		piper := Piper{
			MatchUser:      fmt.Sprintf("^%s$", route.UserID),
			TargetHost:     route.ContainerIP,
			TargetPort:     22,
			TargetUser:     "root",
			TargetPassword: route.SSHPassword,
		}
		pipers = append(pipers, piper)
	}

	config := PipeConfig{
		Pipers: pipers,
	}

	// YAML 파일 생성
	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("YAML 마샬링 실패: %v", err)
	}

	// 설정 디렉토리 생성
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("설정 디렉토리 생성 실패: %v", err)
	}

	// 파일 쓰기
	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("설정 파일 쓰기 실패: %v", err)
	}

	m.log.Infof("SSHPiper 설정 업데이트 완료: %d개 라우트", len(m.routes))

	// SSHPiper 재로드
	return m.reloadSSHPiper()
}

// reloadSSHPiper SSHPiper 프로세스에 SIGHUP 신호 전송
func (m *Manager) reloadSSHPiper() error {
	// Docker 컨테이너에서 실행 중인 SSHPiper에 신호 전송
	cmd := exec.Command("docker", "exec", "sshpiper", "pkill", "-HUP", "sshpiper")
	if err := cmd.Run(); err != nil {
		m.log.Warnf("SSHPiper 재로드 실패 (컨테이너): %v", err)

		// 로컬 프로세스 재로드 시도
		return m.reloadLocalSSHPiper()
	}

	m.log.Info("SSHPiper 재로드 완료 (컨테이너)")
	return nil
}

// reloadLocalSSHPiper 로컬 SSHPiper 프로세스 재로드
func (m *Manager) reloadLocalSSHPiper() error {
	// pgrep으로 sshpiper 프로세스 찾기
	cmd := exec.Command("pgrep", "sshpiper")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("SSHPiper 프로세스를 찾을 수 없음: %v", err)
	}

	// PID 파싱 및 SIGHUP 전송
	var pid int
	if _, err := fmt.Sscanf(string(output), "%d", &pid); err != nil {
		return fmt.Errorf("PID 파싱 실패: %v", err)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("SIGHUP 신호 전송 실패: %v", err)
	}

	m.log.Infof("SSHPiper 재로드 완료 (PID: %d)", pid)
	return nil
}

// GenerateConfig 설정 파일 템플릿 생성
func (m *Manager) GenerateConfig() error {
	configFile := filepath.Join(m.configDir, "pipe.yaml")

	// 기본 설정 템플릿
	configTemplate := `# SSHPiper 설정 파일
# GPU 컨테이너 오케스트레이터에 의해 자동 생성됨

pipers:
  # 동적으로 추가되는 세션 라우트
{{- range .Pipers }}
  - match_user: "{{ .MatchUser }}"
    target_host: "{{ .TargetHost }}"
    target_port: {{ .TargetPort }}
    target_user: "{{ .TargetUser }}"
    target_password: "{{ .TargetPassword }}"
{{- end }}
`

	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("템플릿 파싱 실패: %v", err)
	}

	// 설정 디렉토리 생성
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("설정 디렉토리 생성 실패: %v", err)
	}

	// 파일 생성
	file, err := os.Create(configFile)
	if err != nil {
		return fmt.Errorf("설정 파일 생성 실패: %v", err)
	}
	defer file.Close()

	config := PipeConfig{
		Pipers: []Piper{},
	}

	if err := tmpl.Execute(file, config); err != nil {
		return fmt.Errorf("템플릿 실행 실패: %v", err)
	}

	m.log.Infof("SSHPiper 기본 설정 파일 생성: %s", configFile)
	return nil
}

// GetRoutes 현재 라우트 목록 조회
func (m *Manager) GetRoutes() map[string]*SessionRoute {
	routes := make(map[string]*SessionRoute)
	for k, v := range m.routes {
		routes[k] = v
	}
	return routes
}

// ValidateConfig 설정 파일 유효성 검사
func (m *Manager) ValidateConfig() error {
	configFile := filepath.Join(m.configDir, "pipe.yaml")

	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("설정 파일 읽기 실패: %v", err)
	}

	var config PipeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("YAML 파싱 실패: %v", err)
	}

	// 기본 유효성 검사
	for i, piper := range config.Pipers {
		if piper.MatchUser == "" {
			return fmt.Errorf("파이프 %d: match_user가 비어있음", i)
		}
		if piper.TargetHost == "" {
			return fmt.Errorf("파이프 %d: target_host가 비어있음", i)
		}
		if piper.TargetPort <= 0 || piper.TargetPort > 65535 {
			return fmt.Errorf("파이프 %d: 잘못된 target_port: %d", i, piper.TargetPort)
		}
	}

	m.log.Infof("설정 파일 유효성 검사 통과: %d개 파이프", len(config.Pipers))
	return nil
}
