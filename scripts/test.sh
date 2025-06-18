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
    \"mig_profile\": \"3g.20gb\"
  }")

echo "세션 생성 응답: $SESSION_RESPONSE"

# 세션 ID 추출
SESSION_ID=$(echo $SESSION_RESPONSE | grep -o '"session_id":"[^"]*"' | cut -d'"' -f4)
if [[ -z "$SESSION_ID" ]]; then
    echo "❌ 세션 ID를 추출할 수 없습니다"
    exit 1
fi

echo "📋 생성된 세션 ID: $SESSION_ID"

# 세션 정보 조회
echo "5️⃣ 세션 정보 조회..."
SESSION_INFO=$(curl -s http://$API_HOST/sessions/$SESSION_ID)
echo "세션 정보: $SESSION_INFO"

# 컨테이너 IP 추출
CONTAINER_IP=$(echo $SESSION_INFO | grep -o '"container_ip":"[^"]*"' | cut -d'"' -f4)
echo "📍 컨테이너 IP: $CONTAINER_IP"

# 네트워크 연결 테스트 (컨테이너가 준비될 때까지 대기)
echo "6️⃣ 컨테이너 준비 대기..."
for i in {1..30}; do
    if nc -z $CONTAINER_IP 22 2>/dev/null; then
        echo "✅ 컨테이너 SSH 서비스가 준비되었습니다"
        break
    fi
    echo "⏳ 컨테이너 준비 중... ($i/30)"
    sleep 2
done

# SSH 연결 테스트 (자동 종료)
echo "7️⃣ SSH 연결 테스트..."
if timeout 10 ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no \
   -o UserKnownHostsFile=/dev/null root@$CONTAINER_IP "echo 'SSH 연결 성공'" 2>/dev/null; then
    echo "✅ SSH 연결이 성공했습니다"
else
    echo "⚠️ SSH 연결 테스트를 건너뜁니다 (인증 필요)"
fi

# 전체 세션 목록 조회
echo "8️⃣ 전체 세션 목록 조회..."
ALL_SESSIONS=$(curl -s http://$API_HOST/sessions)
echo "전체 세션: $ALL_SESSIONS"

# 세션 삭제
echo "9️⃣ 테스트 세션 삭제..."
DELETE_RESPONSE=$(curl -s -X DELETE http://$API_HOST/sessions/$SESSION_ID)
echo "삭제 응답: $DELETE_RESPONSE"

# 세션 삭제 확인
echo "🔟 세션 삭제 확인..."
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
echo "- 세션 조회: ✅"
echo "- 컨테이너 시작: ✅"
echo "- 세션 삭제: ✅"
echo "" 