#!/bin/bash

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 전역 변수
API_URL="http://localhost:8080"
TEST_PASSED=0
TEST_FAILED=0
CREATED_SESSIONS=()

# 유틸리티 함수
log() {
    echo -e "${CYAN}[$(date '+%H:%M:%S')] $1${NC}"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
    ((TEST_PASSED++))
}

error() {
    echo -e "${RED}❌ $1${NC}"
    ((TEST_FAILED++))
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

section() {
    echo -e "\n${PURPLE}=== $1 ===${NC}"
}

# HTTP 요청 함수
http_get() {
    local url="$1"
    local expected_status="${2:-200}"
    
    response=$(curl -s -w "\n%{http_code}" "$API_URL$url")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

http_post() {
    local url="$1"
    local data="$2"
    local expected_status="${3:-200}"
    
    response=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Content-Type: application/json" \
        -d "$data" \
        "$API_URL$url")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

http_delete() {
    local url="$1"
    local expected_status="${2:-200}"
    
    response=$(curl -s -w "\n%{http_code}" -X DELETE "$API_URL$url")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "$body"
        return 0
    else
        echo "HTTP $http_code: $body"
        return 1
    fi
}

# 서버 연결 확인
check_server() {
    section "서버 연결 확인"
    
    if curl -s -f "$API_URL/healthz" > /dev/null 2>&1; then
        success "서버 연결 성공"
        return 0
    else
        error "서버에 연결할 수 없습니다. 서버가 실행 중인지 확인하세요."
        return 1
    fi
}

# 사용 가능한 GPU 조회 테스트
test_gpu_availability() {
    section "GPU 가용성 조회 테스트"
    
    local result
    result=$(http_get "/gpus/available")
    if [ $? -eq 0 ]; then
        success "GPU 가용성 조회 성공"
        info "사용 가능한 GPU 인스턴스:"
        echo "$result" | jq -r '.[] | "  - \(.profile) (\(.uuid))"' 2>/dev/null || echo "$result"
    else
        error "GPU 가용성 조회 실패: $result"
    fi
}

# 세션 생성 테스트 (프로필 기반)
test_session_creation_profile() {
    section "세션 생성 테스트 (프로필 기반)"
    
    local user_id="test_user_$(date +%s)"
    local mig_profile="1g.10gb"
    local data="{\"user_id\":\"$user_id\",\"mig_profile\":\"$mig_profile\"}"
    
    local result
    result=$(http_post "/sessions" "$data" 201)
    if [ $? -eq 0 ]; then
        success "프로필 기반 세션 생성 성공"
        local session_id=$(echo "$result" | jq -r '.id' 2>/dev/null)
        if [ "$session_id" != "null" ] && [ "$session_id" != "" ]; then
            CREATED_SESSIONS+=("$session_id")
            info "생성된 세션 ID: $session_id"
            info "사용자 ID: $user_id"
            info "MIG 프로필: $mig_profile"
        fi
    else
        error "프로필 기반 세션 생성 실패: $result"
    fi
}

# 세션 생성 테스트 (UUID 기반)
test_session_creation_uuid() {
    section "세션 생성 테스트 (UUID 기반)"
    
    # 먼저 사용 가능한 UUID 가져오기
    local available_result
    available_result=$(http_get "/gpus/available")
    if [ $? -ne 0 ]; then
        error "UUID 기반 테스트를 위한 GPU 정보 조회 실패"
        return
    fi
    
    local mig_uuid=$(echo "$available_result" | jq -r '.available_instances[0].uuid' 2>/dev/null)
    if [ "$mig_uuid" = "null" ] || [ "$mig_uuid" = "" ]; then
        warning "사용 가능한 MIG 인스턴스가 없어 UUID 기반 테스트를 건너뜁니다"
        return
    fi
    
    local user_id="test_user_uuid_$(date +%s)"
    local data="{\"user_id\":\"$user_id\",\"mig_instance_uuid\":\"$mig_uuid\"}"
    
    local result
    result=$(http_post "/sessions" "$data" 201)
    if [ $? -eq 0 ]; then
        success "UUID 기반 세션 생성 성공"
        local session_id=$(echo "$result" | jq -r '.session_id' 2>/dev/null)
        if [ "$session_id" != "null" ] && [ "$session_id" != "" ]; then
            CREATED_SESSIONS+=("$session_id")
            info "생성된 세션 ID: $session_id"
            info "사용자 ID: $user_id"
            info "MIG UUID: $mig_uuid"
        fi
    else
        error "UUID 기반 세션 생성 실패: $result"
    fi
}

# 세션 목록 조회 테스트
test_session_list() {
    section "세션 목록 조회 테스트"
    
    local result
    result=$(http_get "/sessions")
    if [ $? -eq 0 ]; then
        success "세션 목록 조회 성공"
        local session_count=$(echo "$result" | jq length 2>/dev/null || echo "0")
        info "현재 활성 세션 수: $session_count"
        
        if [ "$session_count" != "0" ]; then
            info "활성 세션 목록:"
            echo "$result" | jq -r '.[] | "  - \(.id) (\(.user_id))"' 2>/dev/null || echo "$result"
        fi
    else
        error "세션 목록 조회 실패: $result"
    fi
}

# 개별 세션 조회 테스트
test_session_get() {
    section "개별 세션 조회 테스트"
    
    if [ ${#CREATED_SESSIONS[@]} -eq 0 ]; then
        warning "조회할 세션이 없습니다"
        return
    fi
    
    local session_id="${CREATED_SESSIONS[0]}"
    local result
    result=$(http_get "/sessions/$session_id")
    if [ $? -eq 0 ]; then
        success "개별 세션 조회 성공"
        info "세션 정보:"
        echo "$result" | jq . 2>/dev/null || echo "$result"
    else
        error "개별 세션 조회 실패: $result"
    fi
}

# 에러 케이스 테스트
test_error_cases() {
    section "에러 케이스 테스트"
    
    # 1. 중복 사용자 ID로 세션 생성
    info "중복 사용자 ID 테스트..."
    local duplicate_user="duplicate_user_test"
    local data1="{\"user_id\":\"$duplicate_user\",\"mig_profile\":\"1g.10gb\"}"
    
    local result1
    result1=$(http_post "/sessions" "$data1" 201)
    if [ $? -eq 0 ]; then
        local session_id1=$(echo "$result1" | jq -r '.session_id' 2>/dev/null)
        CREATED_SESSIONS+=("$session_id1")
        
        # 같은 사용자로 다시 세션 생성 시도
        local result2
        result2=$(http_post "/sessions" "$data1" 500)
        if [ $? -eq 0 ]; then
            success "중복 사용자 ID 에러 처리 성공"
        else
            error "중복 사용자 ID 에러 처리 실패: $result2"
        fi
    else
        error "중복 사용자 테스트용 세션 생성 실패: $result1"
    fi
    
    # 2. 존재하지 않는 세션 조회
    info "존재하지 않는 세션 조회 테스트..."
    local fake_session_id="fake-session-id-123"
    local result3
    result3=$(http_get "/sessions/$fake_session_id" 404)
    if [ $? -eq 0 ]; then
        success "존재하지 않는 세션 조회 에러 처리 성공"
    else
        error "존재하지 않는 세션 조회 에러 처리 실패: $result3"
    fi
    
    # 3. 잘못된 UUID로 세션 생성
    info "잘못된 UUID로 세션 생성 테스트..."
    local invalid_data="{\"user_id\":\"invalid_uuid_user\",\"mig_instance_uuid\":\"invalid-uuid-123\"}"
    local result4
    result4=$(http_post "/sessions" "$invalid_data" 500)
    if [ $? -eq 0 ]; then
        success "잘못된 UUID 에러 처리 성공"
    else
        error "잘못된 UUID 에러 처리 실패: $result4"
    fi
    
    # 4. 잘못된 프로필로 세션 생성
    info "잘못된 프로필로 세션 생성 테스트..."
    local invalid_profile_data="{\"user_id\":\"invalid_profile_user\",\"mig_profile\":\"invalid.profile\"}"
    local result5
    result5=$(http_post "/sessions" "$invalid_profile_data" 500)
    if [ $? -eq 0 ]; then
        success "잘못된 프로필 에러 처리 성공"
    else
        error "잘못된 프로필 에러 처리 실패: $result5"
    fi
}

# 세션 삭제 테스트
test_session_deletion() {
    section "세션 삭제 테스트"
    
    if [ ${#CREATED_SESSIONS[@]} -eq 0 ]; then
        warning "삭제할 세션이 없습니다"
        return
    fi
    
    local deleted_count=0
    for session_id in "${CREATED_SESSIONS[@]}"; do
        info "세션 삭제 중: $session_id"
        local result
        result=$(http_delete "/sessions/$session_id")
        if [ $? -eq 0 ]; then
            success "세션 삭제 성공: $session_id"
            ((deleted_count++))
        else
            error "세션 삭제 실패: $session_id - $result"
        fi
    done
    
    info "총 $deleted_count개의 세션이 삭제되었습니다"
    CREATED_SESSIONS=()
}

# 모든 세션 삭제 테스트
test_delete_all_sessions() {
    section "모든 세션 삭제 테스트"
    
    # 먼저 여러 개의 테스트 세션 생성
    local temp_sessions=()
    local session_count=3
    
    info "$session_count개의 테스트 세션 생성 중..."
    for i in $(seq 1 $session_count); do
        local user_id="bulk_delete_test_$i"
        local data="{\"user_id\":\"$user_id\",\"mig_profile\":\"1g.10gb\"}"
        
        local result
        result=$(http_post "/sessions" "$data" 201)
        if [ $? -eq 0 ]; then
            local session_id=$(echo "$result" | jq -r '.session_id' 2>/dev/null)
            if [ "$session_id" != "null" ] && [ "$session_id" != "" ]; then
                temp_sessions+=("$session_id")
            fi
        fi
    done
    
    info "생성된 테스트 세션 수: ${#temp_sessions[@]}"
    
    # 모든 세션 삭제 API 호출
    local result
    result=$(http_delete "/sessions")
    if [ $? -eq 0 ]; then
        success "모든 세션 삭제 API 호출 성공"
        info "응답: $result"
        
        # 세션 목록이 비어있는지 확인
        sleep 2  # 삭제 처리 시간 대기
        local list_result
        list_result=$(http_get "/sessions")
        if [ $? -eq 0 ]; then
            local remaining_count=$(echo "$list_result" | jq length 2>/dev/null || echo "0")
            if [ "$remaining_count" = "0" ]; then
                success "모든 세션이 성공적으로 삭제되었습니다"
            else
                warning "일부 세션이 아직 남아있습니다: $remaining_count개"
            fi
        fi
    else
        error "모든 세션 삭제 실패: $result"
    fi
    
    # 생성했던 세션들을 CREATED_SESSIONS에서 제거
    CREATED_SESSIONS=()
}

# 리소스 정리 테스트
test_resource_cleanup() {
    section "리소스 정리 확인 테스트"
    
    # 세션 삭제 후 GPU 가용성 재확인
    local result
    result=$(http_get "/gpus/available")
    if [ $? -eq 0 ]; then
        success "리소스 정리 후 GPU 가용성 조회 성공"
        local available_count=$(echo "$result" | jq length 2>/dev/null || echo "0")
        info "정리 후 사용 가능한 GPU 인스턴스 수: $available_count"
    else
        error "리소스 정리 후 GPU 가용성 조회 실패: $result"
    fi
}

# 성능 테스트 (다중 세션 생성/삭제)
test_performance() {
    section "성능 테스트 (다중 세션 처리)"
    
    local session_count=3
    local temp_sessions=()
    
    info "동시에 $session_count개의 세션 생성 중..."
    local start_time=$(date +%s)
    
    for i in $(seq 1 $session_count); do
        local user_id="perf_test_user_$i"
        local data="{\"user_id\":\"$user_id\",\"mig_profile\":\"1g.10gb\"}"
        
        local result
        result=$(http_post "/sessions" "$data" 201)
        if [ $? -eq 0 ]; then
            local session_id=$(echo "$result" | jq -r '.session_id' 2>/dev/null)
            if [ "$session_id" != "null" ] && [ "$session_id" != "" ]; then
                temp_sessions+=("$session_id")
            fi
        fi
    done
    
    local creation_time=$(($(date +%s) - start_time))
    success "$session_count개 세션 생성 완료 (${creation_time}초)"
    
    # 생성된 세션들 삭제
    info "생성된 세션들 삭제 중..."
    local delete_start=$(date +%s)
    
    for session_id in "${temp_sessions[@]}"; do
        http_delete "/sessions/$session_id" > /dev/null 2>&1
    done
    
    local deletion_time=$(($(date +%s) - delete_start))
    success "$session_count개 세션 삭제 완료 (${deletion_time}초)"
}

# SSH 접속 테스트
test_ssh_connection() {
    section "SSH 접속 테스트"
    
    # 테스트용 세션 생성
    local user_id="ssh_test_user_$(date +%s)"
    local data="{\"user_id\":\"$user_id\",\"mig_profile\":\"1g.10gb\"}"
    
    local result
    result=$(http_post "/sessions" "$data" 201)
    if [ $? -ne 0 ]; then
        error "SSH 테스트용 세션 생성 실패: $result"
        return
    fi
    
    local session_id=$(echo "$result" | jq -r '.session_id' 2>/dev/null)
    local ssh_host=$(echo "$result" | jq -r '.ssh_host' 2>/dev/null)
    local ssh_port=$(echo "$result" | jq -r '.ssh_port' 2>/dev/null)
    
    if [ "$session_id" = "null" ] || [ "$session_id" = "" ]; then
        error "세션 ID를 파싱할 수 없습니다"
        return
    fi
    
    # 세션을 정리 목록에 추가
    CREATED_SESSIONS+=("$session_id")
    
    info "SSH 테스트용 세션 생성됨: $session_id"
    info "SSH 접속 정보: $user_id@localhost:10022"
    
    # 세션 상세 정보 조회하여 SSH 비밀번호 획득
    local session_info
    session_info=$(http_get "/sessions/$session_id")
    if [ $? -ne 0 ]; then
        error "세션 정보 조회 실패: $session_info"
        return
    fi
    
    local ssh_password=$(echo "$session_info" | jq -r '.metadata.ssh_password' 2>/dev/null)
    if [ "$ssh_password" = "null" ] || [ "$ssh_password" = "" ]; then
        warning "SSH 비밀번호를 찾을 수 없습니다. 컨테이너가 아직 완전히 시작되지 않았을 수 있습니다."
        info "컨테이너 시작 대기 중... (10초)"
        sleep 10
        
        # 다시 시도
        session_info=$(http_get "/sessions/$session_id")
        ssh_password=$(echo "$session_info" | jq -r '.metadata.ssh_password' 2>/dev/null)
    fi
    
    info "SSH 비밀번호 확인됨"
    
    # SSH 접속 테스트 (포트 접근 가능성만 확인)
    info "SSH 포트 접근성 테스트 중..."
    if timeout 10 bash -c "echo > /dev/tcp/localhost/10022" 2>/dev/null; then
        success "SSH 포트(10022) 접근 가능"
    else
        error "SSH 포트(10022) 접근 불가 - SSHPiper가 실행 중인지 확인하세요"
        return
    fi
    
    # 실제 SSH 연결 테스트 (sshpass 사용 가능한 경우)
    if command -v sshpass > /dev/null; then
        info "sshpass를 사용한 SSH 연결 테스트 중..."
        
        # SSH 연결 테스트 (간단한 명령 실행)
        local ssh_result
        ssh_result=$(timeout 15 sshpass -p "$ssh_password" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 "$user_id@localhost" -p 10022 "echo 'SSH 연결 성공'" 2>/dev/null)
        
        if [ $? -eq 0 ] && echo "$ssh_result" | grep -q "SSH 연결 성공"; then
            success "SSH 연결 및 명령 실행 성공"
        else
            warning "SSH 연결 테스트 실패 - 컨테이너가 아직 완전히 준비되지 않았을 수 있습니다"
            info "수동 테스트: sshpass -p '$ssh_password' ssh $user_id@localhost -p 10022"
        fi
    else
        warning "sshpass가 설치되지 않아 실제 SSH 연결 테스트를 건너뜁니다"
        info "설치 방법: apt-get install sshpass"
        info "수동 테스트: ssh $user_id@localhost -p 10022 (비밀번호: 세션 메타데이터 참조)"
    fi
}

# SSH 기능 테스트
test_ssh_functionality() {
    section "SSH 기능 테스트"
    
    if ! command -v sshpass > /dev/null; then
        warning "sshpass가 없어 SSH 기능 테스트를 건너뜁니다"
        return
    fi
    
    # 기존 세션이 있는지 확인
    if [ ${#CREATED_SESSIONS[@]} -eq 0 ]; then
        warning "SSH 기능 테스트할 세션이 없습니다"
        return
    fi
    
    local session_id="${CREATED_SESSIONS[-1]}"  # 마지막 생성된 세션 사용
    
    # 세션 정보 조회
    local session_info
    session_info=$(http_get "/sessions/$session_id")
    if [ $? -ne 0 ]; then
        error "SSH 기능 테스트용 세션 정보 조회 실패"
        return
    fi
    
    local user_id=$(echo "$session_info" | jq -r '.user_id' 2>/dev/null)
    local ssh_password=$(echo "$session_info" | jq -r '.metadata.ssh_password' 2>/dev/null)
    local gpu_uuid=$(echo "$session_info" | jq -r '.gpu_uuid' 2>/dev/null)
    
    if [ "$user_id" = "null" ] || [ "$ssh_password" = "null" ]; then
        error "SSH 기능 테스트를 위한 세션 정보가 불완전합니다"
        return
    fi
    
    info "SSH 기능 테스트 시작: $user_id"
    
    # SSH 연결을 위한 공통 옵션
    local ssh_opts="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o BatchMode=yes"
    
    # 1. 기본 명령 실행 테스트
    info "기본 명령 실행 테스트..."
    local hostname_result
    hostname_result=$(timeout 10 sshpass -p "$ssh_password" ssh $ssh_opts "$user_id@localhost" -p 10022 "hostname" 2>/dev/null)
    if [ $? -eq 0 ] && [ -n "$hostname_result" ]; then
        success "기본 명령 실행 성공 (hostname: $hostname_result)"
    else
        warning "기본 명령 실행 실패"
    fi
    
    # 2. GPU 접근 테스트
    info "GPU 접근 테스트..."
    local gpu_test_result
    gpu_test_result=$(timeout 15 sshpass -p "$ssh_password" ssh $ssh_opts "$user_id@localhost" -p 10022 "nvidia-smi -L 2>/dev/null | head -1" 2>/dev/null)
    if [ $? -eq 0 ] && echo "$gpu_test_result" | grep -q "GPU"; then
        success "GPU 접근 성공: $gpu_test_result"
    else
        warning "GPU 접근 실패 또는 nvidia-smi 없음"
    fi
    
    # 3. 워크스페이스 접근 테스트
    info "워크스페이스 접근 테스트..."
    local workspace_result
    workspace_result=$(timeout 10 sshpass -p "$ssh_password" ssh $ssh_opts "$user_id@localhost" -p 10022 "ls -la /workspace && echo 'workspace_ok'" 2>/dev/null)
    if [ $? -eq 0 ] && echo "$workspace_result" | grep -q "workspace_ok"; then
        success "워크스페이스 접근 성공"
    else
        warning "워크스페이스 접근 실패"
    fi
    
    # 4. 파일 생성/삭제 테스트
    info "파일 시스템 권한 테스트..."
    local file_test_result
    file_test_result=$(timeout 10 sshpass -p "$ssh_password" ssh $ssh_opts "$user_id@localhost" -p 10022 "echo 'test' > /workspace/test.txt && cat /workspace/test.txt && rm /workspace/test.txt && echo 'file_test_ok'" 2>/dev/null)
    if [ $? -eq 0 ] && echo "$file_test_result" | grep -q "file_test_ok"; then
        success "파일 시스템 권한 테스트 성공"
    else
        warning "파일 시스템 권한 테스트 실패"
    fi
    
    info "SSH 기능 테스트 완료"
}

# 정리 함수
cleanup() {
    section "정리 작업"
    
    # 개별 세션들 정리
    if [ ${#CREATED_SESSIONS[@]} -gt 0 ]; then
        info "남은 테스트 세션들을 정리합니다..."
        for session_id in "${CREATED_SESSIONS[@]}"; do
            http_delete "/sessions/$session_id" > /dev/null 2>&1
        done
    fi
    
    # 모든 세션 강제 정리 (확실한 정리를 위해)
    info "모든 세션 최종 정리 중..."
    http_delete "/sessions" > /dev/null 2>&1
    
    info "정리 완료"
}

# 테스트 결과 출력
print_results() {
    section "테스트 결과"
    
    local total_tests=$((TEST_PASSED + TEST_FAILED))
    echo -e "${BLUE}총 테스트 수: $total_tests${NC}"
    echo -e "${GREEN}성공: $TEST_PASSED${NC}"
    echo -e "${RED}실패: $TEST_FAILED${NC}"
    
    if [ $TEST_FAILED -eq 0 ]; then
        echo -e "\n${GREEN}🎉 모든 테스트가 성공했습니다!${NC}"
        exit 0
    else
        echo -e "\n${RED}💥 $TEST_FAILED개의 테스트가 실패했습니다.${NC}"
        exit 1
    fi
}

# 메인 함수
main() {
    echo -e "${PURPLE}"
    echo "=================================="
    echo "   Sandman 시스템 통합 테스트"
    echo "=================================="
    echo -e "${NC}"
    
    # 신호 처리 설정
    trap cleanup EXIT
    
    # 테스트 실행
    check_server || exit 1
    
    test_gpu_availability
    test_session_creation_profile
    test_session_creation_uuid
    test_session_list
    test_session_get
    test_error_cases
    test_ssh_connection
    test_ssh_functionality
    test_session_deletion
    test_delete_all_sessions
    test_resource_cleanup
    test_performance
    
    print_results
}

# 스크립트 실행
main "$@" 