# 🎯 GPU SSH Gateway

관리자가 API로 특정 사용자 전용 컨테이너를 생성하면, 사용자가 `ssh user123@ssh.gw` 명령으로 접속해 MIG GPU 리소스와 영구 볼륨이 자동 할당된 개별 환경에 접근할 수 있는 SSH 게이트웨이 시스템입니다.

## 🚀 주요 기능

- **GPU MIG 인스턴스 동적 할당 및 회수**
- **볼륨 마운트 및 격리된 컨테이너 생성**  
- **단일 SSH 진입점에서 여러 컨테이너로 라우팅** (SSHPiper)
- **세션 자동 종료 / 관리** (TTL)

## 📦 시스템 구성

```
┌─────────────────┐    SSH     ┌─────────────────┐
│   👤 사용자      │ ───────→  │ 🔀 SSHPiper     │
│ ssh user@ssh.gw │           │   Gateway       │
└─────────────────┘           └─────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────┐
│             🧠 Orchestrator 데몬                         │
│  • 세션 관리  • GPU/MIG 할당  • pipe.yaml 동기화        │
└─────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ 🐳 Docker Engine│    │ 📦 NVML 라이브러리│    │ 💾 Host 볼륨     │
│+ NVIDIA 런타임   │    │(GPU & MIG 정보) │    │/srv/workspaces/ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────┐
│              📦 Session 컨테이너                         │
│  • OpenSSH  • GPU/MIG 할당  • 전용 볼륨 마운트          │
└─────────────────────────────────────────────────────────┘
```

## 🛠️ 설치 및 실행

### 사전 요구사항

- Docker Engine 24.0+ with NVIDIA Container Runtime
- NVIDIA Driver 535+ with MIG 지원
- Go 1.21+ (개발 시)

### 1. 프로젝트 클론

```bash
git clone https://github.com/sandman/gpu-ssh-gateway.git
cd gpu-ssh-gateway
```

### 2. 워크스페이스 이미지 빌드

```bash
docker build -f Dockerfile.gpu-workspace -t gpu-workspace .
```

### 3. 시스템 시작

```bash
# 필요한 디렉토리 생성
sudo mkdir -p /srv/workspaces /var/lib/orchestrator /etc/sshpiper

# Docker Compose로 실행
docker-compose up -d
```

## 📖 API 사용법

### 세션 생성

```bash
curl -X POST http://localhost:8080/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "ttl_minutes": 60,
    "mig_profile": "3g.20gb"
  }'
```

**응답:**
```json
{
  "session_id": "abc-123-def",
  "container_id": "container123",
  "ssh_user": "user123",
  "ssh_host": "ssh.gw",
  "ssh_port": 22,
  "gpu_uuid": "MIG-GPU-3e9c/3/0",
  "created_at": "2025-01-17T08:00:00Z",
  "expires_at": "2025-01-17T09:00:00Z"
}
```

### SSH 접속

```bash
ssh user123@ssh.gw
```

### 세션 조회

```bash
curl http://localhost:8080/sessions/abc-123-def
```

### 세션 삭제

```bash
curl -X DELETE http://localhost:8080/sessions/abc-123-def
```

### GPU 정보 조회

```bash
curl http://localhost:8080/gpus
```

### 지원되는 MIG 프로파일 조회

```bash
curl http://localhost:8080/gpus/profiles
```

## 🎮 지원되는 MIG 프로파일

| 프로파일    | GPU 슬라이스 | 메모리    | 사용 사례           |
|----------|----------|--------|-----------------|
| `1g.5gb` | 1        | 5GB    | 가벼운 개발/테스트     |
| `2g.10gb`| 2        | 10GB   | 중간 규모 훈련       |
| `3g.20gb`| 3        | 20GB   | 일반적인 딥러닝 워크로드  |
| `4g.20gb`| 4        | 20GB   | 큰 모델 추론        |
| `7g.40gb`| 7        | 40GB   | 대형 모델 훈련/추론   |

## 🔧 설정

### 환경 변수

| 변수                   | 기본값                           | 설명              |
|----------------------|-------------------------------|-----------------|
| `--port`             | `8080`                        | API 서버 포트       |
| `--db`               | `/var/lib/orchestrator/sessions.db` | SQLite DB 경로 |
| `--piper-config`     | `/etc/sshpiper/pipe.yaml`    | SSHPiper 설정 경로  |
| `--workspace-root`   | `/srv/workspaces`             | 워크스페이스 루트 디렉토리 |

### 디렉토리 구조

```
/srv/workspaces/
├── user123/
│   ├── .bashrc
│   ├── projects/
│   └── data/
├── user456/
│   └── ...
```

## 🔒 보안 고려사항

- **컨테이너 격리**: `--cap-drop ALL` + `--security-opt no-new-privileges:true`
- **네트워크 격리**: `worknet` 외부 접근 불가, SSHPiper만 포워딩
- **GPU 제한**: `--gpus device=UUID`로 특정 MIG 인스턴스만 접근
- **호스트 보호**: 루트 볼륨 접근 제거, 사용자 마운트만 허용

## 🧹 세션 관리

### TTL 기반 자동 정리

- **기본 TTL**: 60분
- **정리 주기**: 1분마다 만료된 세션 확인
- **정리 과정**:
  1. 컨테이너 중지 및 제거
  2. MIG 인스턴스 해제
  3. SSH 라우팅 규칙 제거
  4. 데이터베이스 레코드 삭제

### 수동 정리

```bash
# 특정 세션 종료
curl -X DELETE http://localhost:8080/sessions/{session_id}

# 모든 활성 세션 조회
curl http://localhost:8080/sessions
```

## 🔍 모니터링

### 로그 확인

```bash
# Orchestrator 로그
docker logs gpu-ssh-orchestrator

# SSHPiper 로그  
docker logs sshpiper

# 특정 사용자 컨테이너 로그
docker logs user123-container
```

### GPU 사용량 모니터링

```bash
# 호스트에서
nvidia-smi

# 컨테이너 내에서
nvtop
```

## 🚨 문제 해결

### 일반적인 문제

1. **MIG 인스턴스 생성 실패**
   ```bash
   # MIG 모드 활성화 확인
   nvidia-smi -i 0 --query-gpu=mig.mode.current --format=csv
   
   # MIG 모드 활성화 (재부팅 필요)
   sudo nvidia-smi -i 0 -mig 1
   ```

2. **SSH 연결 실패**
   ```bash
   # SSHPiper 설정 확인
   cat /etc/sshpiper/pipe.yaml
   
   # 컨테이너 네트워크 확인
   docker network inspect worknet
   ```

3. **컨테이너 시작 실패**
   ```bash
   # Docker 로그 확인
   docker logs gpu-ssh-orchestrator
   
   # GPU 할당 확인
   docker run --rm --gpus all nvidia/cuda:12.2-runtime-ubuntu24.04 nvidia-smi
   ```

## 🤝 기여

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 라이선스

이 프로젝트는 MIT 라이선스 하에 배포됩니다. 자세한 내용은 `LICENSE` 파일을 참조하세요.

## 📞 지원

문제가 있거나 질문이 있으시면 GitHub Issues를 통해 문의해 주세요. 