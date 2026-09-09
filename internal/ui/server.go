package ui

import (
	"context"
	"embed"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/clip"
	"github.com/Yeyo-N/YoYoPaste/internal/roster"
	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
)

// Engine abstracts sync enabled toggle.
type Engine interface {
	Enabled() bool
	SetEnabled(bool)
}

// Server serves local UI API on 127.0.0.1:8384.
type Server struct {
	engine Engine
	store  *store.Store
	roster *roster.Roster
	mux    *http.ServeMux
}

// New creates a UI server. Roster is required (YYP-054).
func New(engine Engine, st *store.Store, r *roster.Roster) *Server {
	s := &Server{engine: engine, store: st, roster: r}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/toggle", s.handleToggle)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("POST /api/history/{id}/copy", s.handleHistoryCopy)
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /index.html", s.handleIndex)
	s.mux = mux
	return s
}

// Handler returns http.Handler.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe binds to 127.0.0.1:8384 only.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := "127.0.0.1:8384"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("ui server listening", "addr", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

type peerInfo struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	Status   string `json:"status"`
	LastSync string `json:"last_sync"`
}

type stateResp struct {
	Enabled bool       `json:"enabled"`
	Self    *selfInfo  `json:"self"`
	Peers   []peerInfo `json:"peers"`
}

type selfInfo struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
	OS   string `json:"os"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	enabled := false
	if s.engine != nil {
		enabled = s.engine.Enabled()
	}
	selfIPStr := ""
	if ip, err := tsnet.SelfIP(r.Context()); err == nil {
		selfIPStr = ip.String()
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "self"
	}
	selfOS := runtime.GOOS
	peers := s.roster.Peers()
	pi := make([]peerInfo, 0, len(peers)+1)
	pi = append(pi, peerInfo{Name: hostname, IP: selfIPStr, OS: selfOS, Status: "online", LastSync: "just now"})
	for _, p := range peers {
		if p.IP.String() == selfIPStr {
			continue
		}
		var status string
		if p.Online {
			if p.Participating {
				status = "online"
			} else {
				status = "not installed"
			}
		} else if p.Participating {
			status = "offline"
		} else {
			status = "not installed"
		}
		lastSync := "never"
		if !p.LastSync.IsZero() {
			lastSync = time.Since(p.LastSync).Round(time.Second).String() + " ago"
			if time.Since(p.LastSync) < time.Minute {
				lastSync = "just now"
			}
		} else if p.Online && p.Participating {
			lastSync = "never"
		}
		pi = append(pi, peerInfo{Name: p.Name, IP: p.IP.String(), OS: p.OS, Status: status, LastSync: lastSync})
	}
	resp := stateResp{Enabled: enabled, Self: &selfInfo{IP: selfIPStr, Name: hostname, OS: selfOS}, Peers: pi}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		http.Error(w, "no engine", http.StatusInternalServerError)
		return
	}
	newVal := !s.engine.Enabled()
	s.engine.SetEnabled(newVal)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": newVal})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	}
	nStr := r.URL.Query().Get("n")
	n := 50
	if nStr != "" {
		if v, err := strconv.Atoi(nStr); err == nil && v > 0 && v <= 500 {
			n = v
		}
	}
	items, err := s.store.Recent(n)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	type histItem struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Mime    string `json:"mime"`
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		SHA256  string `json:"sha256"`
		Created string `json:"created"`
		Preview string `json:"preview"`
	}
	out := make([]histItem, 0, len(items))
	for _, it := range items {
		preview := ""
		if it.Kind == "text" && len(it.Inline) > 0 {
			if len(it.Inline) > 100 {
				preview = string(it.Inline[:100])
			} else {
				preview = string(it.Inline)
			}
		} else if it.Kind == "file" {
			preview = it.Name
		}
		out = append(out, histItem{ID: it.ID, Kind: it.Kind, Mime: it.Mime, Name: it.Name, Size: it.Size, SHA256: it.SHA256, Created: it.Created.Format(time.RFC3339), Preview: preview})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleHistoryCopy(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "no store", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	it, ok, err := s.store.Get(id)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := clip.Set(it); err != nil {
		http.Error(w, "clipboard error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

//go:embed index.html
var indexFS embed.FS

var indexHTML = func() []byte {
	b, _ := indexFS.ReadFile("index.html")
	return b
}()

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}
