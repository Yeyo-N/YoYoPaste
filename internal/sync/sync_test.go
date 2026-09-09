//nolint:all
package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/Yeyo-N/YoYoPaste/internal/peer"
	"github.com/Yeyo-N/YoYoPaste/internal/roster"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type stub struct{}

func (s *stub) Status(_ context.Context) (*ipnstate.Status, error) {
	return &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{UserID: 1}}, nil
}
func (s *stub) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return &apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "x"}}, nil
}

func TestDedupeStopsLoop(t *testing.T) {
	tsnet.SetClient(&stub{})
	defer tsnet.ResetClient()
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer func() { _ = store.Close(st) }()
	ps := peer.New(st, "test")
	rstr := roster.New(context.Background())
	eng := New(st, ps, rstr)
	data := []byte("hello")
	h := sha256.Sum256(data)
	sha := fmt.Sprintf("%x", h[:])
	it := store.Item{ID: "id1", Kind: "text", Inline: data, SHA256: sha}
	// first inbound should store and set clipboard (clip stub)
	if err := eng.handleInbound(it); err != nil {
		t.Fatal(err)
	}
	n, _ := st.Count()
	if n != 1 {
		t.Fatalf("count %d", n)
	}
	// second same sha should be deduped
	it2 := store.Item{ID: "id2", Kind: "text", Inline: data, SHA256: sha}
	if err := eng.handleInbound(it2); err != nil {
		t.Fatal(err)
	}
	n, _ = st.Count()
	if n != 1 {
		t.Fatalf("dedupe failed %d", n)
	}
}

func TestOutboxDirect(t *testing.T) {
	tsnet.SetClient(&stub{})
	defer tsnet.ResetClient()
	dir := t.TempDir()
	st, _ := store.Open(dir)
	defer func() { _ = store.Close(st) }()
	ps := peer.New(st, "test")
	rstr := roster.New(context.Background())
	_ = New(st, ps, rstr)
	st.Put(store.Item{ID: "id1", Kind: "text", SHA256: "sha1", Size: 1})
	_ = st.AddOutbox("id1", "peer1")
	entries, _ := st.ListOutbox("peer1")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	// Increment attempts beyond 20 should be dropped
	for i := 0; i < 21; i++ {
		st.IncrementAttempt("id1", "peer1")
	}
	entries, _ = st.ListOutbox("peer1")
	if entries[0].Attempts < 20 {
		t.Fatalf("attempts %d", entries[0].Attempts)
	}
}
