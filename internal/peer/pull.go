package peer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
)

// Pull downloads blob for item from origin peer with resume.
func Pull(ctx context.Context, origin tsnet.Peer, it store.Item, st *store.Store) error {
	dir := st.BlobDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	finalPath := filepath.Join(dir, it.ID)
	partPath := finalPath + ".part"

	// If final already exists and matches size, verify sha? Assume done.
	if fi, err := os.Stat(finalPath); err == nil && fi.Size() == it.Size {
		// verify sha? Could check but skip for now
		return nil
	}

	deadline := time.Now().Add(10 * time.Minute)
	var lastErr error
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		var offset int64
		if fi, err := os.Stat(partPath); err == nil {
			offset = fi.Size()
		}
		if offset >= it.Size {
			offset = 0
			_ = os.Remove(partPath)
		}
		err := downloadChunk(ctx, origin, it.ID, partPath, offset)
		if err != nil {
			lastErr = err
			backoff := time.Duration(200*(1<<attempt)) * time.Millisecond
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		// Verify sha256 - retry on failure instead of returning
		if err := verifyAndFinalize(partPath, finalPath, it.SHA256, it.Size); err != nil {
			lastErr = err
			_ = os.Remove(partPath)
			backoff := time.Duration(200*(1<<attempt)) * time.Millisecond
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		// Update store blob_path
		_ = st.UpdateBlobPath(it.ID, finalPath)
		return nil
	}
	return fmt.Errorf("pull failed after retries: %w", lastErr)
}

func downloadChunk(ctx context.Context, origin tsnet.Peer, id, partPath string, offset int64) error {
	url := fmt.Sprintf("http://%s:8383/v0/blob/%s", origin.IP.String(), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("blob status %d", resp.StatusCode)
	}
	// If server ignored Range and returned 200 while we have a partial file, truncate before appending.
	if offset > 0 && resp.StatusCode == http.StatusOK {
		_ = os.Remove(partPath)
	}
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// Stream copy without holding in memory
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

func verifyAndFinalize(partPath, finalPath, expectedSHA string, expectedSize int64) error {
	f, err := os.Open(partPath)
	if err != nil {
		return err
	}
	fi, _ := f.Stat()
	if fi.Size() != expectedSize {
		_ = f.Close()
		return fmt.Errorf("size mismatch %d != %d", fi.Size(), expectedSize)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expectedSHA {
		_ = f.Close()
		_ = os.Remove(partPath)
		return fmt.Errorf("sha256 mismatch got %s want %s", got, expectedSHA)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(partPath, finalPath)
}
