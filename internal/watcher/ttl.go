package watcher

import (
	"log"
	"time"

	"github.com/sandman/gpu-ssh-gateway/internal/session"
)

type TTLWatcher struct {
	sessionService *session.Service
	interval       time.Duration
	stopChan       chan struct{}
	running        bool
}

func NewTTLWatcher(sessionService *session.Service, interval time.Duration) *TTLWatcher {
	return &TTLWatcher{
		sessionService: sessionService,
		interval:       interval,
		stopChan:       make(chan struct{}),
	}
}

func (w *TTLWatcher) Start() {
	if w.running {
		return
	}

	w.running = true
	go w.watch()
	log.Printf("⏰ TTL 감시자 시작됨 (간격: %v)", w.interval)
}

func (w *TTLWatcher) Stop() {
	if !w.running {
		return
	}

	w.running = false
	close(w.stopChan)
	log.Println("⏰ TTL 감시자 중지됨")
}

func (w *TTLWatcher) watch() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.sessionService.CleanupExpiredSessions(); err != nil {
				log.Printf("⚠️ 만료된 세션 정리 중 오류: %v", err)
			}
		case <-w.stopChan:
			return
		}
	}
} 