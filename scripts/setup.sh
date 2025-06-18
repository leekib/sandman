#!/bin/bash

set -e

echo "🚀 GPU SSH Gateway 시스템 설정 시작..."

# 루트 권한 확인
if [[ $EUID -ne 0 ]]; then
   echo "❌ 이 스크립트는 루트 권한으로 실행해야 합니다."
   exit 1
fi

# 필요한 디렉토리 생성
echo "📁 필요한 디렉토리 생성 중..."
mkdir -p /srv/workspaces
mkdir -p /var/lib/orchestrator
mkdir -p /etc/sshpiper

# 권한 설정
chmod 755 /srv/workspaces
chmod 700 /var/lib/orchestrator
chmod 755 /etc/sshpiper

# OS 감지 함수
detect_os() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        echo "$ID"
    elif [[ -f /etc/redhat-release ]]; then
        echo "rhel"
    elif [[ -f /etc/debian_version ]]; then
        echo "debian"
    else
        echo "unknown"
    fi
}

# Docker 설치 함수
install_docker() {
    local os_type=$(detect_os)
    echo "🐳 감지된 OS: $os_type"
    
    # 먼저 기존 Docker 상태 확인
    if command -v docker > /dev/null; then
        echo "🔍 기존 Docker 설치 확인 중..."
        
        # Docker 버전 확인
        docker_version=$(docker --version 2>/dev/null | grep -oP '\d+\.\d+\.\d+' | head -1)
        if [[ -n "$docker_version" ]]; then
            echo "📋 현재 Docker 버전: $docker_version"
            
            # 기본적인 Docker 기능 테스트
            if docker info > /dev/null 2>&1; then
                echo "✅ 기존 Docker가 정상 작동합니다. 재설치를 건너뜁니다."
                return 0
            else
                echo "⚠️ 기존 Docker에 문제가 있습니다. 재설치를 진행합니다."
            fi
        fi
    fi
    
    echo "🔧 Docker 설치/재설치를 시작합니다..."
    
    case $os_type in
        "ubuntu"|"debian")
            echo "📦 Ubuntu/Debian용 Docker 설치 중..."
            
            # 문제가 있는 경우에만 기존 패키지 제거
            if command -v docker > /dev/null && ! docker info > /dev/null 2>&1; then
                echo "🗑️ 문제가 있는 기존 Docker 패키지 제거 중..."
                apt-get remove -y docker docker-engine docker.io containerd runc 2>/dev/null || true
            fi
            
            # 필요한 패키지 설치
            apt-get update
            apt-get install -y ca-certificates curl gnupg lsb-release
            
            # Docker GPG 키 추가 (이미 있다면 건너뛰기)
            mkdir -p /etc/apt/keyrings
            if [[ ! -f /etc/apt/keyrings/docker.gpg ]]; then
                curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
            fi
            
            # Docker 저장소 추가 (이미 있다면 건너뛰기)
            if [[ ! -f /etc/apt/sources.list.d/docker.list ]]; then
                echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
            fi
            
            # Docker 설치
            apt-get update
            apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
            
        "rhel"|"centos"|"fedora"|"amzn")
            echo "📦 RHEL/CentOS/Fedora/Amazon Linux용 Docker 설치 중..."
            
            # 문제가 있는 경우에만 기존 패키지 제거
            if command -v docker > /dev/null && ! docker info > /dev/null 2>&1; then
                echo "🗑️ 문제가 있는 기존 Docker 패키지 제거 중..."
                if command -v dnf > /dev/null; then
                    dnf remove -y docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || true
                else
                    yum remove -y docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || true
                fi
            fi
            
            if command -v dnf > /dev/null; then
                dnf install -y yum-utils
                dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
                dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            else
                yum install -y yum-utils
                yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
                yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            fi
            ;;
            
        "opensuse"|"sle")
            echo "📦 OpenSUSE/SLE용 Docker 설치 중..."
            zypper install -y docker docker-compose
            ;;
            
        *)
            echo "❌ 지원되지 않는 OS입니다: $os_type"
            echo "수동으로 Docker를 설치해주세요: https://docs.docker.com/engine/install/"
            return 1
            ;;
    esac
    
    # Docker 서비스 시작 및 활성화
    systemctl start docker
    systemctl enable docker
    
    # 현재 사용자를 docker 그룹에 추가 (sudo 없이 docker 사용 가능)
    if [[ -n "$SUDO_USER" ]]; then
        usermod -aG docker "$SUDO_USER"
        echo "✅ 사용자 $SUDO_USER를 docker 그룹에 추가했습니다."
        echo "⚠️ 새 세션에서 sudo 없이 docker 명령어를 사용할 수 있습니다."
    fi
    
    echo "✅ Docker 설치 완료"
}

# Docker Compose 설치 함수
install_docker_compose() {
    echo "🐙 Docker Compose 설치 중..."
    
    # Docker Compose 플러그인이 이미 설치되어 있는지 확인
    if docker compose version > /dev/null 2>&1; then
        echo "✅ Docker Compose 플러그인이 이미 설치되어 있습니다."
        return 0
    fi
    
    # 독립형 Docker Compose 확인
    if command -v docker-compose > /dev/null; then
        echo "✅ 독립형 Docker Compose가 이미 설치되어 있습니다."
        return 0
    fi
    
    # 최신 Docker Compose 다운로드 및 설치
    DOCKER_COMPOSE_VERSION=$(curl -s https://api.github.com/repos/docker/compose/releases/latest | grep -Po '"tag_name": "\K.*?(?=")')
    echo "📦 Docker Compose $DOCKER_COMPOSE_VERSION 설치 중..."
    
    curl -L "https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    
    # 심볼릭 링크 생성 (docker compose 명령어 지원)
    ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose
    
    # Docker Compose 설치 확인 테스트
    if docker-compose --version > /dev/null 2>&1; then
        echo "✅ Docker Compose 설치 확인 완료"
        docker-compose --version
    elif docker compose version > /dev/null 2>&1; then
        echo "✅ Docker Compose 플러그인 확인 완료"
        docker compose version
    else
        echo "❌ Docker Compose 설치 확인 실패"
        return 1
    fi
    
    echo "✅ Docker Compose 설치 완료"
}

# Docker 설치 확인 및 설치
echo "🐳 Docker 설치 확인 중..."
if ! command -v docker > /dev/null; then
    echo "⚠️ Docker가 설치되지 않았습니다."
    echo "🔧 자동 설치를 시작합니다..."
    
    if install_docker; then
        echo "✅ Docker 설치 및 설정 완료"
        
        # Docker 서비스 상태 확인
        if systemctl is-active --quiet docker; then
            echo "✅ Docker 서비스가 실행 중입니다."
        else
            echo "❌ Docker 서비스가 실행되지 않습니다."
            systemctl start docker
            sleep 3
        fi
        
        # Docker 동작 테스트
        echo "🧪 Docker 설치 확인 테스트 중..."
        if test_docker_permission; then
            echo "✅ Docker 테스트 성공"
        else
            echo "❌ Docker 테스트 실패 - 권한 문제일 수 있습니다."
            echo "💡 다음 명령어로 새 세션을 시작해보세요: newgrp docker"
            exit 1
        fi
        
        # Docker Compose 설치
        install_docker_compose
    else
        echo "❌ Docker 자동 설치 실패"
        exit 1
    fi
else
    echo "✅ Docker가 이미 설치되어 있습니다."
    
    # Docker 서비스 상태 확인
    if ! systemctl is-active --quiet docker; then
        echo "🔧 Docker 서비스를 시작합니다..."
        systemctl start docker
        sleep 3
    fi
    
    # Docker Compose 설치 확인
    install_docker_compose
fi

# Docker 권한 확인 함수
test_docker_permission() {
    if sudo docker run --rm hello-world > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Docker GPU 테스트 함수
test_docker_gpu() {
    if sudo docker run --rm --gpus all nvidia/cuda:12.9.1-runtime-ubuntu24.04 nvidia-smi; then
        return 0
    else
        return 1
    fi
}

# NVIDIA Container Runtime 설치 함수
install_nvidia_container_runtime() {
    local os_type=$(detect_os)
    echo "🎮 감지된 OS: $os_type"
    
    # 먼저 기존 NVIDIA Container Toolkit 확인
    if command -v nvidia-ctk > /dev/null; then
        echo "🔍 기존 NVIDIA Container Toolkit 설치 확인 중..."
        
        # NVIDIA Container Runtime 테스트
        if test_docker_gpu; then
            echo "✅ 기존 NVIDIA Container Runtime이 정상 작동합니다. 재설치를 건너뜁니다."
            return 0
        else
            echo "⚠️ 기존 NVIDIA Container Runtime에 문제가 있습니다. 재설치를 진행합니다."
        fi
    fi
    
    echo "🔧 NVIDIA Container Toolkit 설치/재설치를 시작합니다..."
    
    case $os_type in
        "ubuntu"|"debian")
            echo "📦 Ubuntu/Debian용 NVIDIA Container Toolkit 설치 중..."
            
            # GPG 키 및 저장소 설정 (이미 있다면 건너뛰기)
            if [[ ! -f /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg ]]; then
                curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
            fi
            
            if [[ ! -f /etc/apt/sources.list.d/nvidia-container-toolkit.list ]]; then
                curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
                    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
                    tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
            fi
            
            # 패키지 목록 업데이트 및 설치
            apt-get update
            apt-get install -y nvidia-container-toolkit
            
            # Docker 설정
            nvidia-ctk runtime configure --runtime=docker
            systemctl restart docker
            ;;
            
        "rhel"|"centos"|"fedora"|"amzn")
            echo "📦 RHEL/CentOS/Fedora/Amazon Linux용 NVIDIA Container Toolkit 설치 중..."
            
            # 저장소 설정 (이미 있다면 건너뛰기)
            if [[ ! -f /etc/yum.repos.d/nvidia-container-toolkit.repo ]]; then
                curl -s -L https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo | \
                    tee /etc/yum.repos.d/nvidia-container-toolkit.repo
            fi
            
            # 설치
            if command -v dnf > /dev/null; then
                dnf install -y nvidia-container-toolkit
            else
                yum install -y nvidia-container-toolkit
            fi
            
            # Docker 설정
            nvidia-ctk runtime configure --runtime=docker
            systemctl restart docker
            ;;
            
        "opensuse"|"sle")
            echo "📦 OpenSUSE/SLE용 NVIDIA Container Toolkit 설치 중..."
            
            # 저장소 설정 및 설치
            zypper ar https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo
            zypper --gpg-auto-import-keys install -y nvidia-container-toolkit
            
            # Docker 설정
            nvidia-ctk runtime configure --runtime=docker
            systemctl restart docker
            ;;
            
        *)
            echo "❌ 지원되지 않는 OS입니다: $os_type"
            echo "수동으로 NVIDIA Container Toolkit을 설치해주세요:"
            echo "https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html"
            return 1
            ;;
    esac
    
    echo "✅ NVIDIA Container Toolkit 설치 완료"
}

# NVIDIA Container Runtime 확인 및 설치
echo "🎮 NVIDIA Container Runtime 확인 중..."
if ! command -v nvidia-smi > /dev/null; then
    echo "❌ NVIDIA 드라이버가 설치되지 않았습니다."
    echo "먼저 NVIDIA GPU 드라이버를 설치해주세요."
    exit 1
fi

if ! test_docker_gpu; then
    echo "⚠️ NVIDIA Container Runtime이 설치되지 않았거나 작동하지 않습니다."
    echo "🔧 자동 설치를 시작합니다..."
    
    if install_nvidia_container_runtime; then
        echo "✅ NVIDIA Container Runtime 설치 및 설정 완료"
        
        # 설치 후 다시 테스트
        echo "🧪 설치 확인 테스트 중..."
        sleep 5  # Docker 재시작 대기
        if test_docker_gpu; then
            echo "✅ NVIDIA Container Runtime 테스트 성공"
        else
            echo "❌ 설치 후에도 NVIDIA Container Runtime 테스트 실패"
            echo "💡 시스템을 재부팅한 후 다시 시도해보세요."
            echo "💡 또는 다음 명령어로 새 세션을 시작해보세요: newgrp docker"
            exit 1
        fi
    else
        echo "❌ NVIDIA Container Runtime 자동 설치 실패"
        exit 1
    fi
else
    echo "✅ NVIDIA Container Runtime이 이미 설치되어 있고 정상 작동합니다."
fi

# MIG 지원 확인
echo "🔍 MIG 지원 확인 중..."
if nvidia-smi --query-gpu=mig.mode.current --format=csv,noheader,nounits | grep -q "Enabled"; then
    echo "✅ MIG가 활성화되어 있습니다."
else
    echo "⚠️ MIG가 비활성화되어 있습니다."
    echo "🔧 MIG 자동 활성화를 시작합니다..."
    
    # MIG 활성화 시도
    if nvidia-smi -mig 1; then
        echo "✅ MIG 활성화 성공"
        echo "⚠️ 변경사항을 적용하려면 시스템을 재부팅해야 합니다."
        echo ""
        echo "지금 재부팅하시겠습니까? (y/N)"
        read -r response
        case "$response" in
            [yY]|[yY][eE][sS])
                echo "🔄 시스템을 재부팅합니다..."
                sleep 3
                reboot
                ;;
            *)
                echo "⏸️ 재부팅을 건너뜁니다. 나중에 'sudo reboot' 명령어로 재부팅해주세요."
                echo "재부팅 후 다시 이 스크립트를 실행하시면 MIG 설정이 완료됩니다."
                ;;
        esac
    else
        echo "❌ MIG 활성화 실패"
        echo "GPU가 MIG를 지원하지 않거나 다른 문제가 있을 수 있습니다."
        echo "수동으로 확인해보세요: nvidia-smi -mig 1"
    fi
fi

# Docker 네트워크 생성
echo "🌐 Docker 네트워크 확인 중..."
if ! docker network ls | grep -q worknet; then
    echo "worknet 네트워크 생성 중..."
    docker network create worknet --subnet 172.30.0.0/16
else
    echo "✅ worknet 네트워크가 이미 존재합니다."
fi

# GPU 워크스페이스 이미지 빌드
echo "🐳 GPU 워크스페이스 이미지 빌드 중..."
if [[ -f "Dockerfile.gpu-workspace" ]]; then
    docker build -f Dockerfile.gpu-workspace -t gpu-workspace .
    echo "✅ GPU 워크스페이스 이미지 빌드 완료"
else
    echo "⚠️ Dockerfile.gpu-workspace를 찾을 수 없습니다. 프로젝트 루트에서 실행해주세요."
fi

# 시스템 서비스 등록 (옵션)
echo "📋 시스템 서비스 설정..."
cat > /etc/systemd/system/gpu-ssh-gateway.service << EOF
[Unit]
Description=GPU SSH Gateway
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=$(pwd)
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
echo "✅ 시스템 서비스 등록 완료"

# 방화벽 설정 (옵션)
echo "🔥 방화벽 설정 확인 중..."
if command -v ufw > /dev/null; then
    ufw allow 22/tcp
    ufw allow 8080/tcp
    echo "✅ 방화벽 규칙 추가 완료"
fi

echo ""
echo "🎉 GPU SSH Gateway 설정 완료!"
echo ""
echo "다음 단계:"
echo "1. docker compose up -d"
echo "2. curl http://localhost:8080/healthz"
echo "3. API를 통해 세션 생성 후 SSH 접속 테스트"
echo ""
echo "시스템 시작: systemctl start gpu-ssh-gateway"
echo "시스템 중지: systemctl stop gpu-ssh-gateway"
echo "" 