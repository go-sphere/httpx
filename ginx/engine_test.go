package ginx

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
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

// The unmatched-path fallback is installed unconditionally by New, so a NoRoute
// set before WithEngine is replaced; setting one after New wins. Both halves are
// asserted because both are easy to get wrong from the outside.
func TestRouteFallbackPrecedence(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	t.Run("BeforeNewIsReplaced", func(t *testing.T) {
		ge := gin.New()
		ge.NoRoute(func(gc *gin.Context) { gc.String(http.StatusNotFound, "caller's 404") })

		engine := New(WithEngine(ge))
		engine.Group("").GET("/known", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "ok") })

		rr := httptest.NewRecorder()
		ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
		if body := rr.Body.String(); body == "caller's 404" {
			t.Fatal("a NoRoute set before WithEngine survived; the adapter must replace it")
		} else if !strings.Contains(body, `"message":"Not Found"`) {
			t.Fatalf("body = %q, want the shared httpx error body", body)
		}

		// gin reports a wrong method as 404 until HandleMethodNotAllowed is set,
		// which this installs too.
		rr = httptest.NewRecorder()
		ge.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/known", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405; body=%q", rr.Code, rr.Body.String())
		}
	})

	t.Run("AfterNewWins", func(t *testing.T) {
		ge := gin.New()
		engine := New(WithEngine(ge))
		engine.Group("").GET("/known", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "ok") })
		// gin's setter replaces, so the last caller wins — the documented way to
		// keep your own unmatched-path answer.
		ge.NoRoute(func(gc *gin.Context) { gc.String(http.StatusNotFound, "caller's 404") })

		rr := httptest.NewRecorder()
		ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if got := rr.Body.String(); got != "caller's 404" {
			t.Fatalf("body = %q, want the caller's NoRoute handler to have answered", got)
		}
	})
}
