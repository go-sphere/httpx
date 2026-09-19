package conformance

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/go-sphere/httpx/stdx"
	"github.com/gofiber/fiber/v3"
)

func newTrustedProxyEngine(tb testing.TB, name, addr string, proxies []string) httpx.Engine {
	tb.Helper()
	return newProxyEngine(tb, name, addr, proxies, true)
}

// newProxyEngine builds one engine per adapter. configure selects whether
// WithTrustedProxies is installed at all, so a test can observe the
// framework's own default policy.
func newProxyEngine(tb testing.TB, name, addr string, proxies []string, configure bool) httpx.Engine {
	tb.Helper()
	switch name {
	case "ginx":
		gin.SetMode(gin.ReleaseMode)
		opts := []ginx.Option{ginx.WithAddr(addr)}
		if configure {
			opts = append(opts, ginx.WithTrustedProxies(proxies...))
		}
		return ginx.New(opts...)
	case "echox":
		opts := []echox.Option{echox.WithAddr(addr)}
		if configure {
			opts = append(opts, echox.WithTrustedProxies(proxies...))
		}
		return echox.New(opts...)
	case "fiberx":
		opts := []fiberx.Option{fiberx.WithListen(addr, fiber.ListenConfig{DisableStartupMessage: true})}
		if configure {
			opts = append(opts, fiberx.WithTrustedProxies(proxies...))
		}
		return fiberx.New(opts...)
	case "hertzx":
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
		opts := []hertzx.Option{hertzx.WithAddr(addr)}
		if configure {
			opts = append(opts, hertzx.WithTrustedProxies(proxies...))
		}
		return hertzx.New(opts...)
	case "stdx":
		opts := []stdx.Option{stdx.WithAddr(addr)}
		if configure {
			opts = append(opts, stdx.WithTrustedProxies(proxies...))
		}
		return stdx.New(opts...)
	default:
		tb.Fatalf("unknown framework %q", name)
		return nil
	}
}

func startEngineAndWait(t *testing.T, engine httpx.Engine, addr string) func() {
	t.Helper()
	go func() {
		_ = engine.Start()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = engine.Stop(ctx)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine on %s did not become reachable", addr)
	return nil
}

// proxyFrameworks is the framework list of TestTrustedProxiesConformance. It
// names stdx, which the shared conformanceFrameworks list does not carry.
var proxyFrameworks = []string{"ginx", "fiberx", "echox", "hertzx", "stdx"}

// The trusted-proxy policy depends on the connection's peer address, and the
// in-process requesters of fiberx/hertzx report 0.0.0.0, so this needs a real
// listener. WithTrustedProxies: X-Forwarded-For is honored only when the direct
// peer is trusted, on every adapter — stdx included, which is why this test
// names its frameworks itself.
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
		// proxy: every hop is trusted, so the leftmost entry is the client.
		// gin, hertz, echo and fiber all resolve it that way; stdx used to fall
		// back to the peer here, reporting 127.0.0.1 for every LAN session.
		{name: "AllHopsTrustedReturnsLeftmost", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, xff: "192.168.1.50", wantIP: "192.168.1.50"},
		{name: "AllHopsTrustedMultiHopReturnsLeftmost", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, xff: "192.168.1.50, 192.168.1.60", wantIP: "192.168.1.50"},
		// An entry nothing can parse fails closed to the peer on every adapter,
		// and a port on an entry is equally unusable.
		{name: "MalformedChainFallsBackToPeer", proxies: []string{"127.0.0.1"}, xff: "garbage", wantIP: "127.0.0.1"},
		{name: "EntryWithPortFallsBackToPeer", proxies: []string{"127.0.0.1"}, xff: "1.2.3.4:5678", wantIP: "127.0.0.1"},
	}
	for _, framework := range proxyFrameworks {
		for _, tc := range cases {
			t.Run(framework+"/"+tc.name, func(t *testing.T) {
				addr := reserveAddrTB(t)
				engine := newTrustedProxyEngine(t, framework, addr, tc.proxies)
				engine.Group("").GET("/ip", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, map[string]string{"ip": ctx.ClientIP()})
				})
				stop := startEngineAndWait(t, engine, addr)
				defer stop()

				req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/ip", nil)
				if err != nil {
					t.Fatalf("build request: %v", err)
				}
				req.Header.Set("X-Forwarded-For", tc.xff)
				client := &http.Client{Timeout: 2 * time.Second}
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
					t.Fatalf("%s ClientIP = %q, want %q", framework, payload["ip"], tc.wantIP)
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
			// default: gin, echo and hertz honor forwarding headers from any
			// peer, fiber and stdx answer with the peer.
			name: "DefaultPolicyHonorsForwardedFor",
			xff:  "203.0.113.9",
			want: map[string]string{
				"ginx": "203.0.113.9", "echox": "203.0.113.9", "hertzx": "203.0.113.9",
				"fiberx": "127.0.0.1", "stdx": "127.0.0.1",
			},
		},
	}
	for _, framework := range proxyFrameworks {
		for _, tc := range cases {
			t.Run(framework+"/"+tc.name, func(t *testing.T) {
				want, ok := tc.want[framework]
				if !ok {
					t.Fatalf("%s/%s has no expectation", framework, tc.name)
				}
				addr := reserveAddrTB(t)
				engine := newProxyEngine(t, framework, addr, proxies, tc.configure)
				engine.Group("").GET("/ip", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, map[string]string{"ip": ctx.ClientIP()})
				})
				stop := startEngineAndWait(t, engine, addr)
				defer stop()

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
				client := &http.Client{Timeout: 2 * time.Second}
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
					t.Fatalf("%s ClientIP = %q, want %q", framework, payload["ip"], want)
				}
			})
		}
	}
}
