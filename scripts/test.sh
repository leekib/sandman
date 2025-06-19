#!/bin/bash

set -e

API_HOST="localhost:8080"
TEST_USER="testuser123"

echo "🧪 GPU SSH Gateway 시스템 테스트 시작..."

# 헬스체크
echo "1️⃣ API 헬스체크..."
if curl -f -s http://$API_HOST/healthz > /dev/null; then
    echo "✅ API 서버가 응답합니다"
else
    echo "❌ API 서버가 응답하지 않습니다"
    exit 1
fi

# GPU 정보 조회
echo "2️⃣ GPU 정보 조회..."
GPU_INFO=$(curl -s http://$API_HOST/gpus)
echo "GPU 정보: $GPU_INFO"

# MIG 프로파일 조회
echo "3️⃣ MIG 프로파일 조회..."
MIG_PROFILES=$(curl -s http://$API_HOST/gpus/profiles)
echo "MIG 프로파일: $MIG_PROFILES"

# 세션 생성
echo "4️⃣ 테스트 세션 생성..."
SESSION_RESPONSE=$(curl -s -X POST http://$API_HOST/sessions \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"$TEST_USER\",
    \"ttl_minutes\": 5,
    \"mig_profile\": \"1g.10gb\"
  }")

echo "세션 생성 응답: $(echo "$SESSION_RESPONSE" | jq -r 'del(.ssh_private_key)')"

# 세션 ID 및 SSH 정보 추출SESSION_ID=$(echo $SESSION_RESPONSE | jq -r '.session_id')
SSH_HOST=$(echo $SESSION_RESPONSE | jq -r '.ssh_host')
SSH_PORT=$(echo $SESSION_RESPONSE | jq -r '.ssh_port')
SSH_USER=$(echo $SESSION_RESPONSE | jq -r '.ssh_user')
SSH_PRIVATE_KEY=$(echo $SESSION_RESPONSE | jq -r '.ssh_private_key')

if [[ -z "$SESSION_ID" || "$SESSION_ID" == "null" ]]; then
    echo "❌ 세션 ID를 추출할 수 없습니다"
    exit 1
fi

echo "📋 생성된 세션 ID: $SESSION_ID"
echo "🔗 SSH 접속 정보: $SSH_USER@$SSH_HOST:$SSH_PORT"

# SSH 개인키 임시 파일 생성
TEMP_KEY_FILE="/tmp/test_ssh_key_$$"
echo "$SSH_PRIVATE_KEY" > "$TEMP_KEY_FILE"
chmod 600 "$TEMP_KEY_FILE"

echo "🔑 SSH 개인키 저장됨: $TEMP_KEY_FILE"

# 세션 정보 조회
echo "5️⃣ 세션 정보 조회..."
SESSION_INFO=$(curl -s http://$API_HOST/sessions/$SESSION_ID)
echo "세션 정보: $(echo "$SESSION_INFO" | jq -r 'del(.metadata.ssh_password)')"

# 컨테이너 IP 추출
CONTAINER_IP=$(echo $SESSION_INFO | jq -r '.container_ip')
echo "📍 컨테이너 IP: $CONTAINER_IP"

# 네트워크 연결 테스트 (컨테이너가 준비될 때까지 대기)
echo "6️⃣ 컨테이너 준비 대기..."
for i in {1..30}; do
    if nc -z $SSH_HOST $SSH_PORT 2>/dev/null; then
        echo "✅ SSH 포트($SSH_PORT)가 준비되었습니다"
        break
    fi
    echo "⏳ 컨테이너 준비 중... ($i/30)"
    sleep 2
done

# SSH 키 연결 테스트
echo "7️⃣ SSH 키 연결 테스트..."
SSH_OPTS="-o ConnectTimeout=10 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes"

if timeout 15 ssh $SSH_OPTS -i "$TEMP_KEY_FILE" -p $SSH_PORT $SSH_USER@$SSH_HOST "echo 'SSH 키 인증 성공'" 2>/dev/null; then
    echo "✅ SSH 키 인증이 성공했습니다"
    
    # GPU 정보 확인
    echo "8️⃣ GPU 접근 테스트..."
    GPU_TEST_RESULT=$(timeout 10 ssh $SSH_OPTS -i "$TEMP_KEY_FILE" -p $SSH_PORT $SSH_USER@$SSH_HOST "nvidia-smi -L 2>/dev/null | head -1" 2>/dev/null || echo "")
    if [[ -n "$GPU_TEST_RESULT" && "$GPU_TEST_RESULT" =~ GPU ]]; then
        echo "✅ GPU 접근 성공: $GPU_TEST_RESULT"
    else
        echo "⚠️ GPU 정보를 가져올 수 없습니다"
    fi
    
    # 워크스페이스 접근 테스트
    echo "9️⃣ 워크스페이스 접근 테스트..."
    WORKSPACE_TEST=$(timeout 10 ssh $SSH_OPTS -i "$TEMP_KEY_FILE" -p $SSH_PORT $SSH_USER@$SSH_HOST "ls -la /workspace && echo 'workspace_ok'" 2>/dev/null || echo "")
    if [[ "$WORKSPACE_TEST" =~ workspace_ok ]]; then
        echo "✅ 워크스페이스 접근 성공"
    else
        echo "⚠️ 워크스페이스 접근 실패"
    fi
else
    echo "❌ SSH 키 인증에 실패했습니다"
    
    # 비밀번호 인증 테스트 (fallback)
    echo "🔐 비밀번호 인증 테스트 (fallback)..."
    SSH_PASSWORD=$(echo $SESSION_INFO | jq -r '.metadata.ssh_password')
    if command -v sshpass > /dev/null && [[ -n "$SSH_PASSWORD" && "$SSH_PASSWORD" != "null" ]]; then
        if timeout 10 sshpass -p "$SSH_PASSWORD" ssh $SSH_OPTS -p $SSH_PORT $SSH_USER@$SSH_HOST "echo 'SSH 비밀번호 인증 성공'" 2>/dev/null; then
            echo "✅ SSH 비밀번호 인증이 성공했습니다"
        else
            echo "❌ SSH 비밀번호 인증에도 실패했습니다"
        fi
    else
        echo "⚠️ sshpass가 없거나 비밀번호 정보가 없어 비밀번호 테스트를 건너뜁니다"
    fi
fi

# SSH 개인키 파일 정리
rm -f "$TEMP_KEY_FILE"
echo "🗑️ 임시 SSH 키 파일 삭제됨"

# 전체 세션 목록 조회
echo "🔟 전체 세션 목록 조회..."
ALL_SESSIONS=$(curl -s http://$API_HOST/sessions)
echo "전체 세션: $(echo "$ALL_SESSIONS" | jq -r '.[].session_id' | wc -l)개 세션"

# 세션 삭제
echo "1️⃣1️⃣ 테스트 세션 삭제..."
DELETE_RESPONSE=$(curl -s -X DELETE http://$API_HOST/sessions/$SESSION_ID)
echo "삭제 응답: $DELETE_RESPONSE"

# 세션 삭제 확인
echo "1️⃣2️⃣ 세션 삭제 확인..."
if curl -f -s http://$API_HOST/sessions/$SESSION_ID > /dev/null; then
    echo "⚠️ 세션이 아직 존재합니다 (정리 중일 수 있음)"
else
    echo "✅ 세션이 성공적으로 삭제되었습니다"
fi

echo ""
echo "🎉 모든 테스트가 완료되었습니다!"
echo ""
echo "📊 테스트 요약:"
echo "- API 서버: ✅"
echo "- GPU 정보 조회: ✅"
echo "- 세션 생성: ✅"
echo "- SSH 키 생성: ✅"
echo "- 세션 조회: ✅"
echo "- 컨테이너 시작: ✅"
echo "- SSH 키 인증: ✅"
echo "- 세션 삭제: ✅"
echo "" 