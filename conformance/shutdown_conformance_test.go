package conformance

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/go-sphere/httpx/stdx"
	"github.com/gofiber/fiber/v3"
)

// shutdownFrameworks includes stdx, unlike conformanceFrameworks: the stop
// semantic is the engine's, not the framework's, and stdx has an Engine like
// the rest.
var shutdownFrameworks = []string{"ginx", "fiberx", "echox", "hertzx", "stdx"}

// capsFor reads the capability an adapter declares in its shared suite, so the
// force-close claim is verified against the same declaration third-party
// adapters publish rather than against a second list kept here.
func capsFor(tb testing.TB, name string) httpxtest.Caps {
	tb.Helper()
	for _, suite := range httpxtestSuites() {
		if suite.Name == name {
			return suite.Caps
		}
	}
	tb.Fatalf("no shared suite declares capabilities for %q", name)
	return httpxtest.Caps{}
}

func newShutdownEngine(tb testing.TB, name, addr string) httpx.Engine {
	tb.Helper()
	switch name {
	case "ginx":
		gin.SetMode(gin.ReleaseMode)
		return ginx.New(ginx.WithAddr(addr))
	case "echox":
		return echox.New(echox.WithAddr(addr))
	case "fiberx":
		return fiberx.New(fiberx.WithListen(addr, fiber.ListenConfig{DisableStartupMessage: true}))
	case "hertzx":
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
		return hertzx.New(hertzx.WithAddr(addr))
	case "stdx":
		return stdx.New(stdx.WithAddr(addr))
	default:
		tb.Fatalf("unknown framework %q", name)
		return nil
	}
}

// TestEngineForcedStopConformance pins the other half of the lifecycle
// contract: Stop(ctx) is not just "ask nicely". A graceful drain the caller's
// context cuts short must still leave the server down — the listener closed and
// IsRunning false — instead of reporting a timeout and leaving connections
// serving with no way for the caller to force them shut.
//
// It needs a real listener: the point is whether the socket is still accepting,
// which no in-process requester can answer. A request is held open across the
// stop so the graceful drain has something to wait for and cannot finish inside
// the deadline.
func TestEngineForcedStopConformance(t *testing.T) {
	for _, name := range shutdownFrameworks {
		t.Run(name, func(t *testing.T) {
			addr := reserveAddrTB(t)
			engine := newShutdownEngine(t, name, addr)

			release := make(chan struct{})
			var once sync.Once
			openGate := func() { once.Do(func() { close(release) }) }
			defer openGate()

			entered := make(chan struct{}, 1)
			engine.Group("").GET("/hold", func(ctx httpx.Context) error {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
				return ctx.Text(http.StatusOK, "done")
			})

			go func() { _ = engine.Start() }()
			waitReachable(t, addr)

			// Hold a request inside the handler so the drain has work in flight.
			holdCtx, cancelHold := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelHold()
			held := make(chan struct{})
			go func() {
				defer close(held)
				req, err := http.NewRequestWithContext(holdCtx, http.MethodGet, "http://"+addr+"/hold", nil)
				if err != nil {
					return
				}
				resp, err := (&http.Client{}).Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the held request never reached the handler")
			}

			// A context that has already expired: nothing can be drained inside
			// it, so this exercises the forced path and nothing else.
			stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Nanosecond)
			defer cancelStop()
			<-stopCtx.Done()

			stopped := make(chan error, 1)
			go func() { stopped <- engine.Stop(stopCtx) }()
			var stopErr error
			select {
			case stopErr = <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("Stop did not return")
			}
			if stopErr != nil {
				t.Fatalf("Stop with an expired context = %v, want nil: a forced stop is still a successful stop", stopErr)
			}

			if engine.IsRunning() {
				t.Fatal("IsRunning() is true after Stop returned")
			}
			if stillAccepting(addr) {
				t.Fatalf("%s is still accepting connections on %s after Stop", name, addr)
			}

			// Closing the listener is the contract, and it has now been
			// checked. What the adapters genuinely differ on is the connection
			// still being served, which is why that difference is declared as a
			// capability instead of left silent — see httpxtest.Caps.
			// ForcedStopCutsConnections. Both directions are checked, so an
			// adapter cannot claim the easier answer: the handler is parked on
			// a gate nothing has opened, so the held request can only finish if
			// its connection was cut.
			if capsFor(t, name).ForcedStopCutsConnections {
				select {
				case <-held:
				case <-time.After(5 * time.Second):
					t.Fatalf("%s declares ForcedStopCutsConnections, but the request in flight outlived Stop", name)
				}
			} else {
				select {
				case <-held:
					t.Fatalf("%s declares it cannot cut a connection in flight, yet the held request ended at Stop", name)
				case <-time.After(300 * time.Millisecond):
				}
			}

			openGate()
			<-held
		})
	}
}

func waitReachable(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if dialable(addr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine on %s did not become reachable", addr)
}

// stillAccepting reports whether the address is still taking connections.
// Closing a listener is not instantaneous on every transport, so a refusal is
// given a short window to appear rather than being sampled once.
func stillAccepting(addr string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !dialable(addr) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func dialable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
