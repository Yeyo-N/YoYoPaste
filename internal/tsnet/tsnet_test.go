package tsnet

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
	"go4.org/mem"
)

type stubClient struct {
	status *ipnstate.Status
	stErr  error
	whois  *apitype.WhoIsResponse
	wErr   error
}

func (s *stubClient) Status(_ context.Context) (*ipnstate.Status, error) {
	return s.status, s.stErr
}
func (s *stubClient) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return s.whois, s.wErr
}

func TestPeersFiltering(t *testing.T) {
	// self user 1, peers: two with user 1, one with user 2
	selfKey := key.NodePublic{}
	ip1 := netip.MustParseAddr("100.64.0.1")
	ip2 := netip.MustParseAddr("100.64.0.2")
	ip3 := netip.MustParseAddr("100.64.0.3")
	pk1, pk2, pk3 := key.NodePublic{}, key.NodePublic{}, key.NodePublic{}
	// generate distinct keys by setting bytes
	pk1 = key.NodePublicFromRaw32(mem.B([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	pk2 = key.NodePublicFromRaw32(mem.B([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	pk3 = key.NodePublicFromRaw32(mem.B([]byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}))
	_ = selfKey

	st := &ipnstate.Status{
		BackendState: "Running",
		Self:         &ipnstate.PeerStatus{UserID: 1},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			pk1: {ID: "a", HostName: "peer-a", OS: "linux", UserID: 1, TailscaleIPs: []netip.Addr{ip1}, Online: true},
			pk2: {ID: "b", HostName: "peer-b", OS: "macOS", UserID: 1, TailscaleIPs: []netip.Addr{ip2}, Online: false, LastSeen: time.Now()},
			pk3: {ID: "c", HostName: "peer-c", OS: "windows", UserID: 2, TailscaleIPs: []netip.Addr{ip3}, Online: true},
		},
	}
	SetClient(&stubClient{status: st})
	defer ResetClient()

	peers, err := Peers(context.Background())
	if err != nil {
		t.Fatalf("Peers error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	for _, p := range peers {
		if p.ID == "c" {
			t.Fatalf("peer c should be filtered (different user)")
		}
	}
}

func TestWhoIsRejectsNonTailnet(t *testing.T) {
	SetClient(&stubClient{wErr: errors.New("not connected: no peer")})
	defer ResetClient()
	_, err := WhoIs(context.Background(), "1.2.3.4:1234")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelfIPUnavailable(t *testing.T) {
	SetClient(&stubClient{status: &ipnstate.Status{BackendState: "Stopped"}})
	defer ResetClient()
	_, err := SelfIP(context.Background())
	if !IsTailscaleUnavailable(err) {
		t.Fatalf("expected tailscale unavailable, got %v", err)
	}
}

func TestSelfIPOK(t *testing.T) {
	ip := netip.MustParseAddr("100.64.0.10")
	st := &ipnstate.Status{BackendState: "Running", TailscaleIPs: []netip.Addr{ip}}
	SetClient(&stubClient{status: st})
	defer ResetClient()
	got, err := SelfIP(context.Background())
	if err != nil {
		t.Fatalf("SelfIP error: %v", err)
	}
	if got != ip {
		t.Fatalf("got %v want %v", got, ip)
	}
}
