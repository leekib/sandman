# 빌드 스테이지
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Go 모듈 복사 및 의존성 다운로드
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# 바이너리 빌드
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o orchestrator ./cmd/orchestrator

# 실행 스테이지
FROM nvidia/cuda:12.0-runtime-ubuntu22.04

# 필수 패키지 설치
RUN apt-get update && apt-get install -y \
    sqlite3 \
    curl \
    docker.io \
    && rm -rf /var/lib/apt/lists/*

# NVIDIA Container Toolkit 설정
RUN curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
    && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
       sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
       tee /etc/apt/sources.list.d/nvidia-container-toolkit.list \
    && apt-get update \
    && apt-get install -y nvidia-container-toolkit

# 작업 디렉토리 생성
WORKDIR /app

# 바이너리 복사
COPY --from=builder /app/orchestrator .

# 데이터 디렉토리 생성
RUN mkdir -p /data

# 포트 노출
EXPOSE 8080

# 실행
CMD ["./orchestrator"] 