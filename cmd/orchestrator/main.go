package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sandman/gpu-ssh-gateway/internal/api"
	"github.com/sandman/gpu-ssh-gateway/internal/docker"
	"github.com/sandman/gpu-ssh-gateway/internal/gpu"
	"github.com/sandman/gpu-ssh-gateway/internal/session"
	"github.com/sandman/gpu-ssh-gateway/internal/sshpiper"
	"github.com/sandman/gpu-ssh-gateway/internal/store"
	"github.com/sandman/gpu-ssh-gateway/internal/watcher"
)

var (
	port = flag.String("port", "8080", "API 서버 포트")
	dbPath = flag.String("db", "/var/lib/orchestrator/sessions.db", "SQLite 데이터베이스 파일 경로")
	piperConfigPath = flag.String("piper-config", "/etc/sshpiper/pipe.yaml", "SSHPiper 설정 파일 경로")
	workspaceRoot = flag.String("workspace-root", "/srv/workspaces", "사용자 워크스페이스 루트 디렉토리")
)

func main() {
	flag.Parse()

	// 로그 설정
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("🚀 GPU SSH Gateway Orchestrator 시작 중...")

	// 데이터베이스 초기화
	log.Println("📦 데이터베이스 초기화 중...")
	db, err := store.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatalf("데이터베이스 초기화 실패: %v", err)
	}
	defer db.Close()

	// GPU 관리자 초기화
	log.Println("🎮 GPU 관리자 초기화 중...")
	gpuManager, err := gpu.NewManager()
	if err != nil {
		log.Fatalf("GPU 관리자 초기화 실패: %v", err)
	}
	defer gpuManager.Shutdown()

	// Docker 클라이언트 초기화
	log.Println("🐳 Docker 클라이언트 초기화 중...")
	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Fatalf("Docker 클라이언트 초기화 실패: %v", err)
	}
	defer dockerClient.Close()

	// SSHPiper 관리자 초기화
	log.Println("🔀 SSHPiper 관리자 초기화 중...")
	piperManager := sshpiper.NewManager(*piperConfigPath)

	// 세션 서비스 초기화
	sessionService := session.NewService(db, dockerClient, gpuManager, piperManager, *workspaceRoot)

	// TTL 감시자 시작
	log.Println("⏰ TTL 감시자 시작 중...")
	ttlWatcher := watcher.NewTTLWatcher(sessionService, 1*time.Minute)
	ttlWatcher.Start()
	defer ttlWatcher.Stop()

	// API 서버 초기화
	log.Println("🌐 API 서버 초기화 중...")
	apiServer := api.NewServer(sessionService, gpuManager)
	
	// HTTP 서버 설정
	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: apiServer.SetupRoutes(),
	}

	// 서버 시작
	go func() {
		log.Printf("🎯 API 서버가 포트 %s에서 시작되었습니다", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API 서버 시작 실패: %v", err)
		}
	}()

	// 우아한 종료 처리
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("🛑 Orchestrator 종료 중...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("서버 종료 중 오류: %v", err)
	}

	log.Println("✅ Orchestrator가 성공적으로 종료되었습니다")
} 