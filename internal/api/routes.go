package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sandman/gpu-orchestrator/internal/gpu"
	"github.com/sandman/gpu-orchestrator/internal/session"
	"github.com/sirupsen/logrus"
)

// SessionManager 세션 관리자 인터페이스
type SessionManager interface {
	CreateSession(req *session.CreateRequest) (*session.SessionResponse, error)
	GetSession(sessionID string) (*session.SessionResponse, error)
	DeleteSession(sessionID string) error
	GetAllSessions() ([]*session.SessionResponse, error)
	GetSessionStats() (map[string]interface{}, error)
}

// GPUManager GPU 관리자 인터페이스
type GPUManager interface {
	GetGPUInfo() []*gpu.GPUInfo
	GetAvailableProfiles() []string
}

// SetupRoutes REST API 라우트 설정
func SetupRoutes(router *gin.Engine, sessionManager SessionManager, gpuManager GPUManager) {
	log := logrus.WithField("component", "api")

	// 미들웨어 설정
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())

	// API 그룹
	api := router.Group("/api/v1")
	{
		// 헬스체크
		api.GET("/healthz", healthCheck)

		// 세션 관리
		sessions := api.Group("/sessions")
		{
			sessions.POST("", createSessionHandler(sessionManager))
			sessions.GET("", getAllSessionsHandler(sessionManager))
			sessions.GET("/stats", getSessionStatsHandler(sessionManager))
			sessions.GET("/:id", getSessionHandler(sessionManager))
			sessions.DELETE("/:id", deleteSessionHandler(sessionManager))
		}

		// GPU 정보
		gpus := api.Group("/gpus")
		{
			gpus.GET("", getGPUsHandler(gpuManager))
			gpus.GET("/profiles", getProfilesHandler(gpuManager))
		}
	}

	log.Info("REST API 라우트 설정 완료")
}

// corsMiddleware CORS 미들웨어
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// healthCheck 헬스체크 핸들러
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": "2024-01-01T00:00:00Z",
		"version":   "1.0.0",
	})
}

// createSessionHandler 세션 생성 핸들러
func createSessionHandler(manager SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req session.CreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "잘못된 요청 형식",
				"details": err.Error(),
			})
			return
		}

		// 입력 검증
		if req.UserID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "user_id는 필수입니다",
			})
			return
		}

		if req.MIGProfile == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "mig_profile은 필수입니다",
			})
			return
		}

		response, err := manager.CreateSession(&req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "세션 생성 실패",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusCreated, response)
	}
}

// getSessionHandler 세션 조회 핸들러
func getSessionHandler(manager SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "session ID는 필수입니다",
			})
			return
		}

		response, err := manager.GetSession(sessionID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "세션을 찾을 수 없습니다",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

// deleteSessionHandler 세션 삭제 핸들러
func deleteSessionHandler(manager SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "session ID는 필수입니다",
			})
			return
		}

		if err := manager.DeleteSession(sessionID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "세션 삭제 실패",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "세션이 성공적으로 삭제되었습니다",
		})
	}
}

// getAllSessionsHandler 모든 세션 조회 핸들러
func getAllSessionsHandler(manager SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessions, err := manager.GetAllSessions()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "세션 목록 조회 실패",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"sessions": sessions,
			"count":    len(sessions),
		})
	}
}

// getSessionStatsHandler 세션 통계 조회 핸들러
func getSessionStatsHandler(manager SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := manager.GetSessionStats()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "세션 통계 조회 실패",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, stats)
	}
}

// getGPUsHandler GPU 정보 조회 핸들러
func getGPUsHandler(manager GPUManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		gpus := manager.GetGPUInfo()
		c.JSON(http.StatusOK, gin.H{
			"gpus":  gpus,
			"count": len(gpus),
		})
	}
}

// getProfilesHandler MIG 프로파일 목록 조회 핸들러
func getProfilesHandler(manager GPUManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		profiles := manager.GetAvailableProfiles()
		c.JSON(http.StatusOK, gin.H{
			"profiles": profiles,
			"count":    len(profiles),
		})
	}
}
