package store

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestOpenAndPut(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer Close(s)

	// check db file mode 0600
	info, err := os.Stat(dir + "/yoyopaste.db")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("db perm %o want 0600", info.Mode().Perm())
	}

	it := Item{ID: "01J8Z", Kind: "text", Mime: "text/plain", Name: "", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("hello"))), Origin: "test", Created: time.Now(), Inline: []byte("hello")}
	if err := s.Put(it); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// idempotent
	if err := s.Put(it); err != nil {
		t.Fatalf("Put2: %v", err)
	}
	n, _ := s.Count()
	if n != 1 {
		t.Fatalf("count %d want 1", n)
	}
	// reopen preserves
	Close(s)
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer Close(s2)
	n, _ = s2.Count()
	if n != 1 {
		t.Fatalf("reopen count %d want 1", n)
	}
	// Recent
	items, err := s2.Recent(10)
	if err != nil || len(items) != 1 {
		t.Fatalf("Recent: %v len %d", err, len(items))
	}
	// BySHA
	got, ok, err := s2.BySHA(it.SHA256)
	if err != nil || !ok || got.ID != it.ID {
		t.Fatalf("BySHA: %v ok %v got %v", err, ok, got)
	}
	_, ok, _ = s2.BySHA("nope")
	if ok {
		t.Fatal("expected not found")
	}
}
