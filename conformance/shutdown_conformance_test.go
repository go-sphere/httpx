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

// Unlike conformanceFrameworks, includes stdx: the stop semantics belong to the
// Engine, which stdx has too.
var shutdownFrameworks = []string{"ginx", "fiberx", "echox", "hertzx", "stdx"}

// Reads the force-close capability from the adapter's shared suite, so the claim
// is checked against the declaration third-party adapters publish.
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

// Stop(ctx) is not "ask nicely": a drain the caller's context cuts short must
// still leave the listener closed and IsRunning false, with no timeout error.
// Needs a real listener, and a request held open across Stop so the drain has
// something to wait for.
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

			// Already-expired context: nothing can be drained, so only the forced path runs.
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

			// Whether a connection in flight is cut is a declared capability,
			// and both directions are checked: the handler is parked on a gate
			// nothing opens, so the held request can only finish by being cut.
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

// A real TCP listener whose Close fails, held until released so the moment
// fasthttp collects the error is fixed rather than raced for.
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

// A listener close failure must never be reported as a successful stop: the
// socket may still be bound. fiberx is where that could go wrong — fasthttp
// collects listener errors before consulting the context, so a ctx.Err() check
// alone turned a close failure into nil. Needs a real listener and WithListener.
func TestFiberxStopReportsListenerCloseFailure(t *testing.T) {
	// Already-spent deadline, so ctx.Err() is set when Stop inspects it.
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

	// Waits for the listener to reach fasthttp without dialing it: an accepted
	// connection counts as open, and fasthttp reports the listener error only
	// while none is. The settle covers the gap when Shutdown is still a no-op.
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

	// Let the close failure land only once the accept loop has exited; otherwise
	// the listener error and the context's are chosen between by a ticker race.
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

	// The flag still falls and the engine stays single-use whatever shutdown reported.
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

// Closing a listener is not instantaneous on every transport, so a refusal is
// given a window to appear rather than sampled once.
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
