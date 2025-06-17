package docker

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/sirupsen/logrus"
)

const (
	WorknetName   = "worknet"
	WorknetSubnet = "172.30.0.0/16"
	BaseImage     = "ubuntu:24.04"
)

// ContainerConfig 컨테이너 생성 설정
type ContainerConfig struct {
	UserID       string
	GPUUUID      string
	WorkspaceDir string
	SSHPassword  string
}

// ContainerInfo 컨테이너 정보
type ContainerInfo struct {
	ID      string `json:"id"`
	IP      string `json:"ip"`
	Status  string `json:"status"`
	UserID  string `json:"user_id"`
	GPUUUID string `json:"gpu_uuid"`
}

// Client Docker 클라이언트
type Client struct {
	cli *client.Client
	log *logrus.Entry
}

// NewClient 새 Docker 클라이언트 생성
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("Docker 클라이언트 생성 실패: %v", err)
	}

	dockerClient := &Client{
		cli: cli,
		log: logrus.WithField("component", "docker-client"),
	}

	// worknet 네트워크 초기화
	if err := dockerClient.ensureWorknet(); err != nil {
		return nil, fmt.Errorf("worknet 네트워크 초기화 실패: %v", err)
	}

	dockerClient.log.Info("Docker 클라이언트 초기화 완료")
	return dockerClient, nil
}

// Close Docker 클라이언트 종료
func (c *Client) Close() error {
	return c.cli.Close()
}

// ensureWorknet worknet 네트워크 확인 및 생성
func (c *Client) ensureWorknet() error {
	ctx := context.Background()

	// 기존 네트워크 확인
	networks, err := c.cli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return err
	}

	for _, network := range networks {
		if network.Name == WorknetName {
			c.log.Infof("worknet 네트워크 이미 존재: %s", network.ID[:12])
			return nil
		}
	}

	// 새 네트워크 생성
	_, err = c.cli.NetworkCreate(ctx, WorknetName, types.NetworkCreate{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet: WorknetSubnet,
				},
			},
		},
	})

	if err != nil {
		return err
	}

	c.log.Infof("worknet 네트워크 생성 완료: %s", WorknetSubnet)
	return nil
}

// CreateContainer GPU 컨테이너 생성
func (c *Client) CreateContainer(config *ContainerConfig) (*ContainerInfo, error) {
	ctx := context.Background()

	// 워크스페이스 디렉토리 확인
	workspaceDir := fmt.Sprintf("/srv/workspaces/%s", config.UserID)
	if config.WorkspaceDir != "" {
		workspaceDir = config.WorkspaceDir
	}

	// 컨테이너 IP 할당
	containerIP := c.allocateIP()

	// SSH 패스워드 생성
	sshPassword := config.SSHPassword
	if sshPassword == "" {
		sshPassword = c.generatePassword()
	}

	// 환경 변수 설정
	env := []string{
		fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%s", config.GPUUUID),
		fmt.Sprintf("SSH_PASSWORD=%s", sshPassword),
		"DEBIAN_FRONTEND=noninteractive",
	}

	// 포트 바인딩
	exposedPorts := nat.PortSet{
		"22/tcp": struct{}{},
	}

	// 마운트 설정
	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: workspaceDir,
			Target: "/workspace",
		},
	}

	// 컨테이너 설정
	containerConfig := &container.Config{
		Image:        BaseImage,
		Env:          env,
		ExposedPorts: exposedPorts,
		WorkingDir:   "/workspace",
		Cmd: []string{
			"/bin/bash", "-c",
			c.getStartupScript(sshPassword),
		},
	}

	// 호스트 설정
	hostConfig := &container.HostConfig{
		Mounts: mounts,
		Resources: container.Resources{
			DeviceRequests: []container.DeviceRequest{
				{
					Driver:       "nvidia",
					DeviceIDs:    []string{config.GPUUUID},
					Capabilities: [][]string{{"gpu"}},
				},
			},
		},
		SecurityOpt: []string{
			"no-new-privileges:true",
		},
		CapDrop: []string{"ALL"},
		CapAdd:  []string{"SETGID", "SETUID"},
	}

	// 네트워크 설정
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			WorknetName: {
				IPAMConfig: &network.EndpointIPAMConfig{
					IPv4Address: containerIP,
				},
			},
		},
	}

	// 컨테이너 생성
	containerName := fmt.Sprintf("session-%s", config.UserID)
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("컨테이너 생성 실패: %v", err)
	}

	// 컨테이너 시작
	if err := c.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		// 실패 시 컨테이너 정리
		c.cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("컨테이너 시작 실패: %v", err)
	}

	c.log.Infof("컨테이너 생성 완료: %s (IP: %s, GPU: %s)", resp.ID[:12], containerIP, config.GPUUUID)

	return &ContainerInfo{
		ID:      resp.ID,
		IP:      containerIP,
		Status:  "running",
		UserID:  config.UserID,
		GPUUUID: config.GPUUUID,
	}, nil
}

// getStartupScript 컨테이너 시작 스크립트 생성
func (c *Client) getStartupScript(password string) string {
	return fmt.Sprintf(`
# 패키지 업데이트 및 SSH 서버 설치
apt-get update && apt-get install -y openssh-server sudo

# SSH 설정
mkdir -p /var/run/sshd
echo 'root:%s' | chpasswd
echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config
echo 'PasswordAuthentication yes' >> /etc/ssh/sshd_config

# SSH 키 생성
ssh-keygen -A

# 워크스페이스 권한 설정
chmod 755 /workspace

# SSH 데몬 시작
/usr/sbin/sshd -D
`, password)
}

// StopContainer 컨테이너 중지
func (c *Client) StopContainer(containerID string) error {
	ctx := context.Background()
	timeout := int(30) // 30초 타임아웃

	if err := c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("컨테이너 중지 실패: %v", err)
	}

	c.log.Infof("컨테이너 중지 완료: %s", containerID[:12])
	return nil
}

// RemoveContainer 컨테이너 삭제
func (c *Client) RemoveContainer(containerID string) error {
	ctx := context.Background()

	if err := c.cli.ContainerRemove(ctx, containerID, types.ContainerRemoveOptions{
		Force: true,
	}); err != nil {
		return fmt.Errorf("컨테이너 삭제 실패: %v", err)
	}

	c.log.Infof("컨테이너 삭제 완료: %s", containerID[:12])
	return nil
}

// GetContainerInfo 컨테이너 정보 조회
func (c *Client) GetContainerInfo(containerID string) (*ContainerInfo, error) {
	ctx := context.Background()

	inspection, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("컨테이너 정보 조회 실패: %v", err)
	}

	// IP 주소 추출
	var containerIP string
	if networks := inspection.NetworkSettings.Networks; networks != nil {
		if worknet, exists := networks[WorknetName]; exists {
			containerIP = worknet.IPAddress
		}
	}

	// GPU UUID 추출
	var gpuUUID string
	for _, env := range inspection.Config.Env {
		if strings.HasPrefix(env, "NVIDIA_VISIBLE_DEVICES=") {
			gpuUUID = strings.TrimPrefix(env, "NVIDIA_VISIBLE_DEVICES=")
			break
		}
	}

	return &ContainerInfo{
		ID:      inspection.ID,
		IP:      containerIP,
		Status:  inspection.State.Status,
		GPUUUID: gpuUUID,
	}, nil
}

// allocateIP 사용 가능한 IP 주소 할당
func (c *Client) allocateIP() string {
	// 간단한 IP 할당 로직 (실제로는 더 정교하게 구현)
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("172.30.%d.%d", rand.Intn(254)+1, rand.Intn(254)+1)
}

// generatePassword 랜덤 패스워드 생성
func (c *Client) generatePassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rand.Seed(time.Now().UnixNano())

	password := make([]byte, 16)
	for i := range password {
		password[i] = charset[rand.Intn(len(charset))]
	}
	return string(password)
}

// EnsureImage 필요한 이미지 확인 및 풀
func (c *Client) EnsureImage(imageName string) error {
	ctx := context.Background()

	// 이미지 존재 확인
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		c.log.Infof("이미지 이미 존재: %s", imageName)
		return nil
	}

	// 이미지 풀
	c.log.Infof("이미지 다운로드 중: %s", imageName)
	reader, err := c.cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("이미지 풀 실패: %v", err)
	}
	defer reader.Close()

	// 출력 읽기 (다운로드 진행상황)
	_, err = io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("이미지 풀 완료 실패: %v", err)
	}

	c.log.Infof("이미지 다운로드 완료: %s", imageName)
	return nil
}
