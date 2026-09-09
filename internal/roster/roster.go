package roster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
)

// Peer is a cached roster entry.
type Peer struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	OS            string     `json:"os"`
	IP            netip.Addr `json:"ip"`
	Version       string     `json:"version"`
	Participating bool       `json:"participating"`
	Online        bool       `json:"online"`
	LastSync      time.Time  `json:"last_sync"`
}

// Roster caches probed peers.
type Roster struct {
	mu    sync.RWMutex
	peers map[string]*Peer // keyed by ID
}

// New creates a roster and does an initial refresh.
func New(ctx context.Context) *Roster {
	r := &Roster{peers: make(map[string]*Peer)}
	_ = r.Refresh(ctx)
	return r
}

// Refresh probes all same-user peers via /v0/hello with 2s timeout, in parallel, and updates cache.
// It preserves LastSync for peers that remain and marks offline peers as not online but keeps them.
func (r *Roster) Refresh(ctx context.Context) error {
	peers, err := tsnet.Peers(ctx)
	if err != nil {
		return err
	}
	// Build a map of existing to preserve LastSync
	r.mu.RLock()
	existing := make(map[string]*Peer, len(r.peers))
	for k, v := range r.peers {
		copied := *v
		existing[k] = &copied
	}
	r.mu.RUnlock()

	type result struct {
		peer  tsnet.Peer
		hello map[string]string
		ok    bool
	}
	results := make(chan result, len(peers))
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(p tsnet.Peer) {
			defer wg.Done()
			hello, ok := probeHello(p)
			results <- result{peer: p, hello: hello, ok: ok}
		}(p)
	}
	wg.Wait()
	close(results)

	newPeers := make(map[string]*Peer)
	for res := range results {
		p := res.peer
		id := p.ID
		if id == "" {
			id = p.IP.String()
		}
		peer := &Peer{
			ID:            id,
			Name:          p.Name,
			OS:            p.OS,
			IP:            p.IP,
			Online:        p.Online,
			Participating: res.ok,
			LastSync:      time.Time{},
		}
		if res.ok {
			if h, ok := res.hello["version"]; ok {
				peer.Version = h
			}
			if h, ok := res.hello["name"]; ok && h != "" {
				peer.Name = h
			}
			if h, ok := res.hello["os"]; ok && h != "" {
				peer.OS = h
			}
			// Preserve LastSync if already existed and participating
			if ex, ok := existing[id]; ok && ex.LastSync.After(peer.LastSync) {
				peer.LastSync = ex.LastSync
			}
		} else {
			// Non-participant: keep LastSync if existed
			if ex, ok := existing[id]; ok {
				peer.LastSync = ex.LastSync
			}
		}
		newPeers[id] = peer
	}

	// Preserve peers that went offline but were previously known (so roster survives offline/return)
	// If a peer was in existing but not in new tsnet.Peers (e.g., went offline and not returned by Peers), keep it as offline
	for id, ex := range existing {
		if _, ok := newPeers[id]; !ok {
			ex.Online = false
			newPeers[id] = ex
		}
	}

	r.mu.Lock()
	r.peers = newPeers
	r.mu.Unlock()
	return nil
}

func probeHello(p tsnet.Peer) (map[string]string, bool) {
	if !p.IP.IsValid() {
		return nil, false
	}
	url := fmt.Sprintf("http://%s:8383/v0/hello", p.IP.String())
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var hello map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&hello); err != nil {
		return nil, false
	}
	return hello, true
}

// Peers returns a snapshot of all cached peers (including non-participants).
func (r *Roster) Peers() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, *p)
	}
	return out
}

// Participants returns only peers where /v0/hello succeeded and are online.
func (r *Roster) Participants() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Peer
	for _, p := range r.peers {
		if p.Participating && p.Online {
			out = append(out, *p)
		}
	}
	return out
}

// MarkSynced updates LastSync for a peer by ID or IP.
func (r *Roster) MarkSynced(id string, ip netip.Addr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		if p.ID == id || (ip.IsValid() && p.IP == ip) {
			p.LastSync = time.Now()
			return
		}
	}
}

// Start launches a 30s ticker to refresh the roster until ctx is cancelled.
func (r *Roster) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.Refresh(ctx)
		}
	}
}
