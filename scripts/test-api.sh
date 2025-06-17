#!/bin/bash

# GPU 컨테이너 오케스트레이터 API 테스트 스크립트

set -e

API_BASE="http://localhost:8080/api/v1"
USER_ID="testuser$(date +%s)"

echo "🧪 GPU 컨테이너 오케스트레이터 API 테스트 시작"
echo "==================================================="

# 색상 코드
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 헬퍼 함수
success() {
    echo -e "${GREEN}✅ $1${NC}"
}

error() {
    echo -e "${RED}❌ $1${NC}"
    exit 1
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# 1. 헬스체크
echo ""
info "1️⃣ 헬스체크 테스트"
response=$(curl -s -w "%{http_code}" -o /tmp/health_response.json $API_BASE/healthz)
http_code=${response: -3}

if [ "$http_code" = "200" ]; then
    success "헬스체크 통과"
    cat /tmp/health_response.json | jq .
else
    error "헬스체크 실패 (HTTP $http_code)"
fi

# 2. GPU 정보 조회
echo ""
info "2️⃣ GPU 정보 조회 테스트"
response=$(curl -s -w "%{http_code}" -o /tmp/gpu_response.json $API_BASE/gpus)
http_code=${response: -3}

if [ "$http_code" = "200" ]; then
    success "GPU 정보 조회 성공"
    cat /tmp/gpu_response.json | jq .
else
    warning "GPU 정보 조회 실패 (HTTP $http_code) - GPU가 없을 수 있습니다"
    cat /tmp/gpu_response.json | jq .
fi

# 3. MIG 프로파일 조회
echo ""
info "3️⃣ MIG 프로파일 조회 테스트"
response=$(curl -s -w "%{http_code}" -o /tmp/profiles_response.json $API_BASE/gpus/profiles)
http_code=${response: -3}

if [ "$http_code" = "200" ]; then
    success "MIG 프로파일 조회 성공"
    cat /tmp/profiles_response.json | jq .
else
    error "MIG 프로파일 조회 실패 (HTTP $http_code)"
fi

# 4. 세션 생성
echo ""
info "4️⃣ 세션 생성 테스트 (사용자: $USER_ID)"
session_data="{
    \"user_id\": \"$USER_ID\",
    \"mig_profile\": \"3g.20gb\",
    \"ttl_minutes\": 5
}"

response=$(curl -s -w "%{http_code}" -o /tmp/session_response.json \
    -H "Content-Type: application/json" \
    -X POST \
    -d "$session_data" \
    $API_BASE/sessions)
http_code=${response: -3}

if [ "$http_code" = "201" ]; then
    success "세션 생성 성공"
    cat /tmp/session_response.json | jq .
    SESSION_ID=$(cat /tmp/session_response.json | jq -r '.session_id')
    info "세션 ID: $SESSION_ID"
else
    warning "세션 생성 실패 (HTTP $http_code) - GPU가 없거나 이미 세션이 존재할 수 있습니다"
    cat /tmp/session_response.json | jq .
    SESSION_ID=""
fi

# 5. 세션 조회
if [ ! -z "$SESSION_ID" ]; then
    echo ""
    info "5️⃣ 세션 조회 테스트"
    response=$(curl -s -w "%{http_code}" -o /tmp/get_session_response.json $API_BASE/sessions/$SESSION_ID)
    http_code=${response: -3}

    if [ "$http_code" = "200" ]; then
        success "세션 조회 성공"
        cat /tmp/get_session_response.json | jq .
    else
        error "세션 조회 실패 (HTTP $http_code)"
    fi
fi

# 6. 모든 세션 조회
echo ""
info "6️⃣ 모든 세션 조회 테스트"
response=$(curl -s -w "%{http_code}" -o /tmp/all_sessions_response.json $API_BASE/sessions)
http_code=${response: -3}

if [ "$http_code" = "200" ]; then
    success "모든 세션 조회 성공"
    cat /tmp/all_sessions_response.json | jq .
else
    error "모든 세션 조회 실패 (HTTP $http_code)"
fi

# 7. 세션 통계
echo ""
info "7️⃣ 세션 통계 조회 테스트"
response=$(curl -s -w "%{http_code}" -o /tmp/stats_response.json $API_BASE/sessions/stats)
http_code=${response: -3}

if [ "$http_code" = "200" ]; then
    success "세션 통계 조회 성공"
    cat /tmp/stats_response.json | jq .
else
    error "세션 통계 조회 실패 (HTTP $http_code)"
fi

# 8. 세션 삭제 (생성된 세션이 있는 경우)
if [ ! -z "$SESSION_ID" ]; then
    echo ""
    info "8️⃣ 세션 삭제 테스트"
    response=$(curl -s -w "%{http_code}" -o /tmp/delete_response.json \
        -X DELETE \
        $API_BASE/sessions/$SESSION_ID)
    http_code=${response: -3}

    if [ "$http_code" = "200" ]; then
        success "세션 삭제 성공"
        cat /tmp/delete_response.json | jq .
    else
        error "세션 삭제 실패 (HTTP $http_code)"
    fi
fi

# 정리
rm -f /tmp/*_response.json

echo ""
echo "=================================================="
success "🎉 모든 API 테스트 완료!"
echo ""
info "📋 다음 단계:"
echo "  1. 세션 생성 후 SSH 접속 테스트:"
echo "     ssh $USER_ID@localhost"
echo ""
echo "  2. 실시간 로그 확인:"
echo "     make logs"
echo ""
echo "  3. 모니터링 대시보드:"
echo "     - Prometheus: http://localhost:9090"
echo "     - Grafana: http://localhost:3000" 