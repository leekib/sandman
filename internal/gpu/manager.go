package gpu

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type MIGProfile struct {
	Name     string `json:"name"`
	Memory   string `json:"memory"`
	GPUSlice int    `json:"gpu_slice"`
	MemSlice int    `json:"mem_slice"`
}

type MIGInstance struct {
	UUID      string     `json:"uuid"`
	Profile   MIGProfile `json:"profile"`
	GPUIndex  int        `json:"gpu_index"`
	InUse     bool       `json:"in_use"`
	CreatedBy string     `json:"created_by,omitempty"`
}

type GPUInfo struct {
	Index        int            `json:"index"`
	UUID         string         `json:"uuid"`
	Name         string         `json:"name"`
	MemoryTotal  uint64         `json:"memory_total"`
	MIGEnabled   bool           `json:"mig_enabled"`
	MIGInstances []*MIGInstance `json:"mig_instances"`
}

type Manager struct {
	mu           sync.RWMutex
	gpus         []*GPUInfo
	migInstances map[string]*MIGInstance // UUID -> MIGInstance
	profiles     map[string]MIGProfile   // profile name -> MIGProfile
}

func NewManager() (*Manager, error) {
	log.Printf("🎮 GPU 매니저 초기화 시작...")

	// NVIDIA GPU가 있는지 확인
	if _, err := os.Stat("/dev/nvidia0"); os.IsNotExist(err) {
		log.Printf("⚠️  NVIDIA GPU가 감지되지 않음, GPU 기능 없이 진행")
		return &Manager{
			migInstances: make(map[string]*MIGInstance),
			profiles:     getDefaultMIGProfiles(),
		}, nil
	}

	// GPU 매니저 생성
	manager := &Manager{
		gpus:         make([]*GPUInfo, 0),
		migInstances: make(map[string]*MIGInstance),
		profiles:     getDefaultMIGProfiles(),
	}

	// 실제 MIG 인스턴스 검색
	if err := manager.discoverMIGInstances(); err != nil {
		log.Printf("⚠️ MIG 인스턴스 검색 실패: %v", err)
	}

	log.Printf("✅ GPU 매니저 초기화 완료")
	return manager, nil
}

func (m *Manager) Shutdown() {
	log.Printf("🔄 GPU 매니저 종료")
}

func (m *Manager) discoverMIGInstances() error {
	log.Printf("🔍 MIG 인스턴스 검색 중...")

	// nvidia-smi -L 명령어로 MIG 인스턴스 목록 가져오기
	cmd := exec.Command("nvidia-smi", "-L")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nvidia-smi -L 실행 실패: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "MIG") && strings.Contains(line, "UUID:") {
			// MIG 인스턴스 라인 파싱
			// 예: "  MIG 1g.10gb     Device  1: (UUID: MIG-0042c8df-65bb-5d61-beb7-655f4b4318ea)"
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				uuidPart := strings.TrimSpace(parts[len(parts)-1])
				uuid := strings.Trim(uuidPart, " ()")

				// 프로파일 이름 추출
				profileName := ""
				if strings.Contains(line, "1g.10gb") {
					profileName = "1g.10gb"
				} else if strings.Contains(line, "4g.40gb") {
					profileName = "4g.40gb"
				} else if strings.Contains(line, "3g.40gb") {
					profileName = "3g.40gb"
				} else if strings.Contains(line, "2g.20gb") {
					profileName = "2g.20gb"
				} else if strings.Contains(line, "1g.20gb") {
					profileName = "1g.20gb"
				} else if strings.Contains(line, "7g.80gb") {
					profileName = "7g.80gb"
				}

				if profileName != "" && uuid != "" {
					profile, exists := m.profiles[profileName]
					if !exists {
						// 기본 프로파일이 없으면 새로 생성
						profile = MIGProfile{
							Name:   profileName,
							Memory: strings.Replace(profileName, "g.", "gb", 1),
						}
						m.profiles[profileName] = profile
					}

					migInstance := &MIGInstance{
						UUID:     uuid,
						Profile:  profile,
						GPUIndex: 0,
						InUse:    false,
					}

					m.migInstances[uuid] = migInstance
					log.Printf("✅ MIG 인스턴스 발견: %s (%s)", uuid, profileName)
				}
			}
		}
	}

	log.Printf("📊 총 %d개의 MIG 인스턴스 발견", len(m.migInstances))
	return nil
}

func (m *Manager) ListGPUs() []*GPUInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*GPUInfo, len(m.gpus))
	copy(result, m.gpus)
	return result
}

func (m *Manager) GetGPU(index int) (*GPUInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if index < 0 || index >= len(m.gpus) {
		return nil, fmt.Errorf("GPU 인덱스 %d가 유효하지 않음", index)
	}
	return m.gpus[index], nil
}

func (m *Manager) CreateMIGInstance(gpuIndex int, profileName string) (*MIGInstance, error) {
	log.Printf("⚠️ MIG 인스턴스 생성 기능이 임시로 비활성화됨 (NVML 문제로 인해)")
	return nil, fmt.Errorf("MIG 인스턴스 생성 기능이 비활성화됨")
}

func (m *Manager) DeleteMIGInstance(instanceUUID string) error {
	log.Printf("⚠️ MIG 인스턴스 삭제 기능이 임시로 비활성화됨 (NVML 문제로 인해)")
	return fmt.Errorf("MIG 인스턴스 삭제 기능이 비활성화됨")
}

func (m *Manager) ListMIGInstances() []*MIGInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instances := make([]*MIGInstance, 0, len(m.migInstances))
	for _, instance := range m.migInstances {
		instances = append(instances, instance)
	}
	return instances
}

func (m *Manager) AllocateMIG(profileName, userID string) (*MIGInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("🎯 MIG 할당 요청: 프로파일=%s, 사용자=%s", profileName, userID)

	// 요청된 프로파일과 일치하는 사용 가능한 MIG 인스턴스 찾기
	var availableInstance *MIGInstance
	for _, instance := range m.migInstances {
		if !instance.InUse && instance.Profile.Name == profileName {
			availableInstance = instance
			break
		}
	}

	if availableInstance == nil {
		return nil, fmt.Errorf("프로파일 %s의 사용 가능한 MIG 인스턴스가 없습니다", profileName)
	}

	// 인스턴스 할당
	availableInstance.InUse = true
	availableInstance.CreatedBy = userID

	log.Printf("✅ MIG 할당 성공: UUID=%s, 프로파일=%s, 사용자=%s",
		availableInstance.UUID, profileName, userID)

	return availableInstance, nil
}

func (m *Manager) ReleaseMIG(instanceUUID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("🔓 MIG 해제 요청: UUID=%s, 사용자=%s", instanceUUID, userID)

	instance, exists := m.migInstances[instanceUUID]
	if !exists {
		return fmt.Errorf("MIG 인스턴스 %s를 찾을 수 없습니다", instanceUUID)
	}

	if !instance.InUse {
		log.Printf("⚠️ MIG 인스턴스 %s는 이미 해제된 상태입니다", instanceUUID)
		return nil
	}

	if instance.CreatedBy != userID {
		log.Printf("⚠️ MIG 인스턴스 %s는 다른 사용자(%s)가 사용 중입니다", instanceUUID, instance.CreatedBy)
	}

	// 인스턴스 해제
	instance.InUse = false
	instance.CreatedBy = ""

	log.Printf("✅ MIG 해제 완료: UUID=%s", instanceUUID)
	return nil
}

func (m *Manager) GetGPUInfo() []*GPUInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 실제 MIG 인스턴스 정보를 포함한 가짜 GPU 정보 반환
	if len(m.migInstances) == 0 {
		return []*GPUInfo{}
	}

	// GPU 0 정보 생성
	migInstances := make([]*MIGInstance, 0, len(m.migInstances))
	for _, instance := range m.migInstances {
		migInstances = append(migInstances, &MIGInstance{
			UUID:      instance.UUID,
			Profile:   instance.Profile,
			GPUIndex:  instance.GPUIndex,
			InUse:     instance.InUse,
			CreatedBy: instance.CreatedBy,
		})
	}

	gpuInfo := &GPUInfo{
		Index:        0,
		UUID:         "GPU-372cf708-4ec1-0f35-bfef-a24bae2df638",
		Name:         "NVIDIA H100 80GB HBM3",
		MemoryTotal:  85899345920, // 80GB
		MIGEnabled:   true,
		MIGInstances: migInstances,
	}

	return []*GPUInfo{gpuInfo}
}

func (m *Manager) GetAvailableProfiles() map[string]MIGProfile {
	return m.profiles
}

// GetAvailableMIGInstances 사용 가능한 MIG 인스턴스들의 목록을 인덱스와 함께 반환
func (m *Manager) GetAvailableMIGInstances() []*MIGInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	availableInstances := make([]*MIGInstance, 0)
	index := 0

	for _, instance := range m.migInstances {
		if !instance.InUse {
			// 복사본 생성하여 인덱스 추가
			instanceCopy := &MIGInstance{
				UUID:      instance.UUID,
				Profile:   instance.Profile,
				GPUIndex:  index, // 사용 가능한 인스턴스의 인덱스
				InUse:     instance.InUse,
				CreatedBy: instance.CreatedBy,
			}
			availableInstances = append(availableInstances, instanceCopy)
			index++
		}
	}

	return availableInstances
}

// AllocateMIGByUUID 특정 UUID의 MIG 인스턴스를 직접 할당
func (m *Manager) AllocateMIGByUUID(instanceUUID, userID string) (*MIGInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("🎯 MIG 할당 요청 (UUID 지정): UUID=%s, 사용자=%s", instanceUUID, userID)

	instance, exists := m.migInstances[instanceUUID]
	if !exists {
		return nil, fmt.Errorf("MIG 인스턴스 %s를 찾을 수 없습니다", instanceUUID)
	}

	if instance.InUse {
		return nil, fmt.Errorf("MIG 인스턴스 %s는 이미 사용 중입니다 (사용자: %s)", instanceUUID, instance.CreatedBy)
	}

	// 인스턴스 할당
	instance.InUse = true
	instance.CreatedBy = userID

	log.Printf("✅ MIG 할당 성공 (UUID 지정): UUID=%s, 프로파일=%s, 사용자=%s",
		instance.UUID, instance.Profile.Name, userID)

	return instance, nil
}

func getDefaultMIGProfiles() map[string]MIGProfile {
	return map[string]MIGProfile{
		"1g.5gb": {
			Name:     "1g.5gb",
			Memory:   "5gb",
			GPUSlice: 1,
			MemSlice: 1,
		},
		"1g.10gb": {
			Name:     "1g.10gb",
			Memory:   "10gb",
			GPUSlice: 1,
			MemSlice: 1,
		},
		"1g.20gb": {
			Name:     "1g.20gb",
			Memory:   "20gb",
			GPUSlice: 1,
			MemSlice: 2,
		},
		"2g.10gb": {
			Name:     "2g.10gb",
			Memory:   "10gb",
			GPUSlice: 2,
			MemSlice: 2,
		},
		"2g.20gb": {
			Name:     "2g.20gb",
			Memory:   "20gb",
			GPUSlice: 2,
			MemSlice: 4,
		},
		"3g.20gb": {
			Name:     "3g.20gb",
			Memory:   "20gb",
			GPUSlice: 3,
			MemSlice: 4,
		},
		"3g.40gb": {
			Name:     "3g.40gb",
			Memory:   "40gb",
			GPUSlice: 3,
			MemSlice: 8,
		},
		"4g.20gb": {
			Name:     "4g.20gb",
			Memory:   "20gb",
			GPUSlice: 4,
			MemSlice: 4,
		},
		"4g.40gb": {
			Name:     "4g.40gb",
			Memory:   "40gb",
			GPUSlice: 4,
			MemSlice: 8,
		},
		"7g.40gb": {
			Name:     "7g.40gb",
			Memory:   "40gb",
			GPUSlice: 7,
			MemSlice: 8,
		},
		"7g.80gb": {
			Name:     "7g.80gb",
			Memory:   "80gb",
			GPUSlice: 7,
			MemSlice: 16,
		},
	}
}
