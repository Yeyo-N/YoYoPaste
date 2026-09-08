package tsnet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

// Peer describes a tailnet peer.
type Peer struct {
	ID       string
	Name     string
	OS       string
	IP       netip.Addr
	Online   bool
	LastSeen time.Time
	UserID   tailcfg.UserID
}

// ErrTailscaleUnavailable is returned when Tailscale is not running.
var ErrTailscaleUnavailable = errors.New("tailscale unavailable")

// Client abstracts the Tailscale LocalAPI for testing.
type Client interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

var lc Client = &local.Client{}

func statusClient() Client { return lc }

// SetClient replaces the underlying LocalAPI client (for tests).
func SetClient(c Client) { lc = c }

// ResetClient restores the default LocalAPI client.
func ResetClient() { lc = &local.Client{} }

// SelfIP returns our primary Tailscale IPv4 address.
func SelfIP(ctx context.Context) (netip.Addr, error) {
	st, err := statusClient().Status(ctx)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %v", ErrTailscaleUnavailable, err)
	}
	if st == nil {
		return netip.Addr{}, fmt.Errorf("%w: nil status", ErrTailscaleUnavailable)
	}
	if st.BackendState != "Running" {
		return netip.Addr{}, fmt.Errorf("%w: backend state %s", ErrTailscaleUnavailable, st.BackendState)
	}
	if len(st.TailscaleIPs) == 0 {
		return netip.Addr{}, fmt.Errorf("%w: no Tailscale IPs", ErrTailscaleUnavailable)
	}
	// Prefer IPv4 (100.x). Fall back to first.
	for _, ip := range st.TailscaleIPs {
		if ip.Is4() {
			return ip, nil
		}
	}
	return st.TailscaleIPs[0], nil
}

// Peers lists peers owned by the same user as self.
func Peers(ctx context.Context) ([]Peer, error) {
	st, err := statusClient().Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTailscaleUnavailable, err)
	}
	if st == nil {
		return nil, fmt.Errorf("%w: nil status", ErrTailscaleUnavailable)
	}
	if st.BackendState != "Running" {
		return nil, fmt.Errorf("%w: backend state %s", ErrTailscaleUnavailable, st.BackendState)
	}
	if st.Self == nil {
		return nil, fmt.Errorf("%w: no self status", ErrTailscaleUnavailable)
	}
	selfUser := st.Self.UserID
	var out []Peer
	for _, ps := range st.Peer {
		// Filter to same user (including shared nodes where AltSharer == selfUser? but spec says same user as self).
		// Keep nodes where UserID == selfUser. If UserID is 0, skip.
		if ps.UserID != selfUser {
			continue
		}
		// Pick first Tailscale IP
		var ip netip.Addr
		if len(ps.TailscaleIPs) > 0 {
			ip = ps.TailscaleIPs[0]
		}
		p := Peer{
			ID:       string(ps.ID),
			Name:     ps.HostName,
			OS:       ps.OS,
			IP:       ip,
			Online:   ps.Online,
			LastSeen: ps.LastSeen,
			UserID:   ps.UserID,
		}
		// Fallback DNSName if HostName empty
		if p.Name == "" {
			p.Name = strings.TrimSuffix(ps.DNSName, ".")
		}
		out = append(out, p)
	}
	return out, nil
}

// WhoIs authenticates an inbound request by remoteAddr (host:port).
func WhoIs(ctx context.Context, remoteAddr string) (Peer, error) {
	resp, err := statusClient().WhoIs(ctx, remoteAddr)
	if err != nil {
		// LocalAPI returns error when not tailnet; wrap as unavailable or auth failure.
		// Distinguish: if tailscale down, Status would fail; WhoIs failure is treated as 403 upstream,
		// but we return wrapped unavailable for detection.
		if strings.Contains(err.Error(), "not connected") || strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "connection refused") {
			return Peer{}, fmt.Errorf("%w: %v", ErrTailscaleUnavailable, err)
		}
		return Peer{}, fmt.Errorf("whois %s: %w", remoteAddr, err)
	}
	if resp.Node == nil {
		return Peer{}, fmt.Errorf("whois %s: no node", remoteAddr)
	}
	var ip netip.Addr
	if len(resp.Node.Addresses) > 0 {
		ip, _ = netip.ParseAddr(resp.Node.Addresses[0].Addr().String())
	}
	osName := ""
	if resp.Node.Hostinfo.Valid() {
		osName = resp.Node.Hostinfo.OS()
	}
	return Peer{
		ID:     string(resp.Node.StableID),
		Name:   resp.Node.ComputedName,
		OS:     osName,
		IP:     ip,
		UserID: resp.Node.User,
	}, nil
}

// SelfUserID returns the UserID of the current node.
func SelfUserID(ctx context.Context) (tailcfg.UserID, error) {
	st, err := statusClient().Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrTailscaleUnavailable, err)
	}
	if st == nil || st.Self == nil {
		return 0, fmt.Errorf("%w: no self status", ErrTailscaleUnavailable)
	}
	return st.Self.UserID, nil
}

// IsTailscaleUnavailable reports whether err wraps ErrTailscaleUnavailable.
func IsTailscaleUnavailable(err error) bool {
	return errors.Is(err, ErrTailscaleUnavailable)
}
