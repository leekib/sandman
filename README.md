# 🎯 Sandman

관리자가 API로 특정 사용자 전용 컨테이너를 생성하면, 사용자가 `ssh user123@host:PORT` 명령으로 접속해 MIG GPU 리소스와 영구 볼륨이 자동 할당된 개별 환경에 접근할 수 있는 GPU SSH 게이트웨이 시스템입니다.

## 🚀 주요 기능

- **GPU MIG 인스턴스 동적 할당 및 회수**
- **볼륨 마운트 및 격리된 컨테이너 생성**  
- **직접 포트 바인딩을 통한 SSH 접속** (10000-20000 포트 범위)
- **세션 자동 종료 / 관리** (TTL)

## 📦 시스템 구성

```
┌─────────────────┐    SSH    ┌─────────────────┐
│      User       │ ───────→  │  Host:PORT      │
│ ssh user@host:  │           │  (10000-20000)  │
│     10001       │           │                 │
└─────────────────┘           └─────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────┐
│             🧠 Orchestrator 데몬                         │
│  • 세션 관리  • GPU/MIG 할당  • 포트 할당/해제            │
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
│  • 직접 포트 바인딩 (10000-20000)                      │
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
sudo mkdir -p /srv/workspaces /var/lib/orchestrator

# Docker Compose로 실행
docker-compose up -d
```

## 📖 API 엔드포인트

### 🌐 CORS 설정

이 API는 **모든 오리진에서의 접근을 허용**하도록 설정되어 있습니다:

- **모든 도메인**: `Access-Control-Allow-Origin: *`
- **모든 HTTP 메서드**: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`
- **모든 헤더**: 커스텀 헤더 포함 모든 요청 헤더 허용
- **인증 정보**: `credentials` 포함 요청 지원
- **Preflight 캐시**: 24시간 캐싱으로 성능 최적화

**웹 브라우저에서 직접 호출 가능:**
```javascript
// JavaScript에서 직접 API 호출 가능
fetch('http://localhost:8080/sessions', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Custom-Header': 'value'  // 커스텀 헤더도 허용
  },
  credentials: 'include',  // 쿠키/인증 정보 포함
  body: JSON.stringify({
    user_id: 'user123',
    ttl_minutes: 60
  })
})
.then(response => response.json())
.then(data => console.log(data));
```

---

### 🔍 시스템 상태

#### 헬스체크
```bash
GET /healthz
```
**응답:**
```json
{
  "status": "healthy",
  "service": "gpu-ssh-gateway-orchestrator"
}
```

---

### 👤 세션 관리

#### 1. 세션 생성
```bash
POST /sessions
Content-Type: application/json
```

**요청 본문:**
```json
{
  "user_id": "user123",          // 필수: 사용자 ID
  "ttl_minutes": 60,             // 선택: TTL (기본값: 60분)
  "mig_profile": "3g.20gb",      // 선택: MIG 프로파일 (기본값: 3g.20gb)
  "mig_instance_uuid": "...",    // 선택: 특정 MIG 인스턴스 UUID
  "image": "gpu-workspace"       // 선택: 커스텀 이미지
}
```

**응답 (201 Created):**
```json
{
  "session_id": "abc-123-def-456",
  "container_id": "container_789",
  "ssh_user": "user123",
  "ssh_host": "localhost",
  "ssh_port": 10001,
  "ssh_private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
  "gpu_uuid": "MIG-GPU-3e9c9c52/3/0",
  "created_at": "2025-01-17T08:00:00Z",
  "expires_at": "2025-01-17T09:00:00Z"
}
```

**에러 응답 예시:**
```json
{
  "error": "잘못된 요청 형식: Key: 'CreateRequest.UserID' Error:Field validation for 'UserID' failed on the 'required' tag"
}
```

**사용 예시:**
```bash
# 기본 세션 생성
curl -X POST http://localhost:8080/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "ttl_minutes": 120,
    "mig_profile": "3g.20gb"
  }'

# 특정 GPU 인스턴스로 세션 생성
curl -X POST http://localhost:8080/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user456",
    "mig_instance_uuid": "MIG-GPU-12345678/1/0"
  }'
```

#### 2. 특정 세션 조회
```bash
GET /sessions/{id}
```

**응답 (200 OK):**
```json
{
  "id": "abc-123-def-456",
  "user_id": "user123",
  "container_id": "container_789",
  "container_ip": "172.20.0.10",
  "ssh_port": 10001,
  "gpu_uuid": "MIG-GPU-3e9c9c52/3/0",
  "mig_profile": "3g.20gb",
  "ttl_minutes": 60,
  "created_at": "2025-01-17T08:00:00Z",
  "expires_at": "2025-01-17T09:00:00Z",
  "metadata": {
    "image": "gpu-workspace",
    "workspace": "/srv/workspaces/user123",
    "ssh_password": "auto-generated-password",
    "ssh_port": "10001"
  }
}
```

**에러 응답 (404 Not Found):**
```json
{
  "error": "세션을 찾을 수 없습니다: sql: no rows in result set"
}
```

**사용 예시:**
```bash
curl http://localhost:8080/sessions/abc-123-def-456
```

#### 3. 모든 세션 목록 조회
```bash
GET /sessions
```

**응답 (200 OK):**
```json
[
  {
    "id": "session-1",
    "user_id": "user123",
    "container_id": "container_789",
    "container_ip": "172.20.0.10",
    "ssh_port": 10001,
    "gpu_uuid": "MIG-GPU-3e9c9c52/3/0",
    "mig_profile": "3g.20gb",
    "ttl_minutes": 60,
    "created_at": "2025-01-17T08:00:00Z",
    "expires_at": "2025-01-17T09:00:00Z",
    "metadata": {
      "image": "gpu-workspace",
      "workspace": "/srv/workspaces/user123",
      "ssh_password": "auto-generated-password",
      "ssh_port": "10001"
    }
  }
]
```

**사용 예시:**
```bash
curl http://localhost:8080/sessions
```

#### 4. 특정 세션 삭제
```bash
DELETE /sessions/{id}
```

**응답 (200 OK):**
```json
{
  "message": "세션이 성공적으로 삭제되었습니다"
}
```

**에러 응답 (500 Internal Server Error):**
```json
{
  "error": "세션 삭제 실패: [에러 메시지]"
}
```

**사용 예시:**
```bash
curl -X DELETE http://localhost:8080/sessions/abc-123-def-456
```

#### 5. 모든 세션 삭제
```bash
DELETE /sessions
```

**응답 (200 OK):**
```json
{
  "message": "모든 세션이 성공적으로 삭제되었습니다"
}
```

**에러 응답 (500 Internal Server Error):**
```json
{
  "error": "모든 세션 삭제 실패: [에러 메시지]"
}
```

**사용 예시:**
```bash
curl -X DELETE http://localhost:8080/sessions
```

---

### 🎮 GPU 관리

#### 1. GPU 정보 조회
```bash
GET /gpus
```

**응답 (200 OK):**
```json
{
  "gpus": [
    {
      "uuid": "GPU-12345678-1234-1234-1234-123456789012",
      "name": "NVIDIA A100-SXM4-80GB",
      "memory_total": 85899345920,
      "memory_free": 85899345920,
      "memory_used": 0,
      "utilization": 0,
      "temperature": 35,
      "power_usage": 65.2,
      "power_limit": 400,
      "mig_enabled": true,
      "mig_instances": [
        {
          "uuid": "MIG-GPU-12345678/3/0",
          "profile": "3g.20gb",
          "memory": 21474836480,
          "allocated": false
        }
      ]
    }
  ],
  "count": 1
}
```

**사용 예시:**
```bash
curl http://localhost:8080/gpus
```

#### 2. 지원되는 MIG 프로파일 조회
```bash
GET /gpus/profiles
```

**응답 (200 OK):**
```json
{
  "profiles": [
    {
      "name": "1g.5gb",
      "compute_slices": 1,
      "memory_slices": 1,
      "memory_size": 5368709120,
      "description": "1/7 GPU, 5GB Memory"
    },
    {
      "name": "1g.10gb",
      "compute_slices": 1,
      "memory_slices": 2,
      "memory_size": 10737418240,
      "description": "1/7 GPU, 10GB Memory"
    },
    {
      "name": "2g.10gb",
      "compute_slices": 2,
      "memory_slices": 2,
      "memory_size": 10737418240,
      "description": "2/7 GPU, 10GB Memory"
    },
    {
      "name": "3g.20gb",
      "compute_slices": 3,
      "memory_slices": 4,
      "memory_size": 21474836480,
      "description": "3/7 GPU, 20GB Memory"
    },
    {
      "name": "4g.20gb",
      "compute_slices": 4,
      "memory_slices": 4,
      "memory_size": 21474836480,
      "description": "4/7 GPU, 20GB Memory"
    },
    {
      "name": "7g.40gb",
      "compute_slices": 7,
      "memory_slices": 8,
      "memory_size": 42949672960,
      "description": "7/7 GPU, 40GB Memory"
    }
  ]
}
```

**사용 예시:**
```bash
curl http://localhost:8080/gpus/profiles
```

#### 3. 사용 가능한 MIG 인스턴스 조회
```bash
GET /gpus/available
```

**응답 (200 OK):**
```json
{
  "available_instances": [
    {
      "uuid": "MIG-GPU-12345678/1/0",
      "profile": "1g.10gb",
      "memory": 10737418240,
      "compute_slices": 1,
      "memory_slices": 2,
      "parent_gpu": "GPU-12345678-1234-1234-1234-123456789012",
      "allocated": false
    },
    {
      "uuid": "MIG-GPU-12345678/3/0",
      "profile": "3g.20gb",
      "memory": 21474836480,
      "compute_slices": 3,
      "memory_slices": 4,
      "parent_gpu": "GPU-12345678-1234-1234-1234-123456789012",
      "allocated": false
    }
  ],
  "count": 2
}
```

**사용 예시:**
```bash
curl http://localhost:8080/gpus/available
```

---

### 📝 API 응답 코드

| 상태 코드 | 설명 |
|----------|------|
| `200 OK` | 요청 성공 |
| `201 Created` | 리소스 생성 성공 (세션 생성) |
| `400 Bad Request` | 잘못된 요청 형식 |
| `404 Not Found` | 리소스를 찾을 수 없음 |
| `500 Internal Server Error` | 서버 내부 오류 |

### 🔧 에러 응답 형식

```json
{
  "error": "에러 메시지 설명"
}
```

### 💡 API 사용 팁

1. **세션 생성 시 주의사항:**
   - `user_id`는 고유해야 하며, 기존 활성 세션이 있으면 생성 실패 (`"사용자 [user_id]의 세션이 이미 존재합니다"`)
   - `mig_profile`과 `mig_instance_uuid` 중 하나만 지정 가능
   - 사용 가능한 GPU 리소스가 없으면 생성 실패
   - 포트는 10000-20000 범위에서 자동 할당됨
   - SSH 개인키와 패스워드 모두 응답에 포함됨 (보안 목적으로 개인키 사용 권장)

2. **SSH 접속 옵션:**
   ```bash
   # 패스워드 인증 (비추천)
   ssh user123@localhost -p 10001
   
   # 키 기반 접속 (추천)
   echo "[응답의 ssh_private_key]" > /tmp/key.pem
   chmod 600 /tmp/key.pem
   ssh -i /tmp/key.pem user123@localhost -p 10001
   ```

3. **세션 모니터링:**
   ```bash
   # 주기적으로 세션 상태 확인
   watch -n 5 'curl -s http://localhost:8080/sessions | jq .'
   
   # 특정 사용자 세션 확인
   curl -s http://localhost:8080/sessions | jq '.[] | select(.user_id=="user123")'
   
   # 만료 임박 세션 확인
   curl -s http://localhost:8080/sessions | jq '.[] | select(.expires_at < (now + 300 | strftime("%Y-%m-%dT%H:%M:%SZ")))'
   ```

4. **리소스 정리:**
   ```bash
   # 특정 세션 정리
   curl -X DELETE http://localhost:8080/sessions/{session_id}
   
   # 긴급시 모든 세션 정리
   curl -X DELETE http://localhost:8080/sessions
   ```

5. **GPU 리소스 확인:**
   ```bash
   # 전체 GPU 상태
   curl -s http://localhost:8080/gpus | jq '.gpus[] | {name: .name, utilization: .utilization, memory_used: .memory_used}'
   
   # 사용 가능한 MIG 인스턴스
   curl -s http://localhost:8080/gpus/available | jq '.available_instances[] | {uuid: .uuid, profile: .profile}'
   ```

### 🚨 일반적인 에러 해결

1. **세션 생성 실패:**
   - `"사용자 [user_id]의 세션이 이미 존재합니다"`: 기존 세션을 먼저 삭제하거나 다른 user_id 사용
   - `"GPU 할당 실패"`: 사용 가능한 GPU 인스턴스 확인 (`/gpus/available`)
   - `"컨테이너 생성 실패"`: Docker 데몬 상태 및 이미지 존재 여부 확인

2. **SSH 접속 실패:**
   - 포트 접근 불가: 방화벽 설정 확인
   - 인증 실패: SSH 키 파일 권한 (600) 및 형식 확인
   - 컨테이너 미준비: 세션 생성 후 1-2분 대기

3. **리소스 부족:**
   - MIG 인스턴스 부족: 더 작은 프로파일 사용하거나 기존 세션 정리
   - 포트 부족: 기존 세션 정리 또는 포트 범위 확장

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
| `--workspace-root`   | `/srv/workspaces`             | 워크스페이스 루트 디렉토리 |
| `--ssh-port-start`   | `10000`                       | SSH 포트 범위 시작   |
| `--ssh-port-end`     | `20000`                       | SSH 포트 범위 끝    |

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
- **네트워크 격리**: `worknet` 내부 네트워크 사용
- **GPU 제한**: `--gpus device=UUID`로 특정 MIG 인스턴스만 접근
- **포트 제한**: 10000-20000 포트 범위로 SSH 접속 제한
- **호스트 보호**: 루트 볼륨 접근 제거, 사용자 마운트만 허용

## 🧹 세션 관리

### TTL 기반 자동 정리

- **기본 TTL**: 60분
- **정리 주기**: 1분마다 만료된 세션 확인
- **정리 과정**:
  1. 컨테이너 중지 및 제거
  2. MIG 인스턴스 해제
  3. SSH 포트 해제
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
   # 포트 확인
   docker ps | grep user123
   
   # 컨테이너 네트워크 확인
   docker network inspect sandman_worknet
   ```

3. **컨테이너 시작 실패**
   ```bash
   # Docker 로그 확인
   docker logs gpu-ssh-orchestrator
   
   # GPU 할당 확인
   docker run --rm --gpus all nvidia/cuda:12.2-runtime-ubuntu24.04 nvidia-smi
   ```

4. **포트 범위 부족**
   ```bash
   # 사용 중인 포트 확인
   netstat -tlnp | grep :10[0-9][0-9][0-9]
   
   # 포트 범위 확장
   docker-compose down
   # docker-compose.yml에서 포트 범위 수정
   docker-compose up -d
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