package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"golang.org/x/crypto/ssh"
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
	SSHPublicKey  string
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

	// SSH 키 쌍 생성
	publicKey, privateKey, err := c.generateSSHKeyPair(config.UserID)
	if err != nil {
		return nil, fmt.Errorf("SSH 키 생성 실패: %v", err)
	}

	log.Printf("🔑 SSH 키 쌍 생성 완료: %s", config.UserID)

	// 이미지 빌드 (공개키를 ARG로 전달)
	imageName, err := c.buildImageWithSSHKey(ctx, config.UserID, publicKey)
	if err != nil {
		return nil, fmt.Errorf("이미지 빌드 실패: %v", err)
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
		Image: imageName,
		Env: []string{
			"NVIDIA_VISIBLE_DEVICES=" + config.GPUUUID,
			"SSH_PASSWORD=" + config.SSHPassword,
			"USER_ID=" + config.UserID,
		},
		ExposedPorts: nat.PortSet{
			"22/tcp": struct{}{},
		},
		// Cmd:        []string{"/start.sh"},
		WorkingDir: "/workspace",
	}

	// 호스트 설정 (공유 볼륨 제거)
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: config.WorkspaceDir,
				Target: "/workspace",
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

	log.Printf("✅ 컨테이너 생성 완료: %s (IP: %s, SSH 포트: %d)", resp.ID[:12], ip, sshPort)

	return &ContainerInfo{
		ID:            resp.ID,
		IP:            ip,
		Image:         imageName,
		Status:        "running",
		Created:       time.Now().Format(time.RFC3339),
		SSHPrivateKey: privateKey,
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
		b[i] = charset[mathrand.Intn(len(charset))]
	}
	return string(b)
}

// generateSSHKeyPair은 SSH 키 쌍을 생성합니다
func (c *Client) generateSSHKeyPair(userID string) (string, string, error) {
	// 1. 개인키 생성
	bits := 2048
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", err
	}

	// 2. PEM 형식으로 인코딩된 개인키
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}
	privateKeyPEM := string(pem.EncodeToMemory(&privBlock))

	// 3. SSH 공개키 생성
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKey := string(ssh.MarshalAuthorizedKey(pub)) // id_rsa.pub 형태
	log.Printf("🔑 SSH 키 생성 성공: %s (공개키 길이: %d, 개인키 길이: %d)",
		userID, len(publicKey), len(privateKeyPEM))
	return publicKey, privateKeyPEM, nil
}

// buildImageWithSSHKey는 SSH 공개키를 포함한 이미지를 빌드합니다
func (c *Client) buildImageWithSSHKey(ctx context.Context, userID, publicKey string) (string, error) {
	imageName := fmt.Sprintf("gpu-workspace-%s", userID)

	log.Printf("🏗️ 사용자별 이미지 빌드 시작: %s", imageName)

	// Dockerfile 경로 확인 (컨테이너 내 마운트된 경로)
	dockerfilePath := "/app/source/Dockerfile.gpu-workspace"
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return "", fmt.Errorf("Dockerfile을 찾을 수 없습니다: %s", dockerfilePath)
	}

	// 빌드 컨텍스트 생성 (마운트된 소스 디렉토리)
	buildContext, err := c.createBuildContext("/app/source")
	if err != nil {
		return "", fmt.Errorf("빌드 컨텍스트 생성 실패: %v", err)
	}
	defer buildContext.Close()

	// 빌드 옵션 설정
	buildOptions := types.ImageBuildOptions{
		Dockerfile: "Dockerfile.gpu-workspace", // 상대 경로로 변경
		Tags:       []string{imageName},
		BuildArgs: map[string]*string{
			"USERNAME": &userID,
			"UID":      stringPtr("1001"),
			"GID":      stringPtr("1001"),
			"PUBKEY":   &publicKey,
		},
		Remove:      true,
		ForceRemove: true,
		NoCache:     false, // 캐시 사용으로 빌드 속도 향상
	}

	// 이미지 빌드
	resp, err := c.cli.ImageBuild(ctx, buildContext, buildOptions)
	if err != nil {
		return "", fmt.Errorf("이미지 빌드 실패: %v", err)
	}
	defer resp.Body.Close()

	// 빌드 로그 처리 (에러 확인)
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return "", fmt.Errorf("빌드 로그 처리 실패: %v", err)
	}

	log.Printf("✅ 사용자별 이미지 빌드 완료: %s", imageName)
	return imageName, nil
}

// createBuildContext는 빌드 컨텍스트를 tar 형식으로 생성합니다
func (c *Client) createBuildContext(contextDir string) (io.ReadCloser, error) {
	buf := bytes.NewBuffer(nil)
	tarWriter := tar.NewWriter(buf)
	defer tarWriter.Close()

	// 필요한 파일들을 tar에 추가
	files := []string{
		"Dockerfile.gpu-workspace",
		"start.sh",
	}

	for _, file := range files {
		filePath := filepath.Join(contextDir, file)
		if err := c.addFileToTar(tarWriter, filePath, file); err != nil {
			return nil, fmt.Errorf("파일 추가 실패 (%s): %v", file, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("tar 완료 실패: %v", err)
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// addFileToTar는 파일을 tar 아카이브에 추가합니다
func (c *Client) addFileToTar(tarWriter *tar.Writer, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name: name,
		Size: info.Size(),
		Mode: int64(info.Mode()),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tarWriter, file)
	return err
}

// stringPtr은 문자열 포인터를 반환하는 헬퍼 함수입니다
func stringPtr(s string) *string {
	return &s
}
