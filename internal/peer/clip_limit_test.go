//nolint:all
package peer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type limitStub struct {
	status *ipnstate.Status
	w      *apitype.WhoIsResponse
}

func (s *limitStub) Status(_ context.Context) (*ipnstate.Status, error) { return s.status, nil }
func (s *limitStub) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return s.w, nil
}

func TestClipBodyLimit(t *testing.T) {
	tsnet.SetClient(&limitStub{
		status: &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{UserID: 1}},
		w:      &apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "x", User: 1}},
	})
	defer tsnet.ResetClient()
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer func() { _ = store.Close(st) }()
	srv := New(st, "test")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1 MiB inline should be 413
	inline := make([]byte, 1<<20)
	it := store.Item{ID: "big", Kind: "text", SHA256: "sha", Inline: inline}
	body, _ := json.Marshal(it)
	resp, err := http.Post(ts.URL+"/v0/clip", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != 413 {
		t.Fatalf("expected 413 for 1MiB, got %d", resp.StatusCode)
	}
	// 5 KiB inline should be 400 (over 4096)
	inline2 := make([]byte, 5000)
	it2 := store.Item{ID: "big2", Kind: "text", SHA256: "sha2", Inline: inline2}
	body2, _ := json.Marshal(it2)
	resp2, err := http.Post(ts.URL+"/v0/clip", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("post2: %v", err)
	}
	if resp2.StatusCode != 400 {
		t.Fatalf("expected 400 for 5KiB, got %d", resp2.StatusCode)
	}
}
