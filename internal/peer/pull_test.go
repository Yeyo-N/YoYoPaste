package peer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
)

func TestPullVerifyAndResume(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer store.Close(st)
	content := make([]byte, 1000)
	for i := range content {
		content[i] = byte(i % 256)
	}
	h := sha256.Sum256(content)
	sha := fmt.Sprintf("%x", h[:])
	it := store.Item{ID: "pull-1", Kind: "file", Name: "f.dat", Size: 1000, SHA256: sha}
	if err := st.Put(it); err != nil {
		t.Fatal(err)
	}
	// create part with 500 bytes (simulating interrupted download)
	partPath := filepath.Join(st.BlobDir(), "pull-1.part")
	if err := os.MkdirAll(st.BlobDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, content[:500], 0600); err != nil {
		t.Fatal(err)
	}
	// append remainder as resume would
	f, _ := os.OpenFile(partPath, os.O_APPEND|os.O_WRONLY, 0600)
	if _, err := f.Write(content[500:]); err != nil {
		t.Fatal(err)
	}
	f.Close()
	finalPath := filepath.Join(st.BlobDir(), "pull-1")
	if err := verifyAndFinalize(partPath, finalPath, sha, 1000); err != nil {
		t.Fatalf("verify %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final missing %v", err)
	}
	// corrupted payload should fail
	part2 := filepath.Join(st.BlobDir(), "pull-2.part")
	os.WriteFile(part2, content[:500], 0600)
	f2, _ := os.OpenFile(part2, os.O_APPEND|os.O_WRONLY, 0600)
	bad := make([]byte, 500)
	copy(bad, content[500:])
	bad[0] ^= 0xff
	f2.Write(bad)
	f2.Close()
	final2 := filepath.Join(st.BlobDir(), "pull-2")
	if err := verifyAndFinalize(part2, final2, sha, 1000); err == nil {
		t.Fatalf("expected sha mismatch")
	}
	if _, err := os.Stat(final2); err == nil {
		t.Fatalf("corrupted should not create final")
	}
}
