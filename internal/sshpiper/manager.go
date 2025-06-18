package sshpiper

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type PipeConfig struct {
	Pipers []PipeRule `yaml:"pipers"`
}

type PipeRule struct {
	MatchUser    string `yaml:"match_user"`
	TargetHost   string `yaml:"target_host"`
	TargetPort   int    `yaml:"target_port"`
	TargetUser   string `yaml:"target_user,omitempty"`
	NoPassword   bool   `yaml:"no_password,omitempty"`
}

type Manager struct {
	mu         sync.RWMutex
	configPath string
	rules      map[string]PipeRule // userID -> rule
}

func NewManager(configPath string) *Manager {
	return &Manager{
		configPath: configPath,
		rules:      make(map[string]PipeRule),
	}
}

func (m *Manager) AddRoute(userID, containerIP string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rule := PipeRule{
		MatchUser:  "^" + userID + "$",
		TargetHost: containerIP,
		TargetPort: 22,
		TargetUser: "root",
		NoPassword: false,
	}

	m.rules[userID] = rule
	
	if err := m.writeConfig(); err != nil {
		return fmt.Errorf("설정 파일 쓰기 실패: %v", err)
	}

	if err := m.reloadSSHPiper(); err != nil {
		return fmt.Errorf("SSHPiper 재로드 실패: %v", err)
	}

	log.Printf("🔀 SSH 라우팅 규칙 추가: %s -> %s:22", userID, containerIP)
	return nil
}

func (m *Manager) RemoveRoute(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.rules, userID)

	if err := m.writeConfig(); err != nil {
		return fmt.Errorf("설정 파일 쓰기 실패: %v", err)
	}

	if err := m.reloadSSHPiper(); err != nil {
		return fmt.Errorf("SSHPiper 재로드 실패: %v", err)
	}

	log.Printf("🔀 SSH 라우팅 규칙 제거: %s", userID)
	return nil
}

func (m *Manager) writeConfig() error {
	// 규칙들을 슬라이스로 변환
	var rules []PipeRule
	for _, rule := range m.rules {
		rules = append(rules, rule)
	}

	config := PipeConfig{
		Pipers: rules,
	}

	// YAML로 마샬링
	data, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}

	// 설정 파일 디렉토리 생성
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0755); err != nil {
		return err
	}

	// 파일 쓰기
	return os.WriteFile(m.configPath, data, 0644)
}

func (m *Manager) reloadSSHPiper() error {
	// SSHPiper 컨테이너에 SIGHUP 신호 전송
	cmd := exec.Command("docker", "exec", "sshpiper", "pkill", "-HUP", "sshpiper")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("⚠️ SSHPiper 재로드 실패: %v, 출력: %s", err, string(output))
		return err
	}

	log.Println("🔄 SSHPiper 설정 재로드 완료")
	return nil
}

func (m *Manager) GetRoutes() map[string]PipeRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 복사본 반환
	result := make(map[string]PipeRule)
	for k, v := range m.rules {
		result[k] = v
	}
	return result
} 