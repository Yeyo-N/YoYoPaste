package peer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
)

// Server serves protocol v0 on the Tailscale IP.
type Server struct {
	store   *store.Store
	version string
	mux     *http.ServeMux
	hub     *sseHub
	enabled func() bool
}

// New creates a Server.
func New(st *store.Store, version string) *Server {
	s := &Server{store: st, version: version, hub: newSSEHub(), enabled: func() bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/hello", s.handleHello)
	mux.HandleFunc("POST /v0/clip", s.handleClip)
	mux.HandleFunc("GET /v0/events", s.handleEvents)
	// blob endpoints (YYP-016) will be added later: GET /v0/blob/{id}
	mux.HandleFunc("GET /v0/blob/{id}", s.handleBlob)
	mux.HandleFunc("HEAD /v0/blob/{id}", s.handleBlobHead)
	s.mux = mux
	return s
}

// Handler returns the http.Handler with auth middleware.
func (s *Server) Handler() http.Handler {
	return authTailnet(s.mux)
}

// ListenAndServe binds to Tailscale IP :8383 only.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ip, err := tsnet.SelfIP(ctx)
	if err != nil {
		return fmt.Errorf("peer server: %w", err)
	}
	addr := net.JoinHostPort(ip.String(), "8383")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("peer listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("peer server listening", "addr", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	ip, err := tsnet.SelfIP(r.Context())
	ipStr := ""
	if err == nil {
		ipStr = ip.String()
	}
	hostname, _ := os.Hostname()
	resp := map[string]string{
		"id":      ipStr,
		"name":    hostname,
		"os":      runtime.GOOS,
		"version": s.version,
		"ip":      ipStr,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleClip(w http.ResponseWriter, r *http.Request) {
	if s.enabled != nil && !s.enabled() {
		http.Error(w, "sync disabled", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var it store.Item
	if err := json.NewDecoder(r.Body).Decode(&it); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if it.ID == "" || it.SHA256 == "" {
		http.Error(w, "missing id/sha256", http.StatusBadRequest)
		return
	}
	if len(it.Inline) > 4096 {
		http.Error(w, "inline too large", http.StatusBadRequest)
		return
	}
	if err := s.store.Put(it); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	// Broadcast to SSE subscribers
	s.hub.broadcast(it)
	w.WriteHeader(http.StatusNoContent)
}

// SSE hub

type sseHub struct {
	mu   sync.Mutex
	subs map[chan store.Item]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{subs: make(map[chan store.Item]struct{})}
}

func (h *sseHub) subscribe() chan store.Item {
	ch := make(chan store.Item, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan store.Item) {
	h.mu.Lock()
	delete(h.subs, ch)
	close(ch)
	h.mu.Unlock()
}

func (h *sseHub) broadcast(it store.Item) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- it:
		default:
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	// Send initial comment to confirm connection
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case it := <-ch:
			data, _ := json.Marshal(it)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			fl.Flush()
		case <-time.After(30 * time.Second):
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			fl.Flush()
		}
	}
}

var (
	authLogMu  sync.Mutex
	authLogged = make(map[string]struct{})
)

func authTailnet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := tsnet.WhoIs(r.Context(), r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		selfUID, err := tsnet.SelfUserID(r.Context())
		if err != nil {
			http.Error(w, "tailscale unavailable", http.StatusServiceUnavailable)
			return
		}
		if peer.UserID != selfUID {
			key := peer.ID
			if key == "" {
				key = r.RemoteAddr
			}
			authLogMu.Lock()
			if _, ok := authLogged[key]; !ok {
				slog.Warn("auth rejected: user mismatch", "peer", peer.Name, "peerID", peer.ID, "peerUser", peer.UserID, "selfUser", selfUID, "remote", r.RemoteAddr)
				authLogged[key] = struct{}{}
			}
			authLogMu.Unlock()
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Announce broadcasts item to hub (used by tests/direct store writes).
func (s *Server) Announce(it store.Item) {
	s.hub.broadcast(it)
}

// SetEnabledFunc sets callback for checking if sync is enabled.
func (s *Server) SetEnabledFunc(fn func() bool) { s.enabled = fn }

// Subscribe returns a channel that receives inbound clips.
func (s *Server) Subscribe() chan store.Item { return s.hub.subscribe() }

// Unsubscribe removes channel.
func (s *Server) Unsubscribe(ch chan store.Item) { s.hub.unsubscribe(ch) }

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if id != cleanID(id) {
		http.NotFound(w, r)
		return
	}
	it, ok, err := s.store.Get(id)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if it.BlobPath == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(it.BlobPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	// http.ServeContent handles Range, HEAD, Content-Type
	http.ServeContent(w, r, it.Name, it.Created, f)
}

func (s *Server) handleBlobHead(w http.ResponseWriter, r *http.Request) {
	s.handleBlob(w, r)
}

func cleanID(id string) string {
	// Very strict: alnum + - _ only
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return ""
		}
	}
	if id == "" {
		return ""
	}
	return id
}
