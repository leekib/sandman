package gpu

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/sirupsen/logrus"
)

// MIGProfile MIG 프로파일 정보
type MIGProfile struct {
	Name        string `json:"name"`         // "3g.20gb", "4g.20gb", etc.
	ComputeSize int    `json:"compute_size"` // 3, 4, 7
	MemorySize  int    `json:"memory_size"`  // 20GB, 40GB
}

// MIGInstance MIG 인스턴스 정보
type MIGInstance struct {
	UUID        string     `json:"uuid"`
	Profile     MIGProfile `json:"profile"`
	InUse       bool       `json:"in_use"`
	DeviceIndex int        `json:"device_index"`
}

// GPUInfo GPU 정보
type GPUInfo struct {
	Index      int            `json:"index"`
	UUID       string         `json:"uuid"`
	Name       string         `json:"name"`
	MIGEnabled bool           `json:"mig_enabled"`
	Instances  []*MIGInstance `json:"instances"`
}

// Manager GPU 매니저
type Manager struct {
	mu        sync.RWMutex
	gpus      []*GPUInfo
	instances map[string]*MIGInstance // UUID -> MIGInstance
	log       *logrus.Entry
}

// NewManager 새 GPU 매니저 생성
func NewManager() (*Manager, error) {
	log := logrus.WithField("component", "gpu-manager")

	// NVML 초기화
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("NVML 초기화 실패: %v", nvml.ErrorString(ret))
	}

	manager := &Manager{
		instances: make(map[string]*MIGInstance),
		log:       log,
	}

	if err := manager.discoverGPUs(); err != nil {
		return nil, fmt.Errorf("GPU 탐색 실패: %v", err)
	}

	log.Infof("GPU 매니저 초기화 완료: %d개 GPU 발견", len(manager.gpus))
	return manager, nil
}

// Shutdown GPU 매니저 종료
func (m *Manager) Shutdown() {
	nvml.Shutdown()
}

// discoverGPUs GPU 탐색 및 MIG 인스턴스 수집
func (m *Manager) discoverGPUs() error {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("GPU 개수 조회 실패: %v", nvml.ErrorString(ret))
	}

	m.gpus = make([]*GPUInfo, 0, count)

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			m.log.Warnf("GPU %d 핸들 조회 실패: %v", i, nvml.ErrorString(ret))
			continue
		}

		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			m.log.Warnf("GPU %d UUID 조회 실패: %v", i, nvml.ErrorString(ret))
			continue
		}

		name, ret := device.GetName()
		if ret != nvml.SUCCESS {
			m.log.Warnf("GPU %d 이름 조회 실패: %v", i, nvml.ErrorString(ret))
			continue
		}

		// MIG 모드 확인
		migMode, _, ret := device.GetMigMode()
		if ret != nvml.SUCCESS {
			m.log.Warnf("GPU %d MIG 모드 조회 실패: %v", i, nvml.ErrorString(ret))
			migMode = nvml.DEVICE_MIG_DISABLE
		}

		gpuInfo := &GPUInfo{
			Index:      i,
			UUID:       uuid,
			Name:       name,
			MIGEnabled: migMode == nvml.DEVICE_MIG_ENABLE,
			Instances:  []*MIGInstance{},
		}

		// MIG 인스턴스 수집
		if gpuInfo.MIGEnabled {
			if err := m.discoverMIGInstances(device, gpuInfo); err != nil {
				m.log.Warnf("GPU %d MIG 인스턴스 탐색 실패: %v", i, err)
			}
		}

		m.gpus = append(m.gpus, gpuInfo)
		m.log.Infof("GPU %d 발견: %s (MIG: %v, 인스턴스: %d개)",
			i, name, gpuInfo.MIGEnabled, len(gpuInfo.Instances))
	}

	return nil
}

// discoverMIGInstances MIG 인스턴스 탐색
func (m *Manager) discoverMIGInstances(device nvml.Device, gpuInfo *GPUInfo) error {
	// 실제 구현에서는 nvidia-smi 명령어나 NVML API를 사용하여 MIG 인스턴스를 탐색
	// 여기서는 간단한 예시로 구현
	migInstances, err := m.getMIGInstancesFromCLI(gpuInfo.Index)
	if err != nil {
		return err
	}

	for _, instance := range migInstances {
		instance.DeviceIndex = gpuInfo.Index
		gpuInfo.Instances = append(gpuInfo.Instances, instance)
		m.instances[instance.UUID] = instance
	}

	return nil
}

// getMIGInstancesFromCLI CLI를 통해 MIG 인스턴스 조회
func (m *Manager) getMIGInstancesFromCLI(deviceIndex int) ([]*MIGInstance, error) {
	cmd := exec.Command("nvidia-smi", "mig", "-lgi", "-i", fmt.Sprintf("%d", deviceIndex))
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	instances := []*MIGInstance{}
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if strings.Contains(line, "MIG-GPU") {
			// 파싱 로직 (실제로는 더 정교하게 구현)
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uuid := fields[1]
				profile := MIGProfile{
					Name:        "3g.20gb", // 실제로는 파싱하여 결정
					ComputeSize: 3,
					MemorySize:  20,
				}

				instance := &MIGInstance{
					UUID:    uuid,
					Profile: profile,
					InUse:   false,
				}
				instances = append(instances, instance)
			}
		}
	}

	return instances, nil
}

// AllocateMIG MIG 인스턴스 할당
func (m *Manager) AllocateMIG(profileName string) (*MIGInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 사용 가능한 MIG 인스턴스 찾기
	for _, instance := range m.instances {
		if !instance.InUse && instance.Profile.Name == profileName {
			instance.InUse = true
			m.log.Infof("MIG 인스턴스 할당: %s (프로파일: %s)", instance.UUID, profileName)
			return instance, nil
		}
	}

	// 사용 가능한 인스턴스가 없으면 새로 생성
	instance, err := m.createMIGInstance(profileName)
	if err != nil {
		return nil, fmt.Errorf("MIG 인스턴스 생성 실패: %v", err)
	}

	instance.InUse = true
	m.instances[instance.UUID] = instance

	m.log.Infof("새 MIG 인스턴스 생성 및 할당: %s (프로파일: %s)", instance.UUID, profileName)
	return instance, nil
}

// ReleaseMIG MIG 인스턴스 해제
func (m *Manager) ReleaseMIG(uuid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[uuid]
	if !exists {
		return fmt.Errorf("MIG 인스턴스를 찾을 수 없음: %s", uuid)
	}

	instance.InUse = false
	m.log.Infof("MIG 인스턴스 해제: %s", uuid)

	return nil
}

// createMIGInstance 새 MIG 인스턴스 생성
func (m *Manager) createMIGInstance(profileName string) (*MIGInstance, error) {
	// 적절한 GPU 찾기
	var targetGPU *GPUInfo
	for _, gpu := range m.gpus {
		if gpu.MIGEnabled {
			targetGPU = gpu
			break
		}
	}

	if targetGPU == nil {
		return nil, fmt.Errorf("MIG가 활성화된 GPU를 찾을 수 없음")
	}

	// nvidia-smi를 사용하여 MIG 인스턴스 생성
	cmd := exec.Command("nvidia-smi", "mig", "-cgi",
		fmt.Sprintf("%s", profileName),
		"-i", fmt.Sprintf("%d", targetGPU.Index))

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("MIG 인스턴스 생성 명령 실패: %v", err)
	}

	// 출력에서 UUID 추출 (실제로는 더 정교하게 파싱)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "MIG-GPU") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				profile := MIGProfile{
					Name:        profileName,
					ComputeSize: 3, // 실제로는 profileName에서 파싱
					MemorySize:  20,
				}

				instance := &MIGInstance{
					UUID:        fields[1],
					Profile:     profile,
					InUse:       false,
					DeviceIndex: targetGPU.Index,
				}

				targetGPU.Instances = append(targetGPU.Instances, instance)
				return instance, nil
			}
		}
	}

	return nil, fmt.Errorf("생성된 MIG 인스턴스 UUID를 찾을 수 없음")
}

// GetGPUInfo 모든 GPU 정보 조회
func (m *Manager) GetGPUInfo() []*GPUInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*GPUInfo, len(m.gpus))
	copy(result, m.gpus)
	return result
}

// GetAvailableProfiles 사용 가능한 MIG 프로파일 목록
func (m *Manager) GetAvailableProfiles() []string {
	return []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"}
}
