FROM nvidia/cuda:12.9.1-devel-ubuntu24.04 AS builder

# Go 설치
RUN apt-get update && apt-get install -y wget
RUN wget -qO- https://go.dev/dl/go1.21.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o orchestrator ./cmd/orchestrator

FROM nvidia/cuda:12.9.1-runtime-ubuntu24.04

# 필요한 패키지 설치
RUN apt-get update && apt-get install -y \
    ca-certificates \
    docker.io \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# 빌드된 바이너리 복사
COPY --from=builder /app/orchestrator .

# 필요한 디렉토리 생성
RUN mkdir -p /var/lib/orchestrator /etc/sshpiper

EXPOSE 8080

CMD ["./orchestrator"] 