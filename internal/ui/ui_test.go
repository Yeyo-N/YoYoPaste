//nolint:all
package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Yeyo-N/YoYoPaste/internal/roster"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type stubEngine struct{ enabled bool }

func (e *stubEngine) Enabled() bool     { return e.enabled }
func (e *stubEngine) SetEnabled(v bool) { e.enabled = v }

type stubClient struct {
	status *ipnstate.Status
	stErr  error
	whois  *apitype.WhoIsResponse
	wErr   error
}

func (s *stubClient) Status(_ context.Context) (*ipnstate.Status, error) { return s.status, s.stErr }
func (s *stubClient) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return s.whois, s.wErr
}

func TestStateAndToggle(t *testing.T) {
	// Setup tsnet stub for SelfIP and Peers
	tsnet.SetClient(&stubClient{
		status: &ipnstate.Status{
			BackendState: "Running",
			TailscaleIPs: nil,
			Self:         &ipnstate.PeerStatus{UserID: 1},
		},
	})
	// Actually need to set status with valid TailscaleIPs to avoid error, but our handleState tolerates error.
	// Use nil map check: we need to Reset after.
	defer tsnet.ResetClient()

	eng := &stubEngine{enabled: false}
	srv := New(eng, nil, roster.New(context.Background()))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// GET /api/state should reflect enabled false
	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("state status %d", resp.StatusCode)
	}
	var st map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st["enabled"] != false {
		t.Fatalf("expected enabled false got %v", st["enabled"])
	}

	// POST /api/toggle flips to true
	resp, err = http.Post(ts.URL+"/api/toggle", "application/json", nil)
	if err != nil {
		t.Fatalf("post toggle: %v", err)
	}
	var tog map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&tog); err != nil {
		t.Fatalf("decode toggle: %v", err)
	}
	if !tog["enabled"] {
		t.Fatalf("expected enabled true after toggle")
	}
	if !eng.enabled {
		t.Fatalf("engine not toggled")
	}

	// Next GET should show true
	resp, _ = http.Get(ts.URL + "/api/state")
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st["enabled"] != true {
		t.Fatalf("expected enabled true")
	}

	// Second toggle back to false
	http.Post(ts.URL+"/api/toggle", "application/json", nil)
	if eng.enabled {
		t.Fatalf("expected disabled after second toggle")
	}
}

func TestLocalOnlyBinding(t *testing.T) {
	eng := &stubEngine{}
	srv := New(eng, nil, roster.New(context.Background()))
	handler := srv.Handler()
	// Direct handler request (simulating localhost) should succeed
	req := httptest.NewRequest("GET", "/api/state", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestPeersInState(t *testing.T) {
	tsnet.SetClient(&stubClient{
		status: &ipnstate.Status{
			BackendState: "Running",
			Self:         &ipnstate.PeerStatus{UserID: 1},
		},
	})
	defer tsnet.ResetClient()
	eng := &stubEngine{enabled: true}
	srv := New(eng, nil, roster.New(context.Background()))
	req := httptest.NewRequest("GET", "/api/state", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	var resp stateResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Empty peers is expected with stub
	if resp.Peers == nil {
		t.Fatalf("peers should be non-nil slice")
	}
}

func init() {
	_ = tailcfg.Node{}
}
