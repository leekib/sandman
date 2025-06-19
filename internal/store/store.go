package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Session struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	ContainerID string            `json:"container_id"`
	ContainerIP string            `json:"container_ip"`
	SSHPort     int               `json:"ssh_port"`
	GPUUUID     string            `json:"gpu_uuid"`
	MIGProfile  string            `json:"mig_profile"`
	TTLMinutes  int               `json:"ttl_minutes"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Metadata    map[string]string `json:"metadata"`
}

type Store interface {
	CreateSession(session *Session) error
	GetSession(id string) (*Session, error)
	GetSessionByUserID(userID string) (*Session, error)
	UpdateSession(session *Session) error
	DeleteSession(id string) error
	ListExpiredSessions() ([]*Session, error)
	ListAllSessions() ([]*Session, error)
	Close() error
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL UNIQUE,
		container_id TEXT NOT NULL,
		container_ip TEXT NOT NULL,
		ssh_port INTEGER NOT NULL DEFAULT 0,
		gpu_uuid TEXT,
		mig_profile TEXT,
		ttl_minutes INTEGER NOT NULL,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		metadata TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_expires_at ON sessions(expires_at);
	`
	_, err := s.db.Exec(query)
	return err
}

func (s *SQLiteStore) CreateSession(session *Session) error {
	metadataJSON, _ := json.Marshal(session.Metadata)

	query := `
		INSERT INTO sessions (id, user_id, container_id, container_ip, ssh_port, gpu_uuid, mig_profile, ttl_minutes, created_at, expires_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query,
		session.ID, session.UserID, session.ContainerID, session.ContainerIP, session.SSHPort,
		session.GPUUUID, session.MIGProfile, session.TTLMinutes,
		session.CreatedAt, session.ExpiresAt, string(metadataJSON))

	return err
}

func (s *SQLiteStore) GetSession(id string) (*Session, error) {
	query := `
		SELECT id, user_id, container_id, container_ip, ssh_port, gpu_uuid, mig_profile, ttl_minutes, created_at, expires_at, metadata
		FROM sessions WHERE id = ?
	`

	session := &Session{}
	var metadataJSON string

	err := s.db.QueryRow(query, id).Scan(
		&session.ID, &session.UserID, &session.ContainerID, &session.ContainerIP, &session.SSHPort,
		&session.GPUUUID, &session.MIGProfile, &session.TTLMinutes,
		&session.CreatedAt, &session.ExpiresAt, &metadataJSON)

	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(metadataJSON), &session.Metadata)
	return session, nil
}

func (s *SQLiteStore) GetSessionByUserID(userID string) (*Session, error) {
	query := `
		SELECT id, user_id, container_id, container_ip, ssh_port, gpu_uuid, mig_profile, ttl_minutes, created_at, expires_at, metadata
		FROM sessions WHERE user_id = ?
	`

	session := &Session{}
	var metadataJSON string

	err := s.db.QueryRow(query, userID).Scan(
		&session.ID, &session.UserID, &session.ContainerID, &session.ContainerIP, &session.SSHPort,
		&session.GPUUUID, &session.MIGProfile, &session.TTLMinutes,
		&session.CreatedAt, &session.ExpiresAt, &metadataJSON)

	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(metadataJSON), &session.Metadata)
	return session, nil
}

func (s *SQLiteStore) UpdateSession(session *Session) error {
	metadataJSON, _ := json.Marshal(session.Metadata)

	query := `
		UPDATE sessions SET 
			container_id = ?, container_ip = ?, ssh_port = ?, gpu_uuid = ?, mig_profile = ?,
			ttl_minutes = ?, expires_at = ?, metadata = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		session.ContainerID, session.ContainerIP, session.SSHPort, session.GPUUUID, session.MIGProfile,
		session.TTLMinutes, session.ExpiresAt, string(metadataJSON), session.ID)

	return err
}

func (s *SQLiteStore) DeleteSession(id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

func (s *SQLiteStore) ListExpiredSessions() ([]*Session, error) {
	query := `
		SELECT id, user_id, container_id, container_ip, ssh_port, gpu_uuid, mig_profile, ttl_minutes, created_at, expires_at, metadata
		FROM sessions WHERE expires_at < datetime('now')
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		var metadataJSON string

		err := rows.Scan(
			&session.ID, &session.UserID, &session.ContainerID, &session.ContainerIP, &session.SSHPort,
			&session.GPUUUID, &session.MIGProfile, &session.TTLMinutes,
			&session.CreatedAt, &session.ExpiresAt, &metadataJSON)

		if err != nil {
			continue
		}

		json.Unmarshal([]byte(metadataJSON), &session.Metadata)
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (s *SQLiteStore) ListAllSessions() ([]*Session, error) {
	query := `
		SELECT id, user_id, container_id, container_ip, ssh_port, gpu_uuid, mig_profile, ttl_minutes, created_at, expires_at, metadata
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
		var metadataJSON string

		err := rows.Scan(
			&session.ID, &session.UserID, &session.ContainerID, &session.ContainerIP, &session.SSHPort,
			&session.GPUUUID, &session.MIGProfile, &session.TTLMinutes,
			&session.CreatedAt, &session.ExpiresAt, &metadataJSON)

		if err != nil {
			continue
		}

		json.Unmarshal([]byte(metadataJSON), &session.Metadata)
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
