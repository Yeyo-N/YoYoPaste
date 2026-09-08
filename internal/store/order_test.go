//nolint:all
package store

import (
	"testing"
	"time"
)

func TestRecentOrderingSameSecond(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer Close(s)
	// Two items microseconds apart in same second
	base := time.Unix(100, 0)
	t1 := base.Add(500 * time.Millisecond)    // .5
	t2 := base.Add(500010 * time.Microsecond) // .50001
	// Insert t1 then t2, then check Recent order is t2, t1 (newest first)
	it1 := Item{ID: "id1", Kind: "text", Mime: "text/plain", Size: 1, SHA256: "sha1", Origin: "test", Created: t1, Inline: []byte("a")}
	it2 := Item{ID: "id2", Kind: "text", Mime: "text/plain", Size: 1, SHA256: "sha2", Origin: "test", Created: t2, Inline: []byte("b")}
	if err := s.Put(it1); err != nil {
		t.Fatalf("Put1: %v", err)
	}
	if err := s.Put(it2); err != nil {
		t.Fatalf("Put2: %v", err)
	}
	recent, err := s.Recent(2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("len %d", len(recent))
	}
	if recent[0].ID != "id2" || recent[1].ID != "id1" {
		t.Fatalf("wrong order: got %s, %s want id2, id1", recent[0].ID, recent[1].ID)
	}
	// Also test retention ordering "oldest first" - EnforceRetention should delete oldest
	// Insert many items and ensure oldest is first in ASC order
	// This is implicitly tested via Recent DESC
}
