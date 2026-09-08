package clip

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
)

// Watch emits clipboard changes. Implemented per-OS.
func Watch(ctx context.Context) (<-chan store.Item, error) {
	return watch(ctx)
}

// Set writes item to system clipboard.
func Set(it store.Item) error {
	return set(it)
}

// hash returns sha256 hex of inline data.
func hashInline(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

var _ = time.Now
var _ = hashInline
