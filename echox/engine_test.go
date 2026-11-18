package echox

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

func TestStartReturnsNilAfterGracefulStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenerAddr := ln.Addr()
	if listenerAddr == nil {
		_ = ln.Close()
		t.Fatal("listener returned a nil address")
	}
	addr := listenerAddr.String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	engine := New(WithAddr(addr))
	errCh := make(chan error, 1)
	go func() { errCh <- engine.Start() }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !engine.IsRunning() {
		time.Sleep(10 * time.Millisecond)
	}
	if !engine.IsRunning() {
		t.Fatal("engine did not start")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := engine.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start after graceful Stop = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	if engine.IsRunning() {
		t.Fatal("IsRunning should be false after Stop")
	}
}

// A server the adapter creates bounds header reads and idle connections; one
// the caller supplies is left exactly as configured.
func TestServerTimeoutDefaults(t *testing.T) {
	for name, conf := range map[string]*Config{
		"default":  NewConfig(),
		"WithAddr": NewConfig(WithAddr(":9999")),
	} {
		if conf.server.ReadHeaderTimeout != DefaultReadHeaderTimeout || conf.server.IdleTimeout != DefaultIdleTimeout {
			t.Errorf("%s: ReadHeaderTimeout=%v IdleTimeout=%v, want %v and %v", name,
				conf.server.ReadHeaderTimeout, conf.server.IdleTimeout, DefaultReadHeaderTimeout, DefaultIdleTimeout)
		}
		if conf.server.ReadTimeout != 0 || conf.server.WriteTimeout != 0 {
			t.Errorf("%s: a whole-request timeout would cut uploads and streams", name)
		}
	}
	own := &http.Server{}
	conf := NewConfig(WithServer(own), WithAddr(":9999"))
	if conf.server != own || own.ReadHeaderTimeout != 0 || own.IdleTimeout != 0 {
		t.Errorf("WithServer's server was changed beyond Addr: %+v", own)
	}
}

func TestEchoContextEdgeFixes(t *testing.T) {
	// Bare FromEcho StatusCode defaults to 200 rather than 0.
	bare := FromEcho(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder()))
	if got := bare.StatusCode(); got != http.StatusOK {
		t.Fatalf("bare StatusCode = %d, want 200", got)
	}

	// BindForm ignores malformed RawQuery and binds non-POST urlencoded bodies.
	type formDTO struct {
		Name string `form:"name"`
	}
	req := httptest.NewRequest(http.MethodDelete, "/?bad=%ZZ", strings.NewReader("name=bob"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	fc := FromEcho(echo.New().NewContext(req, httptest.NewRecorder()))
	var dto formDTO
	if err := fc.BindForm(&dto); err != nil || dto.Name != "bob" {
		t.Fatalf("BindForm = (%v, %+v), want nil / bob", err, dto)
	}

	// ?pretty query does not make echox.JSON indent.
	engine := New()
	engine.Group("").GET("/json", func(ctx httpx.Context) error {
		return ctx.JSON(http.StatusOK, map[string]int{"a": 1})
	})
	resp, err := engine.(httpx.TestRequester).Do(httptest.NewRequest(http.MethodGet, "/json?pretty", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf [64]byte
	n, _ := resp.Body.Read(buf[:])
	if got := string(buf[:n]); got != "{\"a\":1}\n" {
		t.Fatalf("JSON with ?pretty = %q, want compact {\"a\":1}\\n", got)
	}
}

func TestServeHTTPAndTimeoutConstants(t *testing.T) {
	if DefaultReadHeaderTimeout != 10*time.Second || DefaultIdleTimeout != 120*time.Second {
		t.Fatalf("timeouts = (%v, %v), want (10s, 120s)", DefaultReadHeaderTimeout, DefaultIdleTimeout)
	}
	engine := New().(*Engine)
	var h http.Handler = engine
	engine.Group("").GET("/hello", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "world")
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/hello", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "world" {
		t.Fatalf("ServeHTTP = %d %q, want 200 %q", rr.Code, rr.Body.String(), "world")
	}
}

func TestOptionsFallbackAndAllowHeader(t *testing.T) {
	t.Run("UnregisteredOptionsRunsMiddlewareAndReturns405", func(t *testing.T) {
		var order []string
		engine := New()
		engine.Use(func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				order = append(order, "httpx")
				return next(ctx)
			}
		})
		engine.(*Engine).UseNative(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				order = append(order, "native")
				return next(c)
			}
		})
		engine.Group("").GET("/items", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "ok")
		})

		req := httptest.NewRequest(http.MethodOptions, "/items", nil)
		rr := httptest.NewRecorder()
		engine.(*Engine).ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("OPTIONS /items status = %d, want 405; body=%q", rr.Code, rr.Body.String())
		}
		if allow := rr.Header().Get(echo.HeaderAllow); allow != http.MethodGet {
			t.Fatalf("Allow = %q, want %q", allow, http.MethodGet)
		}
		if got := strings.Join(order, ","); got != "native,httpx" {
			t.Fatalf("middleware order = %q, want %q", got, "native,httpx")
		}

		// Wrong non-OPTIONS method must also omit synthetic OPTIONS from Allow.
		rrPost := httptest.NewRecorder()
		engine.(*Engine).ServeHTTP(rrPost, httptest.NewRequest(http.MethodPost, "/items", nil))
		if rrPost.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST /items status = %d, want 405", rrPost.Code)
		}
		if allow := rrPost.Header().Get(echo.HeaderAllow); allow != http.MethodGet {
			t.Fatalf("POST /items Allow = %q, want %q", allow, http.MethodGet)
		}
	})

	t.Run("UseNativeCORSInterceptSurvives", func(t *testing.T) {
		engine := New()
		engine.(*Engine).UseNative(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				if c.Request().Method == http.MethodOptions {
					c.Response().Header().Set(echo.HeaderAccessControlAllowOrigin, "*")
					return c.NoContent(http.StatusNoContent)
				}
				return next(c)
			}
		})
		engine.Group("").GET("/items", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "ok")
		})

		rr := httptest.NewRecorder()
		engine.(*Engine).ServeHTTP(rr, httptest.NewRequest(http.MethodOptions, "/items", nil))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS /items status = %d, want 204", rr.Code)
		}
		if got := rr.Header().Get(echo.HeaderAccessControlAllowOrigin); got != "*" {
			t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
		}
	})

	t.Run("ExplicitOptionsStaysInAllow", func(t *testing.T) {
		engine := New()
		r := engine.Group("")
		r.GET("/items", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
		r.OPTIONS("/items", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })

		rr := httptest.NewRecorder()
		engine.(*Engine).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/items", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST /items status = %d, want 405", rr.Code)
		}
		if allow := rr.Header().Get(echo.HeaderAllow); !strings.Contains(allow, http.MethodOptions) {
			t.Fatalf("POST /items Allow = %q, want OPTIONS included", allow)
		}
	})
}
