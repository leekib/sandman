package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sandman/gpu-ssh-gateway/internal/gpu"
	"github.com/sandman/gpu-ssh-gateway/internal/session"
	"github.com/sandman/gpu-ssh-gateway/internal/store"
)

type Server struct {
	sessionService *session.Service
	gpuManager     *gpu.Manager
}

func NewServer(sessionService *session.Service, gpuManager *gpu.Manager) *Server {
	return &Server{
		sessionService: sessionService,
		gpuManager:     gpuManager,
	}
}

// CORS 미들웨어 - 최대한 허용
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 모든 오리진 허용
		c.Header("Access-Control-Allow-Origin", "*")

		// 모든 HTTP 메서드 허용
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")

		// 모든 헤더 허용
		c.Header("Access-Control-Allow-Headers", "*")

		// 인증 정보 허용
		c.Header("Access-Control-Allow-Credentials", "true")

		// Preflight 요청에 대한 최대 캐시 시간 (24시간)
		c.Header("Access-Control-Max-Age", "86400")

		// 클라이언트가 접근할 수 있는 응답 헤더
		c.Header("Access-Control-Expose-Headers", "*")

		// OPTIONS 요청 (preflight)에 대한 처리
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func (s *Server) SetupRoutes() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 미들웨어 추가: 로거, 복구, CORS
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware())

	// Health check
	r.GET("/healthz", s.healthCheck)

	// Session management
	r.POST("/sessions", s.createSession)
	r.GET("/sessions/:id", s.getSession)
	r.DELETE("/sessions/:id", s.deleteSession)
	r.GET("/sessions", s.listSessions)
	r.DELETE("/sessions", s.deleteAllSessions)

	// GPU information
	r.GET("/gpus", s.getGPUInfo)
	r.GET("/gpus/profiles", s.getMIGProfiles)
	r.GET("/gpus/available", s.getAvailableMIGInstances)

	return r
}

func (s *Server) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "gpu-ssh-gateway-orchestrator",
	})
}

func (s *Server) createSession(c *gin.Context) {
	var req session.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "잘못된 요청 형식: " + err.Error(),
		})
		return
	}

	response, err := s.sessionService.CreateSession(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (s *Server) getSession(c *gin.Context) {
	sessionID := c.Param("id")

	session, err := s.sessionService.GetSession(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "세션을 찾을 수 없습니다: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, session)
}

func (s *Server) deleteSession(c *gin.Context) {
	sessionID := c.Param("id")

	if err := s.sessionService.DeleteSession(sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "세션 삭제 실패: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "세션이 성공적으로 삭제되었습니다",
	})
}

func (s *Server) listSessions(c *gin.Context) {
	sessions, err := s.sessionService.ListAllSessions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "세션 목록 조회 실패: " + err.Error(),
		})
		return
	}

	// sessions가 nil인 경우 빈 슬라이스로 초기화
	if sessions == nil {
		sessions = []*store.Session{}
	}

	c.JSON(http.StatusOK, sessions)
}

func (s *Server) deleteAllSessions(c *gin.Context) {
	if err := s.sessionService.DeleteAllSessions(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "모든 세션 삭제 실패: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "모든 세션이 성공적으로 삭제되었습니다",
	})
}

func (s *Server) getGPUInfo(c *gin.Context) {
	gpuInfo := s.gpuManager.GetGPUInfo()

	c.JSON(http.StatusOK, gin.H{
		"gpus":  gpuInfo,
		"count": len(gpuInfo),
	})
}

func (s *Server) getMIGProfiles(c *gin.Context) {
	profiles := s.gpuManager.GetAvailableProfiles()

	c.JSON(http.StatusOK, gin.H{
		"profiles": profiles,
	})
}

func (s *Server) getAvailableMIGInstances(c *gin.Context) {
	availableInstances := s.gpuManager.GetAvailableMIGInstances()

	c.JSON(http.StatusOK, gin.H{
		"available_instances": availableInstances,
		"count":               len(availableInstances),
	})
}
