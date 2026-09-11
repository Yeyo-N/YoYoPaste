package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

// Item is a clipboard entry.
type Item struct {
	ID      string
	Kind    string // "text" only (file transfer removed per YYP-068)
	Mime    string
	Name    string
	Size    int64
	SHA256  string
	Origin  string
	Created time.Time
	Inline  []byte
}

// Store persists items and outbox.
type Store struct {
	db       *sql.DB
	dir      string
	putCount int
}

// Open creates dir 0700, opens db file 0600 on Unix, relies on %LOCALAPPDATA% ACL on Windows (YYP-056).
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir store dir: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0700); err != nil {
			return nil, fmt.Errorf("chmod store dir: %w", err)
		}
	}
	dbPath := filepath.Join(dir, "yoyopaste.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dbPath, 0600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("chmod db: %w", err)
		}
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
			created INTEGER NOT NULL,
			inline BLOB
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
	var createdType string
	err = db.QueryRow(`SELECT type FROM pragma_table_info('items') WHERE name='created'`).Scan(&createdType)
	if err == nil && createdType == "TEXT" {
		_, err = db.Exec(`
			CREATE TABLE items_new(
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				mime TEXT NOT NULL,
				name TEXT NOT NULL,
				size INTEGER NOT NULL,
				sha256 TEXT NOT NULL,
				origin TEXT NOT NULL,
				created INTEGER NOT NULL,
				inline BLOB
			);
		`)
		if err != nil {
			return fmt.Errorf("migrate create new: %w", err)
		}
		rows, err := db.Query(`SELECT id, kind, mime, name, size, sha256, origin, created, inline FROM items`)
		if err != nil {
			// Try old schema with blob_path for backwards compat
			rows, err = db.Query(`SELECT id, kind, mime, name, size, sha256, origin, created, inline, blob_path FROM items`)
			if err != nil {
				return fmt.Errorf("migrate select old: %w", err)
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id, kind, mime, name, sha256, origin, createdStr, blobPath string
				var size int64
				var inline []byte
				if err := rows.Scan(&id, &kind, &mime, &name, &size, &sha256, &origin, &createdStr, &inline, &blobPath); err != nil {
					return err
				}
				var createdInt int64
				if t, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
					createdInt = t.UnixNano()
				} else if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
					createdInt = t.UnixNano()
				}
				_, err = db.Exec(`INSERT INTO items_new(id, kind, mime, name, size, sha256, origin, created, inline) VALUES(?,?,?,?,?,?,?,?,?)`,
					id, kind, mime, name, size, sha256, origin, createdInt, inline)
				if err != nil {
					return err
				}
			}
		} else {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id, kind, mime, name, sha256, origin, createdStr string
				var size int64
				var inline []byte
				if err := rows.Scan(&id, &kind, &mime, &name, &size, &sha256, &origin, &createdStr, &inline); err != nil {
					return err
				}
				var createdInt int64
				if t, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
					createdInt = t.UnixNano()
				} else if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
					createdInt = t.UnixNano()
				}
				_, err = db.Exec(`INSERT INTO items_new(id, kind, mime, name, size, sha256, origin, created, inline) VALUES(?,?,?,?,?,?,?,?,?)`,
					id, kind, mime, name, size, sha256, origin, createdInt, inline)
				if err != nil {
					return err
				}
			}
		}
		if _, err := db.Exec(`DROP TABLE items`); err != nil {
			return err
		}
		if _, err := db.Exec(`ALTER TABLE items_new RENAME TO items`); err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_items_sha256 ON items(sha256)`); err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_items_created ON items(created DESC)`); err != nil {
			return err
		}
	}
	// Drop old blob_path column if it still exists (YYP-068)
	_, _ = db.Exec(`ALTER TABLE items DROP COLUMN blob_path`)
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
		INSERT OR IGNORE INTO items(id, kind, mime, name, size, sha256, origin, created, inline)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Kind, it.Mime, it.Name, it.Size, it.SHA256, it.Origin, it.Created.UnixNano(), it.Inline,
	)
	if err != nil {
		return fmt.Errorf("store put %s: %w", it.ID, err)
	}
	s.putCount++
	if s.putCount%10 == 0 {
		_ = s.EnforceRetention(500)
	}
	return nil
}

// Recent returns newest n items.
func (s *Store) Recent(n int) ([]Item, error) {
	rows, err := s.db.Query(`SELECT id, kind, mime, name, size, sha256, origin, created, inline FROM items ORDER BY created DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("recent query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Item
	for rows.Next() {
		var it Item
		var createdNs int64
		if err := rows.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &createdNs, &it.Inline); err != nil {
			return nil, err
		}
		it.Created = time.Unix(0, createdNs)
		out = append(out, it)
	}
	return out, rows.Err()
}

// BySHA finds item by sha256 for dedupe.
func (s *Store) BySHA(sha string) (Item, bool, error) {
	row := s.db.QueryRow(`SELECT id, kind, mime, name, size, sha256, origin, created, inline FROM items WHERE sha256 = ? LIMIT 1`, sha)
	var it Item
	var createdNs int64
	if err := row.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &createdNs, &it.Inline); err != nil {
		if err == sql.ErrNoRows {
			return Item{}, false, nil
		}
		return Item{}, false, fmt.Errorf("by sha: %w", err)
	}
	it.Created = time.Unix(0, createdNs)
	return it, true, nil
}

// Get returns item by id.
func (s *Store) Get(id string) (Item, bool, error) {
	row := s.db.QueryRow(`SELECT id, kind, mime, name, size, sha256, origin, created, inline FROM items WHERE id = ?`, id)
	var it Item
	var createdNs int64
	if err := row.Scan(&it.ID, &it.Kind, &it.Mime, &it.Name, &it.Size, &it.SHA256, &it.Origin, &createdNs, &it.Inline); err != nil {
		if err == sql.ErrNoRows {
			return Item{}, false, nil
		}
		return Item{}, false, err
	}
	it.Created = time.Unix(0, createdNs)
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
	defer func() { _ = rows.Close() }()
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

// EnforceRetention evicts oldest items beyond the count limit (YYP-068: no blobs, count only).
func (s *Store) EnforceRetention(maxItems int) error {
	if maxItems <= 0 {
		maxItems = 500
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		return err
	}
	if count <= maxItems {
		return nil
	}
	rows, err := s.db.Query(`SELECT id FROM items ORDER BY created ASC LIMIT ?`, count-maxItems)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DELETE FROM items WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
