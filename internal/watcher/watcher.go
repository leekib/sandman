package watcher

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// SessionManager 세션 관리자 인터페이스
type SessionManager interface {
	CleanupExpiredSessions() error
}

// Start TTL 와처 시작
func Start(ctx context.Context, sessionManager SessionManager, interval time.Duration) {
	log := logrus.WithField("component", "ttl-watcher")
	log.Infof("TTL 와처 시작 (간격: %v)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("TTL 와처 종료")
			return
		case <-ticker.C:
			log.Debug("만료된 세션 정리 시작")
			if err := sessionManager.CleanupExpiredSessions(); err != nil {
				log.Errorf("만료된 세션 정리 실패: %v", err)
			} else {
				log.Debug("만료된 세션 정리 완료")
			}
		}
	}
}
