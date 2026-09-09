package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/clip"
	"github.com/Yeyo-N/YoYoPaste/internal/peer"
	"github.com/Yeyo-N/YoYoPaste/internal/roster"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"github.com/oklog/ulid/v2"
)

// Engine wires watcher -> store -> broadcast, and inbound -> store -> clip.Set
type Engine struct {
	store   *store.Store
	peerSrv *peer.Server
	roster  *roster.Roster

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

// NewWithRoster creates an engine with a roster for participant filtering.
func NewWithRoster(st *store.Store, ps *peer.Server, r *roster.Roster) *Engine {
	e := New(st, ps)
	e.roster = r
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
	if e.roster != nil {
		go e.roster.Start(ctx)
	}
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
	var online map[string]tsnet.Peer
	if e.roster != nil {
		online = make(map[string]tsnet.Peer)
		for _, rp := range e.roster.Participants() {
			online[rp.ID] = tsnet.Peer{ID: rp.ID, Name: rp.Name, OS: rp.OS, IP: rp.IP, Online: rp.Online}
		}
	} else {
		peers, err := tsnet.Peers(ctx)
		if err != nil {
			return
		}
		online = make(map[string]tsnet.Peer)
		for _, p := range peers {
			if p.Online {
				online[p.ID] = p
			}
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
			if e.roster != nil {
				e.roster.MarkSynced(p.ID, p.IP)
			}
		}
	}
}

func (e *Engine) handleWatcherItem(ctx context.Context, it store.Item) error {
	if it.ID == "" {
		it.ID = ulid.Make().String()
	}
	if it.SHA256 == "" && len(it.Inline) > 0 {
		h := sha256.Sum256(it.Inline)
		it.SHA256 = fmt.Sprintf("%x", h[:])
	}
	if it.Created.IsZero() {
		it.Created = time.Now()
	}
	if e.isRecentDuplicate(it.SHA256) {
		return nil
	}
	if err := e.store.Put(it); err != nil {
		return err
	}
	// Use roster if available to only target participants (YYP-044)
	var targets []tsnet.Peer
	if e.roster != nil {
		for _, rp := range e.roster.Participants() {
			targets = append(targets, tsnet.Peer{ID: rp.ID, Name: rp.Name, OS: rp.OS, IP: rp.IP, Online: rp.Online})
		}
		// Also queue for offline participants
		for _, rp := range e.roster.Peers() {
			if rp.Participating && !rp.Online {
				_ = e.store.AddOutbox(it.ID, rp.ID)
			}
		}
		if len(targets) == 0 {
			return nil
		}
	} else {
		peers, err := tsnet.Peers(ctx)
		if err != nil {
			slog.Warn("peers unavailable, queuing outbox", "err", err)
			return nil
		}
		if len(peers) == 0 {
			return nil
		}
		for _, p := range peers {
			if p.Online {
				targets = append(targets, p)
			} else {
				_ = e.store.AddOutbox(it.ID, p.ID)
			}
		}
		if len(targets) == 0 {
			return nil
		}
	}
	errs := peer.Broadcast(ctx, targets, it)
	for i, err := range errs {
		if err != nil {
			_ = e.store.AddOutbox(it.ID, targets[i].ID)
			slog.Warn("broadcast failed, queued", "peer", targets[i].Name, "err", err)
		} else if e.roster != nil {
			e.roster.MarkSynced(targets[i].ID, targets[i].IP)
		}
	}
	return nil
}

func (e *Engine) handleInbound(it store.Item) error {
	if e.isRecentDuplicate(it.SHA256) {
		return nil
	}
	if !e.Enabled() {
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

// isRecentDuplicate reports whether sha matches the most recent item.
// This breaks the echo loop without deduping history.
func (e *Engine) isRecentDuplicate(sha string) bool {
	recent, err := e.store.Recent(1)
	if err != nil || len(recent) == 0 {
		return false
	}
	return recent[0].SHA256 == sha
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
