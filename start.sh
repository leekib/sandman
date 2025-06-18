#!/bin/bash

# SSH 키 쌍 생성 및 설정
if [ ! -z "$USER_ID" ]; then
    echo "🔑 사용자 $USER_ID를 위한 SSH 키 생성 중..."
    
    # 사용자 생성 (이미 존재하면 무시)
    useradd -m -s /bin/bash $USER_ID 2>/dev/null || true
    
    # 사용자 홈 디렉토리 .ssh 설정
    USER_HOME="/home/$USER_ID"
    SSH_DIR="$USER_HOME/.ssh"
    mkdir -p $SSH_DIR
    chmod 700 $SSH_DIR
    
    # SSH 키 쌍 생성 (비밀번호 없이)
    ssh-keygen -t ed25519 -f "$SSH_DIR/id_ed25519" -N "" -C "$USER_ID@sandbox" >/dev/null 2>&1
    
    # 공개키를 authorized_keys에 추가
    cat "$SSH_DIR/id_ed25519.pub" > "$SSH_DIR/authorized_keys"
    chmod 600 "$SSH_DIR/authorized_keys"
    chmod 600 "$SSH_DIR/id_ed25519"
    chmod 644 "$SSH_DIR/id_ed25519.pub"
    
    # 개인키를 접근 가능한 위치에 복사 (API에서 읽을 수 있도록)
    cp "$SSH_DIR/id_ed25519" "/tmp/ssh_private_key_$USER_ID"
    chmod 644 "/tmp/ssh_private_key_$USER_ID"
    
    # 사용자 디렉토리 소유권 설정
    chown -R $USER_ID:$USER_ID $USER_HOME
    
    # sudo 권한 부여 (워크스페이스 액세스용)
    echo "$USER_ID ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/$USER_ID"
    
    echo "export USER_ID=$USER_ID" >> /etc/environment
    echo "✅ SSH 키 설정 완료: $USER_ID"
fi

# SSH 비밀번호 설정 (백업용, 키 인증 실패 시)
if [ ! -z "$SSH_PASSWORD" ]; then
    echo "root:$SSH_PASSWORD" | chpasswd
    if [ ! -z "$USER_ID" ]; then
        echo "$USER_ID:$SSH_PASSWORD" | chpasswd
    fi
fi

# GPU 정보 표시
echo "🎮 할당된 GPU 정보:" > /etc/motd
nvidia-smi -L 2>/dev/null >> /etc/motd || echo "GPU 정보를 가져올 수 없습니다." >> /etc/motd
echo "💾 워크스페이스: /workspace" >> /etc/motd
echo "🔗 네트워크: $(hostname -I)" >> /etc/motd
echo "👤 사용자: $USER_ID" >> /etc/motd
echo "" >> /etc/motd

# SSH 서버 호스트 키 생성
ssh-keygen -A

# SSH 서버 시작
exec /usr/sbin/sshd -D 