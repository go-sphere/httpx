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
	"github.com/gofiber/fiber/v3"
)

func newTrustedProxyEngine(tb testing.TB, name, addr string, proxies []string) httpx.Engine {
	tb.Helper()
	switch name {
	case "ginx":
		gin.SetMode(gin.ReleaseMode)
		return ginx.New(ginx.WithAddr(addr), ginx.WithTrustedProxies(proxies...))
	case "echox":
		return echox.New(echox.WithAddr(addr), echox.WithTrustedProxies(proxies...))
	case "fiberx":
		return fiberx.New(
			fiberx.WithListen(addr, fiber.ListenConfig{DisableStartupMessage: true}),
			fiberx.WithTrustedProxies(proxies...),
		)
	case "hertzx":
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
		return hertzx.New(hertzx.WithAddr(addr), hertzx.WithTrustedProxies(proxies...))
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
	}
	for _, framework := range conformanceFrameworks {
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
