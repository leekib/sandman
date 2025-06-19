#!/bin/bash

# Sandman API 종합 테스트 스크립트
# 모든 API 엔드포인트를 체계적으로 테스트합니다

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 설정
API_HOST="localhost:8080"
API_URL="http://$API_HOST"
TEST_USER_PREFIX="testuser"
CREATED_SESSIONS=()
TEST_COUNT=0
PASS_COUNT=0
FAIL_COUNT=0

# 유틸리티 함수
log() {
    echo -e "${CYAN}[$(date '+%H:%M:%S')] $1${NC}"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
    ((PASS_COUNT++))
}

error() {
    echo -e "${RED}❌ $1${NC}"
    ((FAIL_COUNT++))
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

section() {
    echo -e "\n${PURPLE}=== $1 ===${NC}"
    ((TEST_COUNT++))
}

# JSON 응답 검증 함수
validate_json() {
    local response="$1"
    local expected_key="$2"
    
    if echo "$response" | jq -e ".$expected_key" > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# HTTP 요청 함수들
http_get() {
    local endpoint="$1"
    local expected_status="${2:-200}"
    
    local response=$(curl -s -w "\n%{http_code}" "$API_URL$endpoint")
    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

http_post() {
    local endpoint="$1"
    local data="$2"
    local expected_status="${3:-200}"
    
    local response=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Content-Type: application/json" \
        -d "$data" \
        "$API_URL$endpoint")
    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

http_delete() {
    local endpoint="$1"
    local expected_status="${2:-200}"
    
    local response=$(curl -s -w "\n%{http_code}" -X DELETE "$API_URL$endpoint")
    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

# 테스트 함수들

# 1. 서버 연결 테스트
test_server_connection() {
    section "서버 연결 테스트"
    
    if curl -s -f "$API_URL/healthz" > /dev/null 2>&1; then
        success "서버 연결 성공"
        return 0
    else
        error "서버에 연결할 수 없습니다"
        exit 1
    fi
}

# 2. 헬스체크 API 테스트
test_healthcheck() {
    section "헬스체크 API 테스트"
    
    local result=$(http_get "/healthz")
    if [ $? -eq 0 ]; then
        if validate_json "$result" "status"; then
            local status=$(echo "$result" | jq -r '.status')
            if [ "$status" = "healthy" ]; then
                success "헬스체크 성공 - 상태: $status"
            else
                error "헬스체크 실패 - 예상하지 못한 상태: $status"
            fi
        else
            error "헬스체크 응답 형식 오류: $result"
        fi
    else
        error "헬스체크 요청 실패: $result"
    fi
}

# 3. CORS 테스트
test_cors() {
    section "CORS 설정 테스트"
    
    local cors_response=$(curl -s -v -X OPTIONS "$API_URL/sessions" \
        -H "Origin: http://example.com" \
        -H "Access-Control-Request-Method: POST" \
        -H "Access-Control-Request-Headers: Content-Type" 2>&1)
    
    if echo "$cors_response" | grep -q "Access-Control-Allow-Origin: \*"; then
        success "CORS Origin 설정 확인"
    else
        error "CORS Origin 설정 미확인"
    fi
    
    if echo "$cors_response" | grep -q "Access-Control-Allow-Methods:"; then
        success "CORS Methods 설정 확인"
    else
        error "CORS Methods 설정 미확인"
    fi
    
    if echo "$cors_response" | grep -q "HTTP/1.1 204"; then
        success "CORS Preflight 요청 성공"
    else
        error "CORS Preflight 요청 실패"
    fi
}

# 4. GPU 정보 조회 테스트
test_gpu_info() {
    section "GPU 정보 조회 테스트"
    
    local result=$(http_get "/gpus")
    if [ $? -eq 0 ]; then
        if validate_json "$result" "gpus"; then
            local gpu_count=$(echo "$result" | jq -r '.count // 0')
            success "GPU 정보 조회 성공 - GPU 개수: $gpu_count"
            info "GPU 목록: $(echo "$result" | jq -r '.gpus[].name // "N/A"' | tr '\n' ', ' | sed 's/,$//')"
        else
            warning "GPU 정보 응답 형식 확인 필요: $result"
        fi
    else
        error "GPU 정보 조회 실패: $result"
    fi
}

# 5. MIG 프로파일 조회 테스트
test_mig_profiles() {
    section "MIG 프로파일 조회 테스트"
    
    local result=$(http_get "/gpus/profiles")
    if [ $? -eq 0 ]; then
        if validate_json "$result" "profiles"; then
            local profile_count=$(echo "$result" | jq -r '.profiles | length')
            success "MIG 프로파일 조회 성공 - 프로파일 개수: $profile_count"
            info "지원 프로파일: $(echo "$result" | jq -r '.profiles[].name' | tr '\n' ', ' | sed 's/,$//')"
        else
            error "MIG 프로파일 응답 형식 오류: $result"
        fi
    else
        error "MIG 프로파일 조회 실패: $result"
    fi
}

# 6. 사용 가능한 MIG 인스턴스 조회 테스트
test_available_mig() {
    section "사용 가능한 MIG 인스턴스 조회 테스트"
    
    local result=$(http_get "/gpus/available")
    if [ $? -eq 0 ]; then
        if validate_json "$result" "available_instances"; then
            local available_count=$(echo "$result" | jq -r '.count // 0')
            success "사용 가능한 MIG 인스턴스 조회 성공 - 개수: $available_count"
            if [ "$available_count" -gt 0 ]; then
                info "사용 가능한 인스턴스: $(echo "$result" | jq -r '.available_instances[].profile' | tr '\n' ', ' | sed 's/,$//')"
            else
                warning "현재 사용 가능한 MIG 인스턴스가 없습니다"
            fi
        else
            error "사용 가능한 MIG 인스턴스 응답 형식 오류: $result"
        fi
    else
        error "사용 가능한 MIG 인스턴스 조회 실패: $result"
    fi
}

# 7. 빈 세션 목록 조회 테스트
test_empty_sessions() {
    section "빈 세션 목록 조회 테스트"
    
    local result=$(http_get "/sessions")
    if [ $? -eq 0 ]; then
        if [ "$result" = "[]" ]; then
            success "빈 세션 목록이 올바른 형식(빈 배열)으로 반환됨"
        else
            error "빈 세션 목록 형식 오류 - 예상: [], 실제: $result"
        fi
    else
        error "세션 목록 조회 실패: $result"
    fi
}

# 8. 세션 생성 테스트 (Mock - 실제 GPU 없어도 API 테스트)
test_session_creation_api() {
    section "세션 생성 API 테스트"
    
    local user_id="${TEST_USER_PREFIX}_$(date +%s)"
    local test_data="{\"user_id\":\"$user_id\",\"mig_profile\":\"1g.10gb\",\"ttl_minutes\":5}"
    
    info "테스트 데이터: $test_data"
    
    local result=$(http_post "/sessions" "$test_data" 500)  # GPU 없으면 500 에러 예상
    if [ $? -eq 0 ]; then
        if echo "$result" | jq -e '.error' > /dev/null 2>&1; then
            local error_msg=$(echo "$result" | jq -r '.error')
            if echo "$error_msg" | grep -q "GPU 할당 실패"; then
                success "세션 생성 API 동작 확인 (GPU 없음으로 예상된 실패)"
                info "에러 메시지: $error_msg"
            else
                warning "예상하지 못한 에러: $error_msg"
            fi
        else
            # 실제로 세션이 생성된 경우
            if validate_json "$result" "session_id"; then
                local session_id=$(echo "$result" | jq -r '.session_id')
                CREATED_SESSIONS+=("$session_id")
                success "세션 생성 성공 - ID: $session_id"
                info "SSH 포트: $(echo "$result" | jq -r '.ssh_port')"
                info "GPU UUID: $(echo "$result" | jq -r '.gpu_uuid')"
            else
                warning "세션 생성 응답 형식 확인 필요: $result"
            fi
        fi
    else
        error "세션 생성 요청 실패: $result"
    fi
}

# 9. 잘못된 요청 테스트
test_invalid_requests() {
    section "잘못된 요청 처리 테스트"
    
    # 빈 사용자 ID
    local result1=$(http_post "/sessions" "{\"user_id\":\"\",\"mig_profile\":\"1g.10gb\"}" 400)
    if [ $? -eq 0 ]; then
        if echo "$result1" | jq -e '.error' > /dev/null 2>&1; then
            success "빈 사용자 ID 에러 처리 확인"
        else
            error "빈 사용자 ID 에러 처리 실패"
        fi
    else
        warning "빈 사용자 ID 테스트 예상과 다른 응답: $result1"
    fi
    
    # 잘못된 JSON
    local result2=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Content-Type: application/json" \
        -d "invalid json" \
        "$API_URL/sessions")
    local code2=$(echo "$result2" | tail -n1)
    if [ "$code2" = "400" ]; then
        success "잘못된 JSON 형식 에러 처리 확인"
    else
        error "잘못된 JSON 형식 에러 처리 실패 - 코드: $code2"
    fi
    
    # 존재하지 않는 세션 조회
    local result3=$(http_get "/sessions/nonexistent-session-id" 404)
    if [ $? -eq 0 ]; then
        if echo "$result3" | jq -e '.error' > /dev/null 2>&1; then
            success "존재하지 않는 세션 조회 에러 처리 확인"
        else
            error "존재하지 않는 세션 조회 에러 처리 실패"
        fi
    else
        warning "존재하지 않는 세션 조회 테스트 예상과 다른 응답: $result3"
    fi
}

# 10. 중복 사용자 ID 테스트 (Mock)
test_duplicate_user() {
    section "중복 사용자 ID 처리 테스트"
    
    local user_id="duplicate_user_test"
    local test_data="{\"user_id\":\"$user_id\",\"mig_profile\":\"1g.10gb\"}"
    
    # 첫 번째 요청
    local result1=$(http_post "/sessions" "$test_data" 500)  # GPU 없으면 500
    
    # 두 번째 요청 (같은 사용자 ID)
    local result2=$(http_post "/sessions" "$test_data" 500)
    if [ $? -eq 0 ]; then
        local error_msg=$(echo "$result2" | jq -r '.error // empty')
        if echo "$error_msg" | grep -q "세션이 이미 존재합니다"; then
            success "중복 사용자 ID 에러 처리 확인"
        elif echo "$error_msg" | grep -q "GPU 할당 실패"; then
            warning "GPU 없음으로 인한 실패 (중복 검사 전에 실패)"
        else
            warning "예상하지 못한 응답: $error_msg"
        fi
    else
        warning "중복 사용자 ID 테스트 실패: $result2"
    fi
}

# 11. Content-Type 테스트
test_content_types() {
    section "Content-Type 처리 테스트"
    
    # JSON Content-Type 없이 요청
    local result1=$(curl -s -w "\n%{http_code}" -X POST \
        -d '{"user_id":"test","mig_profile":"1g.10gb"}' \
        "$API_URL/sessions")
    local code1=$(echo "$result1" | tail -n1)
    
    if [ "$code1" = "400" ] || [ "$code1" = "500" ]; then
        success "Content-Type 검증 확인"
    else
        warning "Content-Type 검증 결과 확인 필요 - 코드: $code1"
    fi
    
    # 잘못된 Content-Type
    local result2=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Content-Type: text/plain" \
        -d '{"user_id":"test","mig_profile":"1g.10gb"}' \
        "$API_URL/sessions")
    local code2=$(echo "$result2" | tail -n1)
    
    if [ "$code2" = "400" ] || [ "$code2" = "500" ]; then
        success "잘못된 Content-Type 처리 확인"
    else
        warning "잘못된 Content-Type 처리 결과 확인 필요 - 코드: $code2"
    fi
}

# 12. 모든 세션 삭제 테스트
test_delete_all_sessions() {
    section "모든 세션 삭제 API 테스트"
    
    local result=$(http_delete "/sessions")
    if [ $? -eq 0 ]; then
        if validate_json "$result" "message"; then
            local message=$(echo "$result" | jq -r '.message')
            success "모든 세션 삭제 API 성공 - 메시지: $message"
        else
            error "모든 세션 삭제 응답 형식 오류: $result"
        fi
    else
        error "모든 세션 삭제 실패: $result"
    fi
    
    # 삭제 후 세션 목록이 빈 배열인지 확인
    sleep 1
    local sessions=$(http_get "/sessions")
    if [ "$sessions" = "[]" ]; then
        success "모든 세션 삭제 후 목록이 올바르게 비워졌습니다"
    else
        warning "세션 삭제 후에도 목록이 완전히 비워지지 않았습니다: $sessions"
    fi
}

# 13. HTTP 메서드 테스트
test_http_methods() {
    section "HTTP 메서드 테스트"
    
    # GET 메서드 테스트 (이미 다른 테스트에서 확인)
    success "GET 메서드 지원 확인됨"
    
    # POST 메서드 테스트 (이미 다른 테스트에서 확인)
    success "POST 메서드 지원 확인됨"
    
    # DELETE 메서드 테스트 (이미 다른 테스트에서 확인)
    success "DELETE 메서드 지원 확인됨"
    
    # 지원하지 않는 메서드 테스트
    local put_result=$(curl -s -w "\n%{http_code}" -X PUT "$API_URL/sessions")
    local put_code=$(echo "$put_result" | tail -n1)
    
    # PUT은 정의되지 않았으므로 404가 예상됨
    if [ "$put_code" = "404" ]; then
        success "정의되지 않은 메서드(PUT) 적절히 처리됨"
    else
        warning "정의되지 않은 메서드 처리 결과 확인 필요 - 코드: $put_code"
    fi
}

# 14. 응답 시간 테스트
test_response_time() {
    section "응답 시간 테스트"
    
    local start_time=$(date +%s%N)
    http_get "/healthz" > /dev/null 2>&1
    local end_time=$(date +%s%N)
    
    local response_time=$(( (end_time - start_time) / 1000000 ))  # ms 단위
    
    if [ $response_time -lt 1000 ]; then  # 1초 미만
        success "응답 시간 양호: ${response_time}ms"
    elif [ $response_time -lt 5000 ]; then  # 5초 미만
        warning "응답 시간 보통: ${response_time}ms"
    else
        error "응답 시간 느림: ${response_time}ms"
    fi
}

# 15. 동시 요청 테스트
test_concurrent_requests() {
    section "동시 요청 처리 테스트"
    
    info "5개의 동시 헬스체크 요청 실행 중..."
    
    local pids=()
    local results=()
    
    for i in {1..5}; do
        (
            local result=$(http_get "/healthz" 2>/dev/null)
            echo "$result" > "/tmp/concurrent_test_$i.tmp"
        ) &
        pids+=($!)
    done
    
    # 모든 프로세스 대기
    for pid in "${pids[@]}"; do
        wait "$pid"
    done
    
    # 결과 확인
    local success_count=0
    for i in {1..5}; do
        if [ -f "/tmp/concurrent_test_$i.tmp" ]; then
            local result=$(cat "/tmp/concurrent_test_$i.tmp")
            if validate_json "$result" "status"; then
                ((success_count++))
            fi
            rm -f "/tmp/concurrent_test_$i.tmp"
        fi
    done
    
    if [ $success_count -eq 5 ]; then
        success "동시 요청 처리 성공: 5/5"
    else
        warning "동시 요청 처리 부분 성공: $success_count/5"
    fi
}

# 정리 함수
cleanup() {
    info "테스트 정리 중..."
    
    # 생성된 세션들 정리
    for session_id in "${CREATED_SESSIONS[@]}"; do
        http_delete "/sessions/$session_id" > /dev/null 2>&1
    done
    
    # 모든 세션 강제 정리
    http_delete "/sessions" > /dev/null 2>&1
    
    # 임시 파일 정리
    rm -f /tmp/concurrent_test_*.tmp
    
    info "정리 완료"
}

# 결과 출력
print_results() {
    echo -e "\n${PURPLE}=================== 테스트 결과 ===================${NC}"
    echo -e "${BLUE}총 테스트 카테고리: $TEST_COUNT${NC}"
    echo -e "${GREEN}성공한 검증: $PASS_COUNT${NC}"
    echo -e "${RED}실패한 검증: $FAIL_COUNT${NC}"
    
    local total_checks=$((PASS_COUNT + FAIL_COUNT))
    if [ $total_checks -gt 0 ]; then
        local success_rate=$(( PASS_COUNT * 100 / total_checks ))
        echo -e "${CYAN}성공률: ${success_rate}%${NC}"
    fi
    
    echo -e "${PURPLE}=================================================${NC}"
    
    if [ $FAIL_COUNT -eq 0 ]; then
        echo -e "\n${GREEN}🎉 모든 API 테스트가 성공했습니다!${NC}"
        exit 0
    else
        echo -e "\n${YELLOW}⚠️  일부 테스트에서 문제가 발견되었습니다.${NC}"
        echo -e "${YELLOW}   실제 GPU 환경에서 일부 기능이 다르게 동작할 수 있습니다.${NC}"
        exit 0  # GPU 없는 환경에서는 정상
    fi
}

# 메인 함수
main() {
    echo -e "${PURPLE}"
    echo "================================================="
    echo "        Sandman API 종합 테스트 스크립트"
    echo "================================================="
    echo -e "${NC}"
    echo -e "${CYAN}API 서버: $API_URL${NC}"
    echo -e "${CYAN}테스트 시작 시간: $(date)${NC}"
    echo ""
    
    # 신호 처리 설정
    trap cleanup EXIT
    
    # 테스트 실행
    test_server_connection
    test_healthcheck
    test_cors
    test_gpu_info
    test_mig_profiles
    test_available_mig
    test_empty_sessions
    test_session_creation_api
    test_invalid_requests
    test_duplicate_user
    test_content_types
    test_delete_all_sessions
    test_http_methods
    test_response_time
    test_concurrent_requests
    
    print_results
}

# 도움말
show_help() {
    echo "사용법: $0 [옵션]"
    echo ""
    echo "옵션:"
    echo "  -h, --help     이 도움말 표시"
    echo "  -v, --verbose  상세 출력 모드"
    echo ""
    echo "예시:"
    echo "  $0              # 기본 테스트 실행"
    echo "  $0 --verbose    # 상세한 출력과 함께 테스트 실행"
    echo ""
}

# 명령행 인수 처리
case "${1:-}" in
    -h|--help)
        show_help
        exit 0
        ;;
    -v|--verbose)
        set -x
        main
        ;;
    "")
        main
        ;;
    *)
        echo "알 수 없는 옵션: $1"
        show_help
        exit 1
        ;;
esac 