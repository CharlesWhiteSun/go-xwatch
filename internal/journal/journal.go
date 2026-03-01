package journal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"time"

	_ "modernc.org/sqlite"
)

// Entry represents a filesystem event persisted to the journal.
type Entry struct {
	TS    time.Time
	Op    string
	Path  string
	IsDir bool
	Size  int64
}

// Journal wraps SQLite with AES-GCM payload encryption.
type Journal struct {
	db   *sql.DB
	aead cipher.AEAD
}

// Open opens/creates the journal database at dbPath, ensures schema, and sets WAL mode.
func Open(dbPath string, key []byte) (*Journal, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Journal{db: db, aead: aead}, nil
}

func initSchema(db *sql.DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL,
		payload BLOB NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);`
	_, err := db.Exec(stmt)
	return err
}

// Close closes the underlying database.
func (j *Journal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

// Append writes entries in a single transaction.
func (j *Journal) Append(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events(ts, payload) VALUES (?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		payload, err := j.encryptEntry(e)
		if err != nil {
			tx.Rollback()
			return err
		}
		ts := e.TS.UnixMilli()
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		if _, err := stmt.ExecContext(ctx, ts, payload); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// List returns up to limit entries ordered by ts ascending (decrypting payloads).
func (j *Journal) List(ctx context.Context, limit int) ([]Entry, error) {
	return j.Query(ctx, time.Time{}, time.Time{}, limit)
}

// Query returns entries filtered by since/until and limited.
func (j *Journal) Query(ctx context.Context, since, until time.Time, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	var args []any
	clause := ""
	if !since.IsZero() {
		clause += " AND ts >= ?"
		args = append(args, since.UnixMilli())
	}
	if !until.IsZero() {
		clause += " AND ts < ?"
		args = append(args, until.UnixMilli())
	}
	args = append(args, limit)
	query := `SELECT ts, payload FROM events WHERE 1=1` + clause + ` ORDER BY ts ASC LIMIT ?`
	rows, err := j.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var ts int64
		var blob []byte
		if err := rows.Scan(&ts, &blob); err != nil {
			return nil, err
		}
		e, err := j.decryptEntry(blob)
		if err != nil {
			return nil, err
		}
		e.TS = time.UnixMilli(ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Count returns total number of rows in events.
func (j *Journal) Count(ctx context.Context) (int64, error) {
	row := j.db.QueryRowContext(ctx, `SELECT count(*) FROM events`)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (j *Journal) encryptEntry(e Entry) ([]byte, error) {
	plaintext, err := json.Marshal(struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}{e.Op, e.Path, e.IsDir, e.Size})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, j.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return j.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (j *Journal) decryptEntry(data []byte) (Entry, error) {
	ns := j.aead.NonceSize()
	if len(data) < ns {
		return Entry{}, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:ns], data[ns:]
	plaintext, err := j.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return Entry{}, err
	}
	var out Entry
	if err := json.Unmarshal(plaintext, &out); err != nil {
		return Entry{}, err
	}
	return out, nil
}
