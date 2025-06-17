package store

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Session 세션 정보 구조체
type Session struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	ContainerID string    `json:"container_id"`
	GPUUUID     string    `json:"gpu_uuid"`
	ContainerIP string    `json:"container_ip"`
	CreatedAt   time.Time `json:"created_at"`
	TTL         int       `json:"ttl_minutes"`
	Status      string    `json:"status"` // "running", "stopped", "failed"
}

// Store 데이터베이스 스토어
type Store struct {
	db *sql.DB
}

// New 새로운 스토어 인스턴스 생성
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.initTables(); err != nil {
		return nil, err
	}

	return store, nil
}

// Close 데이터베이스 연결 종료
func (s *Store) Close() error {
	return s.db.Close()
}

// initTables 테이블 초기화
func (s *Store) initTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		container_id TEXT,
		gpu_uuid TEXT,
		container_ip TEXT,
		created_at DATETIME NOT NULL,
		ttl_minutes INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'running'
	);
	
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
	CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at);
	`

	_, err := s.db.Exec(query)
	return err
}

// CreateSession 새 세션 생성
func (s *Store) CreateSession(session *Session) error {
	query := `
	INSERT INTO sessions (id, user_id, container_id, gpu_uuid, container_ip, created_at, ttl_minutes, status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query, session.ID, session.UserID, session.ContainerID,
		session.GPUUUID, session.ContainerIP, session.CreatedAt, session.TTL, session.Status)
	return err
}

// GetSession 세션 ID로 세션 조회
func (s *Store) GetSession(id string) (*Session, error) {
	query := `SELECT id, user_id, container_id, gpu_uuid, container_ip, created_at, ttl_minutes, status FROM sessions WHERE id = ?`

	row := s.db.QueryRow(query, id)
	session := &Session{}

	err := row.Scan(&session.ID, &session.UserID, &session.ContainerID,
		&session.GPUUUID, &session.ContainerIP, &session.CreatedAt, &session.TTL, &session.Status)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// GetSessionByUserID 사용자 ID로 활성 세션 조회
func (s *Store) GetSessionByUserID(userID string) (*Session, error) {
	query := `SELECT id, user_id, container_id, gpu_uuid, container_ip, created_at, ttl_minutes, status 
	FROM sessions WHERE user_id = ? AND status = 'running' ORDER BY created_at DESC LIMIT 1`

	row := s.db.QueryRow(query, userID)
	session := &Session{}

	err := row.Scan(&session.ID, &session.UserID, &session.ContainerID,
		&session.GPUUUID, &session.ContainerIP, &session.CreatedAt, &session.TTL, &session.Status)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// UpdateSession 세션 업데이트
func (s *Store) UpdateSession(session *Session) error {
	query := `
	UPDATE sessions SET container_id = ?, gpu_uuid = ?, container_ip = ?, status = ?
	WHERE id = ?
	`

	_, err := s.db.Exec(query, session.ContainerID, session.GPUUUID, session.ContainerIP, session.Status, session.ID)
	return err
}

// DeleteSession 세션 삭제
func (s *Store) DeleteSession(id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// GetExpiredSessions TTL이 만료된 세션들 조회
func (s *Store) GetExpiredSessions() ([]*Session, error) {
	query := `
	SELECT id, user_id, container_id, gpu_uuid, container_ip, created_at, ttl_minutes, status
	FROM sessions 
	WHERE status = 'running' AND datetime(created_at, '+' || ttl_minutes || ' minutes') < datetime('now')
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		err := rows.Scan(&session.ID, &session.UserID, &session.ContainerID,
			&session.GPUUUID, &session.ContainerIP, &session.CreatedAt, &session.TTL, &session.Status)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetAllSessions 모든 세션 조회
func (s *Store) GetAllSessions() ([]*Session, error) {
	query := `
	SELECT id, user_id, container_id, gpu_uuid, container_ip, created_at, ttl_minutes, status
	FROM sessions ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		err := rows.Scan(&session.ID, &session.UserID, &session.ContainerID,
			&session.GPUUUID, &session.ContainerIP, &session.CreatedAt, &session.TTL, &session.Status)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}
