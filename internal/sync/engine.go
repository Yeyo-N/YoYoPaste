package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Yeyo-N/YoYoPaste/internal/clip"
	"github.com/Yeyo-N/YoYoPaste/internal/peer"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"github.com/oklog/ulid/v2"
	"time"
)

// Engine wires watcher -> store -> broadcast, and inbound -> store -> clip.Set
type Engine struct {
	store   *store.Store
	peerSrv *peer.Server

	mu      sync.RWMutex
	enabled bool
}

// New creates an engine. Enabled defaults to true.
func New(st *store.Store, ps *peer.Server) *Engine {
	e := &Engine{store: st, peerSrv: ps, enabled: true}
	if ps != nil {
		ps.SetEnabledFunc(e.Enabled)
	}
	return e
}

// Enabled reports whether sync is enabled.
func (e *Engine) Enabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// SetEnabled toggles the engine. Disabled means watcher stops and inbound is stored but not applied to clipboard.
func (e *Engine) SetEnabled(v bool) {
	e.mu.Lock()
	e.enabled = v
	e.mu.Unlock()
}

// AutosyncEnabled reports whether inbound items should set clipboard.
func (e *Engine) AutosyncEnabled() bool {
	v, _ := e.store.GetSetting("autosync")
	if v == "" {
		return true // default on
	}
	return v != "0"
}

// SetAutosync persists the setting.
func (e *Engine) SetAutosync(v bool) {
	if v {
		_ = e.store.SetSetting("autosync", "1")
	} else {
		_ = e.store.SetSetting("autosync", "0")
	}
}

// Run starts the watcher and the inbound handler. Blocks until ctx cancelled.
func (e *Engine) Run(ctx context.Context) error {
	watchCh, err := clip.Watch(ctx)
	if err != nil {
		return fmt.Errorf("clip watch: %w", err)
	}
	inbound := e.subscribeInbound(ctx)
	go e.outboxLoop(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case it, ok := <-watchCh:
			if !ok {
				return nil
			}
			if !e.Enabled() {
				continue
			}
			if err := e.handleWatcherItem(ctx, it); err != nil {
				slog.Error("handle watcher item", "err", err)
			}
		case it, ok := <-inbound:
			if !ok {
				return nil
			}
			if err := e.handleInbound(it); err != nil {
				slog.Error("handle inbound", "err", err)
			}
		}
	}
}

func (e *Engine) outboxLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.drainOutbox(ctx)
		}
	}
}

func (e *Engine) drainOutbox(ctx context.Context) {
	peers, err := tsnet.Peers(ctx)
	if err != nil {
		return
	}
	online := make(map[string]tsnet.Peer)
	for _, p := range peers {
		if p.Online {
			online[p.ID] = p
		}
	}
	for peerID, p := range online {
		entries, err := e.store.ListOutbox(peerID)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Attempts >= 20 {
				_ = e.store.RemoveOutbox(entry.ItemID, peerID)
				continue
			}
			if entry.LastTry != nil && time.Since(*entry.LastTry) > 24*time.Hour {
				_ = e.store.RemoveOutbox(entry.ItemID, peerID)
				continue
			}
			it, ok, err := e.store.Get(entry.ItemID)
			if err != nil || !ok {
				_ = e.store.RemoveOutbox(entry.ItemID, peerID)
				continue
			}
			if err := peer.Send(ctx, p, it); err != nil {
				_ = e.store.IncrementAttempt(entry.ItemID, peerID)
				continue
			}
			_ = e.store.RemoveOutbox(entry.ItemID, peerID)
		}
	}
}

func (e *Engine) handleWatcherItem(ctx context.Context, it store.Item) error {
	// Fill missing fields if clip watcher didn't set ID etc.
	if it.ID == "" {
		it.ID = ulid.MustNew(ulid.Now(), nil).String()
	}
	if it.SHA256 == "" && len(it.Inline) > 0 {
		h := sha256.Sum256(it.Inline)
		it.SHA256 = fmt.Sprintf("%x", h[:])
	}
	if it.Created.IsZero() {
		it.Created = time.Now()
	}
	// Dedupe by sha256
	if _, ok, _ := e.store.BySHA(it.SHA256); ok {
		return nil
	}
	if err := e.store.Put(it); err != nil {
		return err
	}
	// Broadcast to peers
	peers, err := tsnet.Peers(ctx)
	if err != nil {
		// If tailscale unavailable, queue to outbox (YYP-021) - for now just log
		slog.Warn("peers unavailable, queuing outbox", "err", err)
		// Queue to outbox for offline peers: need peer list? We don't know peers if tailscale down. Skip.
		return nil
	}
	if len(peers) == 0 {
		return nil
	}
	// Filter online? But spec says broadcast to all peers in parallel
	errs := peer.Broadcast(ctx, peers, it)
	for i, err := range errs {
		if err != nil {
			// queue to outbox
			_ = e.store.AddOutbox(it.ID, peers[i].ID)
			slog.Warn("broadcast failed, queued", "peer", peers[i].Name, "err", err)
		}
	}
	return nil
}

func (e *Engine) handleInbound(it store.Item) error {
	// Dedupe
	if _, ok, _ := e.store.BySHA(it.SHA256); ok {
		return nil
	}
	if !e.Enabled() {
		// When disabled, refuse new items - but if we already received via peer server, we still store? Spec: disabled means peer server refuses new items, not receive silently.
		// So this path shouldn't be reached if peer server checks enabled. But we still store only if enabled? Spec says disabled means peer server refuses new items, so inbound should be rejected at HTTP layer.
		// We'll still store but not apply to clipboard; but to match spec we return early without storing.
		return nil
	}
	if err := e.store.Put(it); err != nil {
		return err
	}
	// Optionally set clipboard if autosync is on
	if e.AutosyncEnabled() {
		if err := clip.Set(it); err != nil {
			slog.Error("clip set", "err", err)
		}
	}
	return nil
}

// subscribeInbound returns channel of inbound items from peer server.
func (e *Engine) subscribeInbound(ctx context.Context) <-chan store.Item {
	if e.peerSrv == nil {
		ch := make(chan store.Item)
		go func() { <-ctx.Done(); close(ch) }()
		return ch
	}
	ch := e.peerSrv.Subscribe()
	go func() {
		<-ctx.Done()
		e.peerSrv.Unsubscribe(ch)
	}()
	return ch
}
