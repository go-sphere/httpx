package conformance

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// The trusted-proxy policy depends on the connection's peer address, and the
// in-process requesters of fiberx/hertzx report 0.0.0.0, so this needs a real
// listener. WithTrustedProxies: X-Forwarded-For is honored only when the direct
// peer is trusted, on every adapter.
func TestTrustedProxiesConformance(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		xff     string
		wantIP  string
	}{
		{name: "SpoofIgnored", proxies: nil, xff: "203.0.113.9", wantIP: "127.0.0.1"},
		{name: "TrustedPeerHonored", proxies: []string{"127.0.0.1"}, xff: "203.0.113.9", wantIP: "203.0.113.9"},
		// Trusted hops are skipped right-to-left and the raw joined header must
		// never leak (fiber requires EnableIPValidation for this).
		{name: "MultiHopTrustedTailSkipped", proxies: []string{"127.0.0.1"}, xff: "203.0.113.9, 127.0.0.1", wantIP: "203.0.113.9"},
		// A client that is itself inside a trusted range, behind a same-host
		// proxy: every hop is trusted, so the leftmost entry is the client, as
		// gin, hertz, echo and fiber resolve it. Answering with the peer would
		// report the proxy for every LAN client.
		{name: "AllHopsTrustedReturnsLeftmost", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, xff: "192.168.1.50", wantIP: "192.168.1.50"},
		{name: "AllHopsTrustedMultiHopReturnsLeftmost", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, xff: "192.168.1.50, 192.168.1.60", wantIP: "192.168.1.50"},
		// An entry nothing can parse fails closed to the peer on every adapter,
		// and a port on an entry is equally unusable.
		{name: "MalformedChainFallsBackToPeer", proxies: []string{"127.0.0.1"}, xff: "garbage", wantIP: "127.0.0.1"},
		{name: "EntryWithPortFallsBackToPeer", proxies: []string{"127.0.0.1"}, xff: "1.2.3.4:5678", wantIP: "127.0.0.1"},
	}
	for _, a := range adapters() {
		for _, tc := range cases {
			t.Run(a.Name+"/"+tc.name, func(t *testing.T) {
				engine, addr := a.serve(t, serveConfig{trustProxies: true, proxies: tc.proxies})
				engine.Group("").GET("/ip", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, map[string]string{"ip": ctx.ClientIP()})
				})
				startServing(t, engine, addr)

				req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/ip", nil)
				if err != nil {
					t.Fatalf("build request: %v", err)
				}
				req.Header.Set("X-Forwarded-For", tc.xff)
				client := oneShotClient(2 * time.Second)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("request failed: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()
				body, _ := io.ReadAll(resp.Body)
				var payload map[string]string
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("parse body: %v; body=%q", err, body)
				}
				if payload["ip"] != tc.wantIP {
					t.Fatalf("ClientIP = %q, want %q", payload["ip"], tc.wantIP)
				}
			})
		}
	}
}

// TestClientIPFrameworkDifferences pins the edge cases where the adapters do
// not agree, so a framework upgrade that changes one of them fails here rather
// than in production. The shared contract is TestTrustedProxiesConformance;
// each divergence is also documented on the adapter's WithTrustedProxies.
func TestClientIPFrameworkDifferences(t *testing.T) {
	// A plausible deployment policy: the local reverse proxy plus the LAN.
	proxies := []string{"127.0.0.0/8", "10.0.0.0/8", "192.168.0.0/16"}
	cases := []struct {
		name      string
		configure bool
		xff       string
		xRealIP   string
		want      map[string]string
	}{
		{
			// The peer is trusted and the chain holds a blank entry. gin, echo
			// and hertz stop at it and answer with the peer; fiber and stdx skip
			// it and keep walking left.
			name:      "BlankEntry",
			configure: true,
			xff:       "1.2.3.4, , 10.0.0.2",
			want: map[string]string{
				"ginx": "127.0.0.1", "echox": "127.0.0.1", "hertzx": "127.0.0.1",
				"fiberx": "1.2.3.4", "stdx": "1.2.3.4",
			},
		},
		{
			// A bracketed entry is not a bare IP. Only echo normalizes it; the
			// rest fail closed to the peer.
			name:      "BracketedIPv6Entry",
			configure: true,
			xff:       "[2001:db8::1]",
			want: map[string]string{
				"echox": "2001:db8::1",
				"ginx":  "127.0.0.1", "fiberx": "127.0.0.1", "hertzx": "127.0.0.1", "stdx": "127.0.0.1",
			},
		},
		{
			// gin and hertz fall back to X-Real-IP when X-Forwarded-For yields
			// nothing; the other three never read the header.
			name:      "XRealIPFallback",
			configure: true,
			xRealIP:   "203.0.113.9",
			want: map[string]string{
				"ginx": "203.0.113.9", "hertzx": "203.0.113.9",
				"echox": "127.0.0.1", "fiberx": "127.0.0.1", "stdx": "127.0.0.1",
			},
		},
		{
			// Without WithTrustedProxies each adapter keeps its framework's own
			// default: gin and hertz honor forwarding headers from any peer,
			// whereas echo (v4.16.5+ defaults to peer IP when IPExtractor == nil),
			// fiber and stdx answer with the peer.
			name: "DefaultPolicyHonorsForwardedFor",
			xff:  "203.0.113.9",
			want: map[string]string{
				"ginx": "203.0.113.9", "hertzx": "203.0.113.9",
				"echox": "127.0.0.1", "fiberx": "127.0.0.1", "stdx": "127.0.0.1",
			},
		},
	}
	for _, a := range adapters() {
		for _, configure := range []bool{true, false} {
			engine, addr := a.serve(t, serveConfig{trustProxies: configure, proxies: proxies})
			engine.Group("").GET("/ip", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{"ip": ctx.ClientIP()})
			})
			startServing(t, engine, addr)
			client := oneShotClient(2 * time.Second)

			for _, tc := range cases {
				if tc.configure != configure {
					continue
				}
				t.Run(a.Name+"/"+tc.name, func(t *testing.T) {
					want, ok := tc.want[a.Name]
					if !ok {
						t.Fatalf("%s/%s has no expectation", a.Name, tc.name)
					}
					req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/ip", nil)
					if err != nil {
						t.Fatalf("build request: %v", err)
					}
					if tc.xff != "" {
						req.Header.Set("X-Forwarded-For", tc.xff)
					}
					if tc.xRealIP != "" {
						req.Header.Set("X-Real-IP", tc.xRealIP)
					}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatalf("request failed: %v", err)
					}
					defer func() { _ = resp.Body.Close() }()
					body, _ := io.ReadAll(resp.Body)
					var payload map[string]string
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatalf("parse body: %v; body=%q", err, body)
					}
					if payload["ip"] != want {
						t.Fatalf("ClientIP = %q, want %q", payload["ip"], want)
					}
				})
			}
		}
	}
}
