# 🎯 GPU 컨테이너 오케스트레이터

관리자가 API로 특정 사용자 전용 컨테이너를 생성하면 사용자가 `ssh user123@ssh.gw` 명령으로 접속해 해당 컨테이너에 **MIG GPU 리소스**와 **영구 볼륨**이 자동 할당되는 시스템입니다.

## 🚀 주요 기능

- **GPU MIG 인스턴스 동적 할당 및 회수**
- **볼륨 마운트 및 격리된 컨테이너 생성**
- **단일 SSH 진입점에서 여러 컨테이너로 라우팅** (SSHPiper)
- **세션 자동 종료 / 관리** (TTL)

## 🏗️ 시스템 구성

```
사용자 → SSHPiper (22/tcp) → 세션 컨테이너 (GPU+볼륨)
                ↑
        Orchestrator API (8080/tcp)
                ↓
    Docker + NVIDIA Runtime + NVML
```

## 📦 설치 및 실행

### 1. 사전 요구사항

```bash
# NVIDIA 드라이버 및 CUDA 설치
sudo apt update
sudo apt install nvidia-driver-535 nvidia-cuda-toolkit

# NVIDIA Container Toolkit 설치
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | sudo tee /etc/apt/sources.list.d/nvidia-docker.list
sudo apt update && sudo apt install -y nvidia-container-toolkit
sudo systemctl restart docker

# MIG 모드 활성화 (A100/H100에서)
sudo nvidia-smi -mig 1
sudo nvidia-smi mig -cgi 19,19,19,19,19,19,19 -C  # 7개 3g.20gb 인스턴스 생성
```

### 2. 프로젝트 빌드 및 실행

```bash
# 저장소 클론
git clone <repository>
cd gpu-orchestrator

# worknet 네트워크 생성
docker network create worknet --subnet 172.30.0.0/16

# 워크스페이스 디렉토리 생성
sudo mkdir -p /srv/workspaces
sudo chmod 755 /srv/workspaces

# Docker Compose로 실행
cd docker
docker-compose up -d
```

### 3. 사용법

#### 세션 생성 (관리자)

```bash
curl -X POST http://localhost:8080/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "mig_profile": "3g.20gb",
    "ttl_minutes": 60
  }'
```

응답:
```json
{
  "session_id": "abc-123-def",
  "container_id": "container123",
  "ssh_user": "user123",
  "ssh_host": "ssh.gw",
  "ssh_port": 22,
  "gpu_uuid": "MIG-GPU-3e9c/3/0",
  "created_at": "2024-01-01T00:00:00Z",
  "expires_at": "2024-01-01T01:00:00Z",
  "status": "running"
}
```

#### SSH 접속 (사용자)

```bash
ssh user123@ssh.gw
# 자동으로 해당 사용자의 GPU 컨테이너로 연결됩니다
# /workspace 디렉토리에 영구 볼륨이 마운트되어 있습니다
```

#### 세션 조회

```bash
# 모든 세션 조회
curl http://localhost:8080/api/v1/sessions

# 특정 세션 조회
curl http://localhost:8080/api/v1/sessions/abc-123-def

# 세션 통계
curl http://localhost:8080/api/v1/sessions/stats
```

#### GPU 정보 조회

```bash
# GPU 목록 및 MIG 인스턴스 상태
curl http://localhost:8080/api/v1/gpus

# 사용 가능한 MIG 프로파일
curl http://localhost:8080/api/v1/gpus/profiles
```

#### 세션 삭제

```bash
curl -X DELETE http://localhost:8080/api/v1/sessions/abc-123-def
```

## 🔧 설정

### 환경 변수

| 변수명 | 기본값 | 설명 |
|--------|--------|------|
| `DB_PATH` | `orchestrator.db` | SQLite 데이터베이스 경로 |
| `SSHPIPER_CONFIG_DIR` | `/etc/sshpiper` | SSHPiper 설정 디렉토리 |
| `WORKSPACE_ROOT` | `/srv/workspaces` | 사용자 워크스페이스 루트 |
| `TTL_CHECK_INTERVAL` | `1m` | TTL 체크 간격 |

### MIG 프로파일

| 프로파일 | GPU 메모리 | 컴퓨트 슬라이스 |
|----------|------------|------------------|
| `1g.5gb` | 5GB | 1/7 |
| `2g.10gb` | 10GB | 2/7 |
| `3g.20gb` | 20GB | 3/7 |
| `4g.20gb` | 20GB | 4/7 |
| `7g.40gb` | 40GB | 7/7 |

## 📊 모니터링

### Prometheus 메트릭

- http://localhost:9090 (Prometheus)
- http://localhost:3000 (Grafana, admin/admin)

### 로그

```bash
# 오케스트레이터 로그
docker logs orchestrator

# SSHPiper 로그
docker logs sshpiper

# 개별 세션 컨테이너 로그
docker logs session-user123
```

## 🔒 보안 설정

- 컨테이너 격리: `--cap-drop ALL` + `--security-opt no-new-privileges:true`
- 네트워크 격리: `worknet` 외부 접근 불가
- GPU 제한: `--gpus device=UUID`로 특정 MIG 인스턴스만 접근
- 호스트 보호: 사용자 워크스페이스만 마운트

## 🛠️ 개발

### 로컬 개발 환경

```bash
# 의존성 설치
go mod download

# 테스트
go test ./...

# 로컬 실행
go run cmd/orchestrator/main.go
```

### 프로젝트 구조

```
cmd/
  orchestrator/         # 메인 엔트리포인트
internal/
  api/                  # REST API
  session/              # 세션 관리
  docker/               # Docker 클라이언트
  gpu/                  # GPU/MIG 관리
  sshpiper/             # SSHPiper 설정
  store/                # 데이터베이스
  watcher/              # TTL 모니터링
docker/                 # Docker 설정 파일
```

## ❓ FAQ

### Q: MIG가 지원되지 않는 GPU에서도 작동하나요?
A: 네, 전체 GPU를 할당하는 방식으로 폴백됩니다.

### Q: 사용자별 리소스 제한은 어떻게 설정하나요?
A: 컨테이너 생성 시 `--memory`, `--cpus` 옵션을 추가할 수 있습니다.

### Q: 데이터는 어떻게 백업하나요?
A: `/srv/workspaces`와 SQLite DB 파일을 정기적으로 백업하세요.

## 📝 라이선스

MIT License

## 🤝 기여

이슈와 PR은 언제나 환영합니다! 