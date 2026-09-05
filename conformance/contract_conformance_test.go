package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// newTrustedProxyEngine builds an engine for the given framework bound to
// addr with the given trusted-proxy list, using each adapter's
// WithTrustedProxies option.
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

// TestTrustedProxiesConformance covers B8: with WithTrustedProxies configured,
// X-Forwarded-For must be honored only when the direct peer is trusted, on
// every adapter. Requests go over a real loopback connection so the peer IP
// is 127.0.0.1 everywhere.
func TestTrustedProxiesConformance(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		xff     string
		wantIP  string
	}{
		// Empty list: forwarding headers are ignored; the spoofed XFF must not win.
		{name: "SpoofIgnored", proxies: nil, xff: "203.0.113.9", wantIP: "127.0.0.1"},
		// Loopback peer is trusted: the forwarded client IP is used.
		{name: "TrustedPeerHonored", proxies: []string{"127.0.0.1"}, xff: "203.0.113.9", wantIP: "203.0.113.9"},
		// Multi-hop chain: trusted hops are skipped right-to-left and the raw
		// joined header must never leak through (fiber requires
		// EnableIPValidation for this).
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

// TestBindValidationConformance covers D6: `binding:"required"` must fail
// with 400 on every adapter, for both JSON body and query binding.
func TestBindValidationConformance(t *testing.T) {
	type dto struct {
		Name string `json:"name" query:"name" binding:"required"`
	}
	register := func(r httpx.Router) {
		r.POST("/validate/json", func(ctx httpx.Context) error {
			var d dto
			if err := ctx.BindJSON(&d); err != nil {
				return err
			}
			return ctx.JSON(http.StatusOK, map[string]string{"name": d.Name})
		})
		r.GET("/validate/query", func(ctx httpx.Context) error {
			var d dto
			if err := ctx.BindQuery(&d); err != nil {
				return err
			}
			return ctx.JSON(http.StatusOK, map[string]string{"name": d.Name})
		})
	}
	cases := []struct {
		name       string
		request    func() *http.Request
		wantStatus int
		wantName   string
	}{
		{
			name: "JSONMissingRequired",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "http://example.com/validate/json", bytes.NewBufferString(`{}`))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "JSONPresent",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "http://example.com/validate/json", bytes.NewBufferString(`{"name":"sphere"}`))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			wantStatus: http.StatusOK,
			wantName:   "sphere",
		},
		{
			name: "QueryMissingRequired",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/validate/query", nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "QueryPresent",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/validate/query?name=sphere", nil)
			},
			wantStatus: http.StatusOK,
			wantName:   "sphere",
		},
	}
	for _, framework := range conformanceFrameworks {
		for _, tc := range cases {
			t.Run(framework+"/"+tc.name, func(t *testing.T) {
				h := newHarness(t, framework)
				register(h.Router)
				got := h.Do(t, tc.request())
				if got.Status != tc.wantStatus {
					t.Fatalf("%s status = %d, want %d; body=%q", framework, got.Status, tc.wantStatus, got.Body)
				}
				if tc.wantName != "" && !strings.Contains(got.Body, tc.wantName) {
					t.Fatalf("%s body = %q, want it to contain %q", framework, got.Body, tc.wantName)
				}
			})
		}
	}
}

type stdCtxKey struct{}

// upperWriter is a response-wrapping std middleware helper that upper-cases
// the body, exercising the writer-wrapping path of AdaptStdMiddleware.
type upperWriter struct {
	http.ResponseWriter
}

func (w upperWriter) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(bytes.ToUpper(p))
}

func stdMiddlewareFor(t *testing.T, framework string, mw func(http.Handler) http.Handler) httpx.Middleware {
	t.Helper()
	switch framework {
	case "ginx":
		return ginx.AdaptStdMiddleware(mw)
	case "echox":
		return echox.AdaptStdMiddleware(mw)
	case "fiberx":
		return fiberx.AdaptStdMiddleware(mw)
	case "hertzx":
		return hertzx.AdaptStdMiddleware(mw)
	default:
		t.Fatalf("unknown framework %q", framework)
		return nil
	}
}

// TestAdaptStdMiddlewareConformance covers B9: plain net/http middleware
// (request mutation, response wrapping, short-circuiting) must behave the
// same through every adapter.
func TestAdaptStdMiddlewareConformance(t *testing.T) {
	t.Run("RequestContextAndHeader", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Std", "1")
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), stdCtxKey{}, "from-std")))
			})
		}
		for _, framework := range conformanceFrameworks {
			t.Run(framework, func(t *testing.T) {
				h := newHarness(t, framework)
				h.Router.Use(stdMiddlewareFor(t, framework, mw))
				h.Router.GET("/std/ctx", func(ctx httpx.Context) error {
					v, _ := ctx.Context().Value(stdCtxKey{}).(string)
					return ctx.JSON(http.StatusOK, map[string]string{"value": v})
				})

				got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/std/ctx", nil))
				if got.Status != http.StatusOK {
					t.Fatalf("%s status = %d; body=%q", framework, got.Status, got.Body)
				}
				if got.Headers.Get("X-Std") != "1" {
					t.Fatalf("%s missing X-Std header", framework)
				}
				if !strings.Contains(got.Body, "from-std") {
					t.Fatalf("%s context value lost: body=%q", framework, got.Body)
				}
			})
		}
	})

	t.Run("ResponseWrapping", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Std", "wrapped")
				next.ServeHTTP(upperWriter{w}, r)
			})
		}
		for _, framework := range conformanceFrameworks {
			t.Run(framework, func(t *testing.T) {
				h := newHarness(t, framework)
				h.Router.Use(stdMiddlewareFor(t, framework, mw))
				h.Router.GET("/std/wrap", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "hello")
				})

				got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/std/wrap", nil))
				if got.Status != http.StatusOK {
					t.Fatalf("%s status = %d; body=%q", framework, got.Status, got.Body)
				}
				if got.Body != "HELLO" {
					t.Fatalf("%s wrapped body = %q, want %q", framework, got.Body, "HELLO")
				}
				if got.Headers.Get("X-Std") != "wrapped" {
					t.Fatalf("%s missing X-Std header", framework)
				}
			})
		}
	})

	t.Run("ShortCircuit", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("blocked"))
			})
		}
		for _, framework := range conformanceFrameworks {
			t.Run(framework, func(t *testing.T) {
				h := newHarness(t, framework)
				h.Router.Use(stdMiddlewareFor(t, framework, mw))
				h.Router.GET("/std/block", func(ctx httpx.Context) error {
					ctx.SetHeader("X-Handler-Ran", "1")
					return ctx.Text(http.StatusOK, "handler")
				})

				got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/std/block", nil))
				if got.Status != http.StatusUnauthorized {
					t.Fatalf("%s status = %d, want 401; body=%q", framework, got.Status, got.Body)
				}
				if got.Body != "blocked" {
					t.Fatalf("%s body = %q, want %q", framework, got.Body, "blocked")
				}
				if got.Headers.Get("X-Handler-Ran") != "" {
					t.Fatalf("%s handler ran after short-circuit", framework)
				}
			})
		}
	})
}

// TestMethodNameCaseConformance covers D8: lowercase method names must
// register and dispatch on every adapter.
func TestMethodNameCaseConformance(t *testing.T) {
	methods := []string{"get", "post", "put", "delete", "patch", "options"}
	for _, framework := range conformanceFrameworks {
		t.Run(framework, func(t *testing.T) {
			h := newHarness(t, framework)
			for _, method := range methods {
				h.Router.Handle(method, "/case/"+method, func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "ok")
				})
			}
			for _, method := range methods {
				req := httptest.NewRequest(strings.ToUpper(method), "http://example.com/case/"+method, nil)
				got := h.Do(t, req)
				if got.Status != http.StatusOK {
					t.Fatalf("%s %s status = %d, want 200", framework, method, got.Status)
				}
			}
		})
	}
}

// TestInvalidWildcardRegistrationPanics covers D12: unsupported wildcard
// shapes must fail registration identically on every adapter instead of
// silently registering a different route (fiber) or panicking with
// framework-specific behavior.
func TestInvalidWildcardRegistrationPanics(t *testing.T) {
	badPaths := []string{"/a/*x/b/*y", "/foo*bar", "/a/*x/tail"}
	for _, framework := range conformanceFrameworks {
		for _, path := range badPaths {
			t.Run(framework+"/"+path, func(t *testing.T) {
				h := newHarness(t, framework)
				defer func() {
					if recover() == nil {
						t.Fatalf("%s registering %q did not panic", framework, path)
					}
				}()
				h.Router.Handle("GET", path, func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "ok")
				})
			})
		}
	}
}

// TestBytesEmptyContentTypeConformance covers a conformance blind spot: an
// empty contentType must be sniffed identically on every adapter.
func TestBytesEmptyContentTypeConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/bytes/sniff", func(ctx httpx.Context) error {
			return ctx.Bytes(http.StatusOK, []byte("plain text payload"), "")
		})
	}, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "http://example.com/bytes/sniff", nil)
	})
	for _, framework := range conformanceFrameworks {
		ct := results[framework].Headers.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("%s content-type = %q, want text/plain (sniffed)", framework, ct)
		}
	}
	assertMatchesGin(t, results)
}

// TestTextContentTypeExactConformance covers a conformance blind spot: the
// full Content-Type (including charset case) must match across adapters.
func TestTextContentTypeExactConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/text/exact", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "hi")
		})
	}, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "http://example.com/text/exact", nil)
	})
	want := results["ginx"].Headers.Get("Content-Type")
	if want == "" {
		t.Fatal("gin baseline has no content type")
	}
	for _, framework := range conformanceFrameworks {
		got := results[framework].Headers.Get("Content-Type")
		if got != want {
			t.Fatalf("%s content-type = %q, want exactly %q", framework, got, want)
		}
	}
}

// TestStateStoreNilValueConformance covers a conformance blind spot: a stored
// nil must be reported as absent on every adapter.
func TestStateStoreNilValueConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/state/nil", func(ctx httpx.Context) error {
			ctx.Set("k", nil)
			_, ok := ctx.Get("k")
			ctx.Set("v", "value")
			v, vok := ctx.Get("v")
			return ctx.JSON(http.StatusOK, map[string]any{
				"nilOK":   ok,
				"value":   v,
				"valueOK": vok,
			})
		})
	}, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "http://example.com/state/nil", nil)
	})
	for _, framework := range conformanceFrameworks {
		var payload struct {
			NilOK   bool   `json:"nilOK"`
			Value   string `json:"value"`
			ValueOK bool   `json:"valueOK"`
		}
		if err := json.Unmarshal([]byte(results[framework].Body), &payload); err != nil {
			t.Fatalf("%s parse body: %v", framework, err)
		}
		if payload.NilOK {
			t.Fatalf("%s stored nil reported as present", framework)
		}
		if !payload.ValueOK || payload.Value != "value" {
			t.Fatalf("%s stored value lost: %+v", framework, payload)
		}
	}
}

// TestPathDecodingConformance covers a conformance blind spot: Path() must
// return the decoded request path on every adapter.
func TestPathDecodingConformance(t *testing.T) {
	for _, framework := range conformanceFrameworks {
		t.Run(framework, func(t *testing.T) {
			h := newHarness(t, framework)
			h.Router.GET("/enc/:id", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{"path": ctx.Path()})
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/enc/caf%C3%A9", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d; body=%q", framework, got.Status, got.Body)
			}
			var payload map[string]string
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body: %v", framework, err)
			}
			if want := fmt.Sprintf("/enc/%s", "café"); payload["path"] != want {
				t.Fatalf("%s Path() = %q, want %q (decoded)", framework, payload["path"], want)
			}
		})
	}
}
