package docker

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type Client struct {
	cli         *client.Client
	portManager *PortManager
}

type PortManager struct {
	mu        sync.Mutex
	startPort int
	endPort   int
	usedPorts map[int]bool
}

type ContainerConfig struct {
	UserID        string
	GPUUUID       string
	WorkspaceDir  string
	SSHPassword   string
	SSHPrivateKey string
	Image         string
	NetworkName   string
}

type ContainerInfo struct {
	ID            string `json:"id"`
	IP            string `json:"ip"`
	Image         string `json:"image"`
	Status        string `json:"status"`
	Created       string `json:"created"`
	SSHPrivateKey string `json:"ssh_private_key"`
	SSHPort       int    `json:"ssh_port"`
}

const (
	DefaultImage       = "gpu-workspace"
	DefaultNetworkName = "sandman_worknet"
	NetworkSubnet      = "10.100.0.0/16"
	IPRangeStart       = 100 // 10.100.0.100부터 시작
	IPRangeEnd         = 254 // 10.100.0.254까지
)

func NewClient(sshPortStart, sshPortEnd int) (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Docker 클라이언트 생성 실패: %v", err)
	}

	portManager := &PortManager{
		startPort: sshPortStart,
		endPort:   sshPortEnd,
		usedPorts: make(map[int]bool),
	}

	dockerClient := &Client{
		cli:         cli,
		portManager: portManager,
	}

	// 네트워크 초기화
	if err := dockerClient.ensureNetwork(); err != nil {
		return nil, fmt.Errorf("네트워크 초기화 실패: %v", err)
	}

	log.Println("✅ Docker 클라이언트 초기화 완료")
	return dockerClient, nil
}

func (pm *PortManager) AllocatePort() (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for port := pm.startPort; port <= pm.endPort; port++ {
		if !pm.usedPorts[port] {
			pm.usedPorts[port] = true
			return port, nil
		}
	}
	return 0, fmt.Errorf("사용 가능한 포트가 없습니다")
}

func (pm *PortManager) ReleasePort(port int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.usedPorts, port)
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) ensureNetwork() error {
	ctx := context.Background()

	// 네트워크 존재 여부 확인
	networks, err := c.cli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}

	for _, net := range networks {
		if net.Name == DefaultNetworkName {
			log.Printf("🌐 기존 네트워크 사용: %s", DefaultNetworkName)
			return nil
		}
	}

	// 네트워크 생성
	_, err = c.cli.NetworkCreate(ctx, DefaultNetworkName, types.NetworkCreate{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet: NetworkSubnet,
				},
			},
		},
		Options: map[string]string{
			"com.docker.network.bridge.name": DefaultNetworkName,
		},
	})

	if err != nil {
		return fmt.Errorf("네트워크 생성 실패: %v", err)
	}

	log.Printf("🌐 새 네트워크 생성: %s (%s)", DefaultNetworkName, NetworkSubnet)
	return nil
}

func (c *Client) CreateContainer(config ContainerConfig) (*ContainerInfo, error) {
	ctx := context.Background()

	// 이미지 준비
	image := config.Image
	if image == "" {
		image = DefaultImage
	}

	if err := c.pullImageIfNotExists(ctx, image); err != nil {
		return nil, fmt.Errorf("이미지 준비 실패: %v", err)
	}

	// 워크스페이스 디렉토리 생성
	if err := c.ensureWorkspaceDir(config.WorkspaceDir); err != nil {
		return nil, fmt.Errorf("워크스페이스 디렉토리 생성 실패: %v", err)
	}

	// 사용 가능한 IP 찾기
	ip, err := c.findAvailableIP()
	if err != nil {
		return nil, fmt.Errorf("사용 가능한 IP 찾기 실패: %v", err)
	}

	// SSH 포트 할당
	sshPort, err := c.portManager.AllocatePort()
	if err != nil {
		return nil, fmt.Errorf("SSH 포트 할당 실패: %v", err)
	}

	// SSH 비밀번호 생성
	if config.SSHPassword == "" {
		config.SSHPassword = generateRandomPassword()
	}

	// 컨테이너 설정
	containerConfig := &container.Config{
		Image: image,
		Env: []string{
			"NVIDIA_VISIBLE_DEVICES=" + config.GPUUUID,
			"SSH_PASSWORD=" + config.SSHPassword,
			"USER_ID=" + config.UserID,
		},
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
		Cmd:        []string{"/start.sh"},
		WorkingDir: "/workspace",
	}

	// 호스트 설정
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: config.WorkspaceDir,
				Target: "/workspace",
			},
			{
				Type:   mount.TypeVolume,
				Source: "sandman_ssh_keys",
				Target: "/shared/ssh_keys",
			},
		},
		NetworkMode: container.NetworkMode(DefaultNetworkName),
		PortBindings: nat.PortMap{
			"22/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", sshPort),
				},
			},
		},
		Resources: container.Resources{
			DeviceRequests: []container.DeviceRequest{
				{
					Driver:       "nvidia",
					DeviceIDs:    []string{config.GPUUUID},
					Capabilities: [][]string{{"gpu"}},
				},
			},
			PidsLimit: &[]int64{100}[0],
		},
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
		AutoRemove: false, // 포트 관리를 위해 자동 제거 비활성화
		SecurityOpt: []string{
			"no-new-privileges:true",
			"apparmor:unconfined",
		},
		// CapDrop:        []string{"ALL"},
		// CapAdd:         []string{"SETUID", "SETGID", "DAC_OVERRIDE", "CHOWN"},
		ReadonlyRootfs: false,
	}

	// 네트워크 설정
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			DefaultNetworkName: {
				IPAMConfig: &network.EndpointIPAMConfig{
					IPv4Address: ip,
				},
			},
		},
	}

	// 컨테이너 생성
	containerName := fmt.Sprintf("%s-container", config.UserID)
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		c.portManager.ReleasePort(sshPort)
		return nil, fmt.Errorf("컨테이너 생성 실패: %v", err)
	}

	// 컨테이너 시작
	if err := c.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		c.portManager.ReleasePort(sshPort)
		c.cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("컨테이너 시작 실패: %v", err)
	}

	// SSH 키 추출을 위해 잠시 대기
	time.Sleep(5 * time.Second)

	// SSH 개인키 추출
	sshPrivateKey, err := c.extractSSHPrivateKey(resp.ID, config.UserID)
	if err != nil {
		log.Printf("⚠️ SSH 개인키 추출 실패: %v", err)
		sshPrivateKey = ""
	}

	log.Printf("✅ 컨테이너 생성 완료: %s (IP: %s, SSH 포트: %d)", resp.ID[:12], ip, sshPort)

	return &ContainerInfo{
		ID:            resp.ID,
		IP:            ip,
		Image:         image,
		Status:        "running",
		Created:       time.Now().Format(time.RFC3339),
		SSHPrivateKey: sshPrivateKey,
		SSHPort:       sshPort,
	}, nil
}

func (c *Client) StopContainer(containerID string) error {
	ctx := context.Background()

	timeoutSeconds := 10
	err := c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeoutSeconds})
	if err != nil {
		log.Printf("⚠️ 컨테이너 중지 실패 (강제 종료 시도): %v", err)
		// 강제 종료 시도
		return c.cli.ContainerKill(ctx, containerID, "SIGKILL")
	}

	log.Printf("🛑 컨테이너 중지됨: %s", containerID[:12])
	return nil
}

func (c *Client) RemoveContainer(containerID string) error {
	ctx := context.Background()

	// 컨테이너 정보 조회하여 포트 번호 확인
	inspect, err := c.cli.ContainerInspect(ctx, containerID)
	if err == nil {
		// 포트 바인딩에서 SSH 포트 찾아서 해제
		if inspect.HostConfig != nil && inspect.HostConfig.PortBindings != nil {
			if bindings, exists := inspect.HostConfig.PortBindings["22/tcp"]; exists && len(bindings) > 0 {
				if hostPort := bindings[0].HostPort; hostPort != "" {
					if port := parsePort(hostPort); port > 0 {
						c.portManager.ReleasePort(port)
						log.Printf("🔓 포트 해제됨: %d", port)
					}
				}
			}
		}
	}

	err = c.cli.ContainerRemove(ctx, containerID, types.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})

	if err != nil {
		return fmt.Errorf("컨테이너 제거 실패: %v", err)
	}

	log.Printf("🗑️ 컨테이너 제거됨: %s", containerID[:12])
	return nil
}

func parsePort(portStr string) int {
	if port, err := strconv.Atoi(portStr); err == nil {
		return port
	}
	return 0
}

func (c *Client) GetContainerInfo(containerID string) (*ContainerInfo, error) {
	ctx := context.Background()

	inspect, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	ip := ""
	if inspect.NetworkSettings != nil && inspect.NetworkSettings.Networks != nil {
		if netInfo, exists := inspect.NetworkSettings.Networks[DefaultNetworkName]; exists {
			ip = netInfo.IPAddress
		}
	}

	return &ContainerInfo{
		ID:      inspect.ID,
		IP:      ip,
		Image:   inspect.Config.Image,
		Status:  inspect.State.Status,
		Created: inspect.Created,
	}, nil
}

func (c *Client) pullImageIfNotExists(ctx context.Context, image string) error {
	// 이미지 존재 확인
	_, _, err := c.cli.ImageInspectWithRaw(ctx, image)
	if err == nil {
		return nil // 이미지가 이미 존재
	}

	log.Printf("📥 이미지 다운로드 중: %s", image)

	reader, err := c.cli.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Pull 진행 상황을 로그로 출력하지 않고 완료만 대기
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (c *Client) ensureWorkspaceDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	// 기본 파일들 생성
	bashrcPath := filepath.Join(path, ".bashrc")
	if _, err := os.Stat(bashrcPath); os.IsNotExist(err) {
		bashrcContent := `# GPU SSH Gateway 워크스페이스
export PS1='\[\033[01;32m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '
alias ll='ls -alF'
alias la='ls -A'
alias l='ls -CF'

# GPU 정보 표시
echo "🎮 할당된 GPU 정보:"
nvidia-smi -L 2>/dev/null || echo "GPU 정보를 가져올 수 없습니다."
echo "💾 워크스페이스: /workspace"
echo "🔗 네트워크: ` + "`" + `hostname -I` + "`" + `"
echo ""
`
		os.WriteFile(bashrcPath, []byte(bashrcContent), 0644)
	}

	return nil
}

func (c *Client) findAvailableIP() (string, error) {
	ctx := context.Background()

	// 사용 중인 IP 목록 수집
	usedIPs := make(map[string]bool)

	containers, err := c.cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		if container.NetworkSettings != nil && container.NetworkSettings.Networks != nil {
			if netInfo, exists := container.NetworkSettings.Networks[DefaultNetworkName]; exists && netInfo.IPAddress != "" {
				usedIPs[netInfo.IPAddress] = true
			}
		}
	}

	// 사용 가능한 IP 찾기
	for i := IPRangeStart; i <= IPRangeEnd; i++ {
		ip := fmt.Sprintf("10.100.0.%d", i)
		if !usedIPs[ip] {
			return ip, nil
		}
	}

	return "", fmt.Errorf("사용 가능한 IP가 없습니다")
}

func generateRandomPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (c *Client) extractSSHPrivateKey(containerID, userID string) (string, error) {
	// 공유 볼륨에서 직접 SSH 키 파일 읽기
	keyPath := fmt.Sprintf("/shared/ssh_keys/ssh_private_key_%s", userID)

	log.Printf("🔍 SSH 키 추출 시작: %s (경로: %s)", userID, keyPath)

	// 최대 30초 동안 SSH 키 파일이 생성될 때까지 기다림
	for i := 0; i < 30; i++ {
		// 파일 존재 확인
		if _, err := os.Stat(keyPath); err == nil {
			// 파일이 존재하면 읽기
			content, err := os.ReadFile(keyPath)
			if err != nil {
				log.Printf("⚠️ SSH 키 파일 읽기 실패 (시도 %d/30): %v", i+1, err)
				time.Sleep(1 * time.Second)
				continue
			}

			privateKey := string(content)
			log.Printf("🔍 SSH 키 내용 확인 (길이: %d)", len(privateKey))

			// SSH 키 정리 및 유효성 검증
			cleanedKey := cleanSSHKey(privateKey)
			if len(cleanedKey) > 0 && (strings.Contains(cleanedKey, "BEGIN OPENSSH PRIVATE KEY") ||
				strings.Contains(cleanedKey, "BEGIN RSA PRIVATE KEY") ||
				strings.Contains(cleanedKey, "BEGIN EC PRIVATE KEY")) {
				log.Printf("🔑 SSH 개인키 추출 성공: %s (길이: %d바이트)", userID, len(cleanedKey))

				// 디버깅: 키 파일은 삭제하지 않음 (재사용 가능하도록)
				// os.Remove(keyPath)

				return cleanedKey, nil
			}

			if len(privateKey) > 0 {
				log.Printf("⚠️ SSH 키 형식 불일치 (시도 %d/30): 길이=%d, 내용: %s", i+1, len(privateKey), privateKey[:min(100, len(privateKey))])
			}
		} else {
			log.Printf("⏳ SSH 키 파일 대기 중 (시도 %d/30): %s", i+1, keyPath)
		}

		time.Sleep(1 * time.Second)
	}

	log.Printf("❌ SSH 키 추출 시간 초과: %s", userID)
	return "", fmt.Errorf("SSH 키 추출 시간 초과: %s", userID)
}

// cleanSSHKey는 SSH 키에서 바이너리 데이터를 제거하고 유효한 키만 반환합니다
func cleanSSHKey(rawKey string) string {
	// 개행 문자 정규화
	cleanKey := strings.ReplaceAll(rawKey, "\r\n", "\n")
	cleanKey = strings.ReplaceAll(cleanKey, "\r", "\n")

	// ASCII가 아닌 문자 제거 (SSH 키는 ASCII 기반)
	var result strings.Builder
	for _, r := range cleanKey {
		if r <= 127 && (r >= 32 || r == '\n' || r == '\t') {
			result.WriteRune(r)
		}
	}

	cleanKey = result.String()

	// SSH 키 블록 추출
	beginIndex := strings.Index(cleanKey, "-----BEGIN")
	endIndex := strings.LastIndex(cleanKey, "-----END")

	if beginIndex != -1 && endIndex != -1 && endIndex > beginIndex {
		// SSH 키 블록만 추출
		keyBlock := cleanKey[beginIndex:]
		endMarker := strings.Index(keyBlock, "-----\n")
		if endMarker == -1 {
			endMarker = strings.Index(keyBlock, "-----")
		}
		if endMarker != -1 {
			// END 마커까지 포함하여 추출
			endMarkerEnd := strings.Index(keyBlock[endMarker:], "\n")
			if endMarkerEnd != -1 {
				keyBlock = keyBlock[:endMarker+endMarkerEnd]
			} else {
				keyBlock = keyBlock[:endMarker+5] // "-----" 길이
			}
		}

		// 마지막 정리
		keyBlock = strings.TrimSpace(keyBlock)
		if !strings.HasSuffix(keyBlock, "-----") {
			if strings.Contains(keyBlock, "OPENSSH PRIVATE KEY") {
				keyBlock += "\n-----END OPENSSH PRIVATE KEY-----"
			} else if strings.Contains(keyBlock, "RSA PRIVATE KEY") {
				keyBlock += "\n-----END RSA PRIVATE KEY-----"
			} else if strings.Contains(keyBlock, "EC PRIVATE KEY") {
				keyBlock += "\n-----END EC PRIVATE KEY-----"
			}
		}

		return keyBlock
	}

	return cleanKey
}
