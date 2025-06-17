.PHONY: build run test clean docker up down logs

# 기본 설정
APP_NAME := orchestrator
DOCKER_COMPOSE_FILE := docker/docker-compose.yml

# Go 빌드
build:
	@echo "🔨 오케스트레이터 빌드 중..."
	CGO_ENABLED=1 go build -o bin/$(APP_NAME) ./cmd/orchestrator

# 로컬 실행
run: build
	@echo "🚀 오케스트레이터 실행 중..."
	./bin/$(APP_NAME)

# 테스트
test:
	@echo "🧪 테스트 실행 중..."
	go test -v ./...

# 정리
clean:
	@echo "🧹 빌드 파일 정리 중..."
	rm -rf bin/
	docker system prune -f

# Docker 이미지 빌드
docker:
	@echo "🐳 Docker 이미지 빌드 중..."
	docker build -f docker/orchestrator.Dockerfile -t gpu-orchestrator .
	docker build -f docker/sshpiper.Dockerfile -t sshpiper docker/

# 전체 시스템 시작
up:
	@echo "🌟 GPU 컨테이너 오케스트레이터 시스템 시작 중..."
	@echo "1️⃣ worknet 네트워크 생성..."
	@docker network inspect worknet >/dev/null 2>&1 || docker network create worknet --subnet 172.30.0.0/16
	@echo "2️⃣ 워크스페이스 디렉토리 생성..."
	@sudo mkdir -p /srv/workspaces && sudo chmod 755 /srv/workspaces
	@echo "3️⃣ Docker Compose 시작..."
	docker-compose -f $(DOCKER_COMPOSE_FILE) up -d
	@echo "✅ 시스템 시작 완료!"
	@echo ""
	@echo "📋 접속 정보:"
	@echo "  • API 서버: http://localhost:8080"
	@echo "  • SSH 게이트웨이: ssh user@localhost"
	@echo "  • Prometheus: http://localhost:9090"
	@echo "  • Grafana: http://localhost:3000 (admin/admin)"

# 전체 시스템 중지
down:
	@echo "🛑 GPU 컨테이너 오케스트레이터 시스템 중지 중..."
	docker-compose -f $(DOCKER_COMPOSE_FILE) down

# 로그 확인
logs:
	@echo "📋 시스템 로그:"
	docker-compose -f $(DOCKER_COMPOSE_FILE) logs -f

# 개발 환경 설정
dev-setup:
	@echo "🛠️ 개발 환경 설정 중..."
	@echo "1️⃣ Go 의존성 다운로드..."
	go mod download
	@echo "2️⃣ 개발용 디렉토리 생성..."
	mkdir -p bin/ logs/ data/
	@echo "3️⃣ Git hooks 설정..."
	@if [ -d .git ]; then \
		echo "#!/bin/sh\nmake test" > .git/hooks/pre-commit; \
		chmod +x .git/hooks/pre-commit; \
	fi
	@echo "✅ 개발 환경 설정 완료!"

# API 테스트
test-api:
	@echo "🧪 API 테스트 실행 중..."
	@echo "1️⃣ 헬스체크..."
	@curl -s http://localhost:8080/api/v1/healthz | jq .
	@echo "\n2️⃣ GPU 정보 조회..."
	@curl -s http://localhost:8080/api/v1/gpus | jq .
	@echo "\n3️⃣ MIG 프로파일 조회..."
	@curl -s http://localhost:8080/api/v1/gpus/profiles | jq .

# 세션 생성 테스트
test-session:
	@echo "🧪 세션 생성 테스트..."
	@curl -X POST http://localhost:8080/api/v1/sessions \
		-H "Content-Type: application/json" \
		-d '{"user_id":"testuser","mig_profile":"3g.20gb","ttl_minutes":60}' | jq .

# 의존성 업데이트
update-deps:
	@echo "📦 의존성 업데이트 중..."
	go get -u ./...
	go mod tidy

# 도커 정리
docker-clean:
	@echo "🧹 Docker 정리 중..."
	docker-compose -f $(DOCKER_COMPOSE_FILE) down -v
	docker system prune -a -f
	docker volume prune -f

# 도움말
help:
	@echo "🎯 GPU 컨테이너 오케스트레이터 Makefile"
	@echo ""
	@echo "📋 사용 가능한 명령:"
	@echo "  build         - Go 바이너리 빌드"
	@echo "  run           - 로컬에서 실행"
	@echo "  test          - 테스트 실행"
	@echo "  clean         - 빌드 파일 정리"
	@echo "  docker        - Docker 이미지 빌드"
	@echo "  up            - 전체 시스템 시작"
	@echo "  down          - 전체 시스템 중지"
	@echo "  logs          - 로그 확인"
	@echo "  dev-setup     - 개발 환경 설정"
	@echo "  test-api      - API 테스트"
	@echo "  test-session  - 세션 생성 테스트"
	@echo "  update-deps   - 의존성 업데이트"
	@echo "  docker-clean  - Docker 정리"
	@echo "  help          - 이 도움말" 