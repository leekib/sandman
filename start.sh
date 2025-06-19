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

# 사용자 디렉토리 소유권 설정
chown -R $USER_ID:$USER_ID $USER_HOME

# sudo 권한 부여
echo "$USER_ID ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/$USER_ID"

exec /usr/sbin/sshd -D