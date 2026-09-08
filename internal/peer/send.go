package peer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// Send POSTs /v0/clip to peer with 5s timeout. Text <=4KiB carries inline.
func Send(ctx context.Context, p tsnet.Peer, it store.Item) error {
	// Ensure context timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	data, err := json.Marshal(it)
	if err != nil {
		return fmt.Errorf("marshal clip: %w", err)
	}
	url := fmt.Sprintf("http://%s:8383/v0/clip", p.IP.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("peer %s returned %d", p.Name, resp.StatusCode)
	}
	return nil
}

// Broadcast sends to peers in parallel. One slow peer must not delay others.
func Broadcast(ctx context.Context, peers []tsnet.Peer, it store.Item) []error {
	var wg sync.WaitGroup
	errs := make([]error, len(peers))
	for i, p := range peers {
		wg.Add(1)
		go func(idx int, peer tsnet.Peer) {
			defer wg.Done()
			// Use background context with timeout per Send
			errs[idx] = Send(ctx, peer, it)
		}(i, p)
	}
	wg.Wait()
	return errs
}
