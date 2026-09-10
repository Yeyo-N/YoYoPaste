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
	mu          sync.RWMutex
	peers       map[string]*Peer // keyed by ID
	absentSince map[string]time.Time
}

// New creates a roster without blocking startup (YYP-054).
func New(ctx context.Context) *Roster {
	r := &Roster{peers: make(map[string]*Peer), absentSince: make(map[string]time.Time)}
	return r
}

// Refresh probes the roster. It is safe to call concurrently (YYP-058).
func (r *Roster) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refreshLocked(ctx)
}

func (r *Roster) refreshLocked(ctx context.Context) error {
	peers, err := tsnet.Peers(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]*Peer, len(r.peers))
	for k, v := range r.peers {
		copied := *v
		existing[k] = &copied
	}

	type result struct {
		peer  tsnet.Peer
		hello map[string]string
		ok    bool
	}
	results := make(chan result, len(peers))
	var wg sync.WaitGroup
	for _, p := range peers {
		if !p.Online {
			results <- result{peer: p, ok: false}
			continue
		}
		wg.Add(1)
		go func(p tsnet.Peer) {
			defer wg.Done()
			hello, ok := probeHello(ctx, p)
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
				if len(h) > 64 {
					h = h[:64]
				}
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

	now := time.Now()
	for id, ex := range existing {
		if _, ok := newPeers[id]; !ok {
			if r.absentSince == nil {
				r.absentSince = make(map[string]time.Time)
			}
			if _, seen := r.absentSince[id]; !seen {
				r.absentSince[id] = now
			}
			ex.Online = false
			if since, ok := r.absentSince[id]; ok && now.Sub(since) > 5*time.Minute {
				delete(r.absentSince, id)
				continue
			}
			newPeers[id] = ex
		} else {
			if r.absentSince != nil {
				delete(r.absentSince, id)
			}
		}
	}

	r.peers = newPeers
	return nil
}

func probeHello(ctx context.Context, p tsnet.Peer) (map[string]string, bool) {
	if !p.IP.IsValid() {
		return nil, false
	}
	url := fmt.Sprintf("http://%s:8383/v0/hello", p.IP.String())
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
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
// It does an initial refresh immediately (but not blocking New).
func (r *Roster) Start(ctx context.Context) {
	_ = r.Refresh(ctx)
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
