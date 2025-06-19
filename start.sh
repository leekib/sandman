#!/bin/bash

# 환경 변수 확인
if [ -z "$USER_ID" ]; then
    echo "❌ USER_ID 환경 변수가 설정되지 않았습니다."
    exit 1
fi

echo "🚀 SSH 컨테이너 초기화 시작: $USER_ID"

# 사용자 생성 및 설정
echo "👤 사용자 생성: $USER_ID"
groupadd -f $USER_ID
useradd -m -g $USER_ID -s /bin/bash $USER_ID 2>/dev/null || true

# 사용자 홈 디렉토리 SSH 설정
USER_HOME="/home/$USER_ID"
SSH_DIR="$USER_HOME/.ssh"
mkdir -p $SSH_DIR
chmod 700 $SSH_DIR

# SSH 키 쌍 생성
echo "🔑 SSH 키 쌍 생성 중..."
ssh-keygen -t ed25519 -f "$SSH_DIR/id_ed25519" -N "" -C "$USER_ID@gpu-workspace" >/dev/null 2>&1

# 공개키를 authorized_keys에 추가
cat "$SSH_DIR/id_ed25519.pub" > "$SSH_DIR/authorized_keys"
chmod 600 "$SSH_DIR/authorized_keys"
chmod 600 "$SSH_DIR/id_ed25519"
chmod 644 "$SSH_DIR/id_ed25519.pub"

# 공유 볼륨에 개인키 저장 (API에서 읽을 수 있도록)
mkdir -p "/shared/ssh_keys"
cp "$SSH_DIR/id_ed25519" "/shared/ssh_keys/ssh_private_key_$USER_ID"
chmod 644 "/shared/ssh_keys/ssh_private_key_$USER_ID"

echo "🔐 SSH 키가 공유 볼륨에 저장됨: /shared/ssh_keys/ssh_private_key_$USER_ID"

# 사용자 디렉토리 소유권 설정
chown -R $USER_ID:$USER_ID $USER_HOME

# sudo 권한 부여
echo "$USER_ID ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/$USER_ID"

# 사용자 비밀번호 설정 (SSH_PASSWORD 환경 변수 또는 기본값)
PASSWORD=${SSH_PASSWORD:-$USER_ID}
echo "$USER_ID:$PASSWORD" | chpasswd
echo "🔐 사용자 비밀번호 설정 완료"

# SSH 서버 설정
echo "🔧 SSH 서버 설정 작성 중..."
cat > /etc/ssh/sshd_config << EOF
Port 22
ListenAddress 0.0.0.0
Protocol 2

# 인증 설정
PubkeyAuthentication yes
PasswordAuthentication yes
PermitRootLogin no
AuthorizedKeysFile .ssh/authorized_keys

# 보안 설정 (컨테이너 환경에 최적화)
UsePAM no
UsePrivilegeSeparation no
ChrootDirectory none
StrictModes no
PermitEmptyPasswords no
LoginGraceTime 60
MaxStartups 10:30:100
MaxSessions 10

# 기타 설정
X11Forwarding no
PrintMotd yes
TCPKeepAlive yes
ClientAliveInterval 30
ClientAliveCountMax 3
Subsystem sftp /usr/lib/openssh/sftp-server
EOF

# MOTD 생성
echo "📝 환영 메시지 생성 중..."
cat > /etc/motd << EOF
🎮 GPU SSH Gateway 워크스페이스에 오신 것을 환영합니다!

👤 사용자: $USER_ID
💾 워크스페이스: /workspace
🔗 네트워크: $(hostname -I | awk '{print $1}')
🎯 할당된 GPU: ${NVIDIA_VISIBLE_DEVICES:-"정보 없음"}

📋 GPU 정보:
$(nvidia-smi -L 2>/dev/null || echo "GPU 정보를 가져올 수 없습니다.")

🔑 SSH 키 또는 비밀번호로 인증되었습니다.
💡 워크스페이스는 영구적으로 저장됩니다.

EOF

# SSH 호스트 키 생성
echo "🔐 SSH 호스트 키 생성 중..."
ssh-keygen -A

# SSH 설정 파일 권한 설정
chmod 644 /etc/ssh/sshd_config

# 환경 변수 등록
echo "export USER_ID=$USER_ID" >> /etc/environment

echo "✅ SSH 컨테이너 초기화 완료: $USER_ID"

# SSH 서버 시작 (포그라운드에서 실행)
echo "🚀 SSH 서버 시작 중..."
exec /usr/sbin/sshd -D -e 