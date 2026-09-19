package conformance

import (
	"context"
	"errors"
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

// failCloseListener is a real TCP listener whose Close fails. The failure is
// held until the test releases it, so the moment fasthttp collects it is fixed
// rather than raced for: see TestFiberxStopReportsListenerCloseFailure.
type failCloseListener struct {
	net.Listener
	closing chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

func (l *failCloseListener) Close() error {
	l.once.Do(func() {
		_ = l.Listener.Close()
		close(l.closing)
		<-l.release
	})
	return l.err
}

// A listener that fails to close means the socket may still be bound, and that
// must never be reported as a successful stop — the whole point of the forced-stop
// work is not telling a caller the server is down when it is not.
//
// fiberx is the adapter that could get this wrong, because fasthttp closes its
// listeners and collects their errors *before* it consults the context: testing
// ctx.Err() alone turned a genuine close failure into nil for every caller who
// passed a context.WithTimeout, which is every caller doing a graceful drain.
//
// This is fiberx-only and lives here rather than in the shared suite because it
// needs a real listener and a fiber-specific injection point (WithListener);
// no other adapter routes a caller's net.Listener into its shutdown path.
func TestFiberxStopReportsListenerCloseFailure(t *testing.T) {
	// A deadline that is already spent, so ctx.Err() is set when Stop inspects
	// it. That is the state in which the old check returned nil.
	stopCtx, cancelStop := context.WithCancel(context.Background())
	cancelStop()

	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := &failCloseListener{
		Listener: base,
		closing:  make(chan struct{}),
		release:  make(chan struct{}),
		err:      errors.New("fiberx test: listener close failed"),
	}

	// Wait for the listener to be handed to fasthttp without dialing it: an
	// accepted connection counts as open, and fasthttp only reports the listener
	// error while nothing is. The settle covers the gap between this hook and
	// fasthttp registering the listener, before which Shutdown is a no-op.
	serving := make(chan struct{})
	engine := fiberx.New(fiberx.WithListener(ln, fiber.ListenConfig{
		DisableStartupMessage: true,
		BeforeServeFunc:       func(*fiber.App) error { close(serving); return nil },
	}))
	engine.Group("").GET("/ping", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "pong")
	})

	served := make(chan error, 1)
	go func() { served <- engine.Start() }()
	<-serving
	time.Sleep(100 * time.Millisecond)

	stopped := make(chan error, 1)
	go func() { stopped <- engine.Stop(stopCtx) }()

	// Let the close failure land only once fasthttp's accept loop has exited, so
	// it sees no connection open and returns the listener error instead of the
	// context's. Without this the two are chosen between by a 100ms ticker race.
	select {
	case <-ln.closing:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop never closed the listener")
	}
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after the listener closed")
	}
	close(ln.release)

	select {
	case stopErr := <-stopped:
		if !errors.Is(stopErr, ln.err) {
			t.Fatalf("Stop = %v, want the listener close failure %v", stopErr, ln.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}

	// Still the rest of the contract: the flag falls and the engine is single-use
	// whatever shutdown reported.
	if engine.IsRunning() {
		t.Fatal("IsRunning() is true after Stop returned")
	}
	if err := engine.Start(); !errors.Is(err, httpx.ErrEngineClosed) {
		t.Fatalf("Start after a failed Stop = %v, want httpx.ErrEngineClosed", err)
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
