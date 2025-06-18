package docker

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type Client struct {
	cli *client.Client
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
}

const (
	DefaultImage       = "gpu-workspace"
	DefaultNetworkName = "sandman_worknet"
	NetworkSubnet      = "10.100.0.0/16"
	IPRangeStart       = 100 // 10.100.0.100부터 시작
	IPRangeEnd         = 254 // 10.100.0.254까지
)

func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Docker 클라이언트 생성 실패: %v", err)
	}

	dockerClient := &Client{cli: cli}

	// 네트워크 초기화
	if err := dockerClient.ensureNetwork(); err != nil {
		return nil, fmt.Errorf("네트워크 초기화 실패: %v", err)
	}

	log.Println("✅ Docker 클라이언트 초기화 완료")
	return dockerClient, nil
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
		},
		NetworkMode: container.NetworkMode(DefaultNetworkName),
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
		AutoRemove: true,
		SecurityOpt: []string{
			"no-new-privileges:true",
			"apparmor:unconfined",
		},
		CapDrop:        []string{"ALL"},
		CapAdd:         []string{"SETUID", "SETGID", "DAC_OVERRIDE"},
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
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, config.UserID+"-container")
	if err != nil {
		return nil, fmt.Errorf("컨테이너 생성 실패: %v", err)
	}

	// 컨테이너 시작
	if err := c.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("컨테이너 시작 실패: %v", err)
	}

	// 컨테이너가 준비될 때까지 잠시 대기
	log.Printf("⏳ 컨테이너 시작 대기 중: %s", resp.ID[:12])
	time.Sleep(3 * time.Second)

	// SSH 키 추출
	log.Printf("🔍 SSH 키 추출 함수 호출 시작: %s", config.UserID)
	privateKey, err := c.extractSSHPrivateKey(resp.ID, config.UserID)
	if err != nil {
		log.Printf("⚠️ SSH 키 추출 실패: %v", err)
		// SSH 키 추출 실패해도 세션 생성은 계속 진행
	} else {
		log.Printf("✅ SSH 키 추출 완료: %s (길이: %d)", config.UserID, len(privateKey))
	}

	log.Printf("🐳 컨테이너 생성됨: %s (IP: %s, GPU: %s)", resp.ID[:12], ip, config.GPUUUID)

	return &ContainerInfo{
		ID:            resp.ID,
		IP:            ip,
		Image:         image,
		Status:        "created",
		Created:       time.Now().Format(time.RFC3339),
		SSHPrivateKey: privateKey,
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

	err := c.cli.ContainerRemove(ctx, containerID, types.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})

	if err != nil {
		return fmt.Errorf("컨테이너 제거 실패: %v", err)
	}

	log.Printf("🗑️ 컨테이너 제거됨: %s", containerID[:12])
	return nil
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
	ctx := context.Background()

	// 컨테이너에서 SSH 개인키 파일 읽기
	keyPath := fmt.Sprintf("/tmp/ssh_private_key_%s", userID)

	log.Printf("🔍 SSH 키 추출 시작: %s (경로: %s)", userID, keyPath)

	// 최대 30초 동안 SSH 키 생성을 기다림
	for i := 0; i < 30; i++ {
		cmd := []string{"cat", keyPath}

		execConfig := types.ExecConfig{
			Cmd:          cmd,
			AttachStdout: true,
			AttachStderr: true,
		}

		execIDResp, err := c.cli.ContainerExecCreate(ctx, containerID, execConfig)
		if err != nil {
			log.Printf("⚠️ SSH 키 추출 명령 생성 실패 (시도 %d/30): %v", i+1, err)
			time.Sleep(1 * time.Second)
			continue
		}

		resp, err := c.cli.ContainerExecAttach(ctx, execIDResp.ID, types.ExecStartCheck{})
		if err != nil {
			log.Printf("⚠️ SSH 키 추출 명령 실행 실패 (시도 %d/30): %v", i+1, err)
			time.Sleep(1 * time.Second)
			continue
		}
		defer resp.Close()

		// 출력 읽기
		output, err := io.ReadAll(resp.Reader)
		if err != nil {
			log.Printf("⚠️ SSH 키 출력 읽기 실패 (시도 %d/30): %v", i+1, err)
			time.Sleep(1 * time.Second)
			continue
		}

		privateKey := string(output)
		log.Printf("🔍 SSH 키 내용 확인 (길이: %d)", len(privateKey))

		// SSH 키 유효성 검증 - 더 포괄적인 조건
		if len(privateKey) > 0 && (strings.Contains(privateKey, "BEGIN OPENSSH PRIVATE KEY") ||
			strings.Contains(privateKey, "BEGIN RSA PRIVATE KEY") ||
			strings.Contains(privateKey, "BEGIN EC PRIVATE KEY")) {
			log.Printf("🔑 SSH 개인키 추출 성공: %s (길이: %d바이트)", userID, len(privateKey))
			return privateKey, nil
		}

		if len(privateKey) > 0 {
			log.Printf("⚠️ SSH 키 형식 불일치 (시도 %d/30): 길이=%d, 미리보기=%s", i+1, len(privateKey), privateKey[:min(50, len(privateKey))])
		}

		time.Sleep(1 * time.Second)
	}

	log.Printf("❌ SSH 키 추출 시간 초과: %s", userID)
	return "", fmt.Errorf("SSH 키 추출 시간 초과: %s", userID)
}
