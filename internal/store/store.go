package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Item is a clipboard entry.
type Item struct {
	ID       string
	Kind     string // "text" | "file"
	Mime     string
	Name     string
	Size     int64
	SHA256   string
	Origin   string
	Created  time.Time
	Inline   []byte
	BlobPath string
}

// Store persists items and outbox.
type Store struct {
	db  *sql.DB
	dir string
}

// Open creates dir 0700, opens db file 0600, migrates.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir store dir: %w", err)
	}
	// Ensure dir mode 0700 (MkdirAll may leave existing perms)
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("chmod store dir: %w", err)
	}
	dbPath := filepath.Join(dir, "yoyopaste.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Restrict to single connection for SQLite
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	// Ensure file mode 0600
	if err := os.Chmod(dbPath, 0600); err != nil {
		db.Close()
		return nil, fmt.Errorf("chmod db: %w", err)
	}
	return &Store{db: db, dir: dir}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS items(
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			mime TEXT NOT NULL,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			sha256 TEXT NOT NULL,
			origin TEXT NOT NULL,
			created TEXT NOT NULL,
			inline BLOB,
			blob_path TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_items_sha256 ON items(sha256);
		CREATE INDEX IF NOT EXISTS idx_items_created ON items(created DESC);
		CREATE TABLE IF NOT EXISTS outbox(
			item_id TEXT NOT NULL,
			peer_id TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_try TEXT,
			PRIMARY KEY(item_id, peer_id)
		);
		CREATE TABLE IF NOT EXISTS settings(
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close closes the database.
func Close(s *Store) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Put inserts item idempotently.
func (s *Store) Put(it Item) error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO items(id, kind, mime, name, size, sha256, origin, created, inline, blob_path)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Kind, it.Mime, it.Name, it.Size, it.SHA256, it.Origin, it.Created.UTC().Format(time.RFC3339Nano), it.Inline, it.BlobPath,
	)
	if err != nil {
		return fmt.Errorf("store put %s: %w", it.ID, err)
	}
	_ = s.EnforceRetention(500, 2*1024*1024*1024)
	return nil
}

// Recent returns newest n items.
func (s *Store) Recent(n int) ([]Item, error) {
	rows, err := s.db.Query(`SELECT id, kind, mime, name, size, sha256, origin, created, inline, blob_path FROM items ORDER BY created DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("recent query: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var created string
		if err := rows.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &created, &it.Inline, &it.BlobPath); err != nil {
			return nil, err
		}
		it.Created, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, it)
	}
	return out, rows.Err()
}

// BySHA finds item by sha256 for dedupe.
func (s *Store) BySHA(sha string) (Item, bool, error) {
	row := s.db.QueryRow(`SELECT id, kind, mime, name, size, sha256, origin, created, inline, blob_path FROM items WHERE sha256 = ? LIMIT 1`, sha)
	var it Item
	var created string
	if err := row.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &created, &it.Inline, &it.BlobPath); err != nil {
		if err == sql.ErrNoRows {
			return Item{}, false, nil
		}
		return Item{}, false, fmt.Errorf("by sha: %w", err)
	}
	it.Created, _ = time.Parse(time.RFC3339Nano, created)
	return it, true, nil
}

// Get returns item by id.
func (s *Store) Get(id string) (Item, bool, error) {
	row := s.db.QueryRow(`SELECT id, kind, mime, name, size, sha256, origin, created, inline, blob_path FROM items WHERE id = ?`, id)
	var it Item
	var created string
	if err := row.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &created, &it.Inline, &it.BlobPath); err != nil {
		if err == sql.ErrNoRows {
			return Item{}, false, nil
		}
		return Item{}, false, err
	}
	it.Created, _ = time.Parse(time.RFC3339Nano, created)
	return it, true, nil
}

// Count returns total items.
func (s *Store) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// BlobDir returns directory for blobs.
func (s *Store) BlobDir() string {
	return filepath.Join(s.dir, "blobs")
}

// --- settings ---

// GetSetting returns setting value or empty if not found.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// SetSetting upserts key/value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// --- outbox ---

// OutboxEntry is a queued announcement.
type OutboxEntry struct {
	ItemID   string
	PeerID   string
	Attempts int
	LastTry  *time.Time
}

// AddOutbox queues item for peer.
func (s *Store) AddOutbox(itemID, peerID string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO outbox(item_id, peer_id, attempts) VALUES(?,?,0)`, itemID, peerID)
	return err
}

// ListOutbox returns outbox entries for peer ordered oldest first (by last_try? use rowid).
func (s *Store) ListOutbox(peerID string) ([]OutboxEntry, error) {
	rows, err := s.db.Query(`SELECT item_id, peer_id, attempts, last_try FROM outbox WHERE peer_id = ? ORDER BY rowid`, peerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxEntry
	for rows.Next() {
		var e OutboxEntry
		var lastTry sql.NullString
		if err := rows.Scan(&e.ItemID, &e.PeerID, &e.Attempts, &lastTry); err != nil {
			return nil, err
		}
		if lastTry.Valid {
			t, _ := time.Parse(time.RFC3339Nano, lastTry.String)
			e.LastTry = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RemoveOutbox deletes entry.
func (s *Store) RemoveOutbox(itemID, peerID string) error {
	_, err := s.db.Exec(`DELETE FROM outbox WHERE item_id = ? AND peer_id = ?`, itemID, peerID)
	return err
}

// IncrementAttempt bumps attempt count.
func (s *Store) IncrementAttempt(itemID, peerID string) error {
	_, err := s.db.Exec(`UPDATE outbox SET attempts = attempts + 1, last_try = ? WHERE item_id = ? AND peer_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), itemID, peerID)
	return err
}

// CountOutbox returns total outbox rows.
func (s *Store) CountOutbox() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// UpdateBlobPath sets blob_path for item.
func (s *Store) UpdateBlobPath(id, path string) error {
	_, err := s.db.Exec(`UPDATE items SET blob_path = ? WHERE id = ?`, path, id)
	return err
}

// EnforceRetention evicts oldest items beyond limits (500 items / 2GB).
func (s *Store) EnforceRetention(maxItems int, maxBytes int64) error {
	if maxItems <= 0 {
		maxItems = 500
	}
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024 * 1024
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		return err
	}
	var total sql.NullInt64
	if err := s.db.QueryRow(`SELECT SUM(size) FROM items`).Scan(&total); err != nil {
		return err
	}
	totalBytes := int64(0)
	if total.Valid {
		totalBytes = total.Int64
	}
	if count <= maxItems && totalBytes <= maxBytes {
		return nil
	}
	rows, err := s.db.Query(`SELECT id, size, blob_path FROM items ORDER BY created ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type entry struct {
		id       string
		size     int64
		blobPath string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.size, &e.blobPath); err != nil {
			return err
		}
		entries = append(entries, e)
	}
	for _, e := range entries {
		if count <= maxItems && totalBytes <= maxBytes {
			break
		}
		if _, err := s.db.Exec(`DELETE FROM items WHERE id = ?`, e.id); err != nil {
			return err
		}
		if e.blobPath != "" {
			_ = os.Remove(e.blobPath)
			_ = os.Remove(e.blobPath + ".part")
		}
		count--
		totalBytes -= e.size
	}
	return nil
}
