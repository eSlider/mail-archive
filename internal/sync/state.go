// Package sync provides email synchronisation from IMAP, POP3, and Gmail API.
package sync

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/eslider/mails/internal/model"
)

const syncDBFile = "sync.sqlite"

const createTablesSQL = `
CREATE TABLE IF NOT EXISTS sync_jobs (
	id          TEXT PRIMARY KEY,
	account_id  TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'pending',
	started_at  DATETIME,
	finished_at DATETIME,
	new_messages INTEGER NOT NULL DEFAULT 0,
	error       TEXT
);

CREATE TABLE IF NOT EXISTS sync_uids (
	account_id TEXT NOT NULL,
	folder     TEXT NOT NULL DEFAULT '',
	uid        TEXT NOT NULL,
	PRIMARY KEY (account_id, folder, uid)
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_account ON sync_jobs(account_id);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON sync_jobs(status);
`

// StateDB manages sync state in a per-user SQLite database.
type StateDB struct {
	db *sql.DB
}

// OpenStateDB opens or creates the sync state database for a user.
func OpenStateDB(usersDir, userID string) (*StateDB, error) {
	dbPath := filepath.Join(usersDir, userID, syncDBFile)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sync db: %w", err)
	}

	if _, err := db.Exec(createTablesSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init sync db: %w", err)
	}

	return &StateDB{db: db}, nil
}

// Close releases the database connection.
func (s *StateDB) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// CreateJob inserts a new sync job record.
func (s *StateDB) CreateJob(accountID string) (*model.SyncJob, error) {
	job := model.SyncJob{
		ID:        model.NewID(),
		AccountID: accountID,
		Status:    model.SyncStatusPending,
		StartedAt: time.Now(),
	}

	_, err := s.db.Exec(
		`INSERT INTO sync_jobs (id, account_id, status, started_at) VALUES (?, ?, ?, ?)`,
		job.ID, job.AccountID, job.Status, job.StartedAt,
	)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateJob updates a sync job's status and results.
func (s *StateDB) UpdateJob(job *model.SyncJob) error {
	_, err := s.db.Exec(
		`UPDATE sync_jobs SET status = ?, finished_at = ?, new_messages = ?, error = ? WHERE id = ?`,
		job.Status, job.FinishedAt, job.NewMessages, job.Error, job.ID,
	)
	return err
}

// LastJob returns the most recent sync job for an account.
func (s *StateDB) LastJob(accountID string) (*model.SyncJob, error) {
	row := s.db.QueryRow(
		`SELECT id, account_id, status, started_at, finished_at, new_messages, error
		 FROM sync_jobs WHERE account_id = ? ORDER BY started_at DESC LIMIT 1`,
		accountID,
	)

	var job model.SyncJob
	err := row.Scan(&job.ID, &job.AccountID, &job.Status, &job.StartedAt,
		&job.FinishedAt, &job.NewMessages, &job.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// IsUIDSynced checks whether a UID has been synced for an account+folder.
func (s *StateDB) IsUIDSynced(accountID, folder, uid string) bool {
	var count int
	s.db.QueryRow(
		`SELECT COUNT(*) FROM sync_uids WHERE account_id = ? AND folder = ? AND uid = ?`,
		accountID, folder, uid,
	).Scan(&count)
	return count > 0
}

// MarkUIDSynced records a UID as synced.
func (s *StateDB) MarkUIDSynced(accountID, folder, uid string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO sync_uids (account_id, folder, uid) VALUES (?, ?, ?)`,
		accountID, folder, uid,
	)
	return err
}

// SyncedUIDs returns all synced UIDs for an account+folder.
func (s *StateDB) SyncedUIDs(accountID, folder string) (map[string]bool, error) {
	rows, err := s.db.Query(
		`SELECT uid FROM sync_uids WHERE account_id = ? AND folder = ?`,
		accountID, folder,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uids := make(map[string]bool)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		uids[uid] = true
	}
	return uids, nil
}
