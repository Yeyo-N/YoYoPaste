package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/peer"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type stubClient struct {
	wErr error
	whois *apitype.WhoIsResponse
}

func (s *stubClient) Status(_ context.Context) (*ipnstate.Status, error) { return nil, nil }
func (s *stubClient) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	if s.wErr != nil {
		return nil, s.wErr
	}
	return s.whois, nil
}

func allow() *stubClient { return &stubClient{whois: &apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "test"}}} }

func TestTwoDeviceTextSync(t *testing.T) {
	tsnet.SetClient(allow())
	defer tsnet.ResetClient()

	dirA := t.TempDir()
	dirB := t.TempDir()
	stA, _ := store.Open(dirA)
	defer store.Close(stA)
	stB, _ := store.Open(dirB)
	defer store.Close(stB)

	srvA := peer.New(stA, "test")
	srvB := peer.New(stB, "test")

	tsA := httptest.NewServer(srvA.Handler())
	defer tsA.Close()
	tsB := httptest.NewServer(srvB.Handler())
	defer tsB.Close()

	// Simulate clipboard on A: create item and POST to B
	data := []byte("hello from A")
	h := sha256.Sum256(data)
	sha := fmt.Sprintf("%x", h[:])
	it := store.Item{ID: "test-01", Kind: "text", Mime: "text/plain", Size: int64(len(data)), SHA256: sha, Origin: "A", Created: time.Now(), Inline: data}
	if err := stA.Put(it); err != nil {
		t.Fatalf("put A: %v", err)
	}
	body, _ := json.Marshal(it)
	start := time.Now()
	resp, err := http.Post(tsB.URL+"/v0/clip", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post to B: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Logf("warning: crossing took %v (>100ms) but still passes logic", elapsed)
	}
	// Verify B stored
	got, ok, _ := stB.Get("test-01")
	if !ok || string(got.Inline) != "hello from A" {
		t.Fatalf("B store miss: %v ok %v", got, ok)
	}
	// Dedupe on repeat: second POST should not duplicate
	resp, _ = http.Post(tsB.URL+"/v0/clip", "application/json", bytes.NewReader(body))
	resp.Body.Close()
	n, _ := stB.Count()
	if n != 1 {
		t.Fatalf("dedupe failed count %d want 1", n)
	}
	// Ensure A count still 1
	n, _ = stA.Count()
	if n != 1 {
		t.Fatalf("A count %d want 1", n)
	}
}
