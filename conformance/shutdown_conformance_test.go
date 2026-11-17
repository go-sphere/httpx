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

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/gofiber/fiber/v3"
)

// Stop(ctx) is not "ask nicely": a drain the caller's context cuts short must
// still leave the listener closed and IsRunning false, with no timeout error.
// Needs a real listener, and a request held open across Stop so the drain has
// something to wait for.
func TestEngineForcedStopConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{})

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
				t.Fatalf("%s is still accepting connections on %s after Stop", a.Name, addr)
			}

			// Whether a connection in flight is cut is a declared capability,
			// and both directions are checked: the handler is parked on a gate
			// nothing opens, so the held request can only finish by being cut.
			if a.Caps.ForcedStopCutsConnections {
				select {
				case <-held:
				case <-time.After(5 * time.Second):
					t.Fatalf("%s declares ForcedStopCutsConnections, but the request in flight outlived Stop", a.Name)
				}
			} else {
				select {
				case <-held:
					t.Fatalf("%s declares it cannot cut a connection in flight, yet the held request ended at Stop", a.Name)
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
	case startErr := <-served:
		if startErr != nil {
			t.Fatalf("Start = %v, want nil once Stop ended it", startErr)
		}
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

// The lifecycle answers that must not depend on the framework underneath:
// Stop on an engine that never started returns nil, Start returns nil — not the
// framework's "server closed" — once a graceful Stop ended it, IsRunning is
// false after Stop, and an engine is single-use, so Start after Stop in either
// order returns httpx.ErrEngineClosed without blocking. A caller can treat any
// other error from Start or Stop as a real failure.
func TestEngineLifecycleErrorsConformance(t *testing.T) {
	t.Run("StopBeforeStartReturnsNil", func(t *testing.T) {
		for _, a := range adapters() {
			t.Run(a.Name, func(t *testing.T) {
				engine, _ := a.serve(t, serveConfig{})
				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := engine.Stop(stopCtx); err != nil {
					t.Fatalf("Stop before Start = %v, want nil", err)
				}
				if engine.IsRunning() {
					t.Fatal("IsRunning() is true after Stop")
				}
				if err := startWithTimeout(t, engine); !errors.Is(err, httpx.ErrEngineClosed) {
					t.Fatalf("Start after Stop = %v, want ErrEngineClosed", err)
				}
			})
		}
	})

	t.Run("StartReturnsNilAfterGracefulStop", func(t *testing.T) {
		for _, a := range adapters() {
			t.Run(a.Name, func(t *testing.T) {
				// Several rounds, each with a request served first, so a
				// "closed" error that surfaces only sometimes is caught.
				for round := range 5 {
					engine, addr := a.serve(t, serveConfig{})
					engine.Group("").GET("/ping", func(ctx httpx.Context) error {
						return ctx.Text(http.StatusOK, "pong")
					})

					served := make(chan error, 1)
					go func() { served <- engine.Start() }()
					waitReachable(t, addr)
					// No keep-alive: hertz's drain waits out an idle connection
					// until its ExitWaitTimeout, five seconds a round.
					resp, err := oneShotClient(2 * time.Second).Get("http://" + addr + "/ping")
					if err != nil {
						t.Fatalf("round %d request: %v", round, err)
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()

					stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					stopErr := engine.Stop(stopCtx)
					cancel()
					if stopErr != nil {
						t.Fatalf("round %d graceful Stop = %v, want nil", round, stopErr)
					}
					select {
					case err := <-served:
						if err != nil {
							t.Fatalf("round %d Start after graceful Stop = %v, want nil", round, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatalf("round %d Start did not return after Stop", round)
					}
					if engine.IsRunning() {
						t.Fatalf("round %d IsRunning() is true after Stop", round)
					}
					if err := startWithTimeout(t, engine); !errors.Is(err, httpx.ErrEngineClosed) {
						t.Fatalf("round %d restart after Stop = %v, want ErrEngineClosed", round, err)
					}
				}
			})
		}
	})
}

// startWithTimeout fails the test if Start blocks: a closed engine must return immediately.
func startWithTimeout(t *testing.T, engine httpx.Engine) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start()
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return; a closed engine must fail immediately")
		return nil
	}
}

func TestDoubleStartReturnsErrorAndKeepsServingConformance(t *testing.T) {
	t.Run("WhileRunning", func(t *testing.T) {
		for _, a := range adapters() {
			t.Run(a.Name, func(t *testing.T) {
				engine, addr := a.serve(t, serveConfig{})
				engine.Group("").GET("/ping", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "pong")
				})
				startServing(t, engine, addr)

				if err := startWithTimeout(t, engine); err == nil {
					t.Fatal("second Start() returned nil, want non-nil error")
				}

				resp, err := oneShotClient(2 * time.Second).Get("http://" + addr + "/ping")
				if err != nil {
					t.Fatalf("GET /ping after double Start: %v", err)
				}
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK || string(body) != "pong" {
					t.Fatalf("GET /ping = %d %q, want 200 pong", resp.StatusCode, body)
				}
			})
		}
	})

	t.Run("Concurrent", func(t *testing.T) {
		for _, a := range adapters() {
			t.Run(a.Name, func(t *testing.T) {
				engine, addr := a.serve(t, serveConfig{})
				engine.Group("").GET("/ping", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "pong")
				})

				errs := make(chan error, 2)
				var wg sync.WaitGroup
				wg.Add(2)
				for range 2 {
					go func() {
						defer wg.Done()
						errs <- engine.Start()
					}()
				}

				var firstErr error
				select {
				case firstErr = <-errs:
				case <-time.After(5 * time.Second):
					t.Fatal("concurrent double Start did not return an error from the loser")
				}
				if firstErr == nil {
					t.Fatal("first exiting Start in concurrent double-Start = nil, want error")
				}
				waitReachable(t, addr)

				resp, err := oneShotClient(2 * time.Second).Get("http://" + addr + "/ping")
				if err != nil {
					t.Fatalf("GET /ping: %v", err)
				}
				_ = resp.Body.Close()

				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := engine.Stop(stopCtx); err != nil {
					t.Fatalf("Stop: %v", err)
				}
				wg.Wait()
				if secondErr := <-errs; secondErr != nil {
					t.Fatalf("serving Start after Stop = %v, want nil", secondErr)
				}
			})
		}
	})
}
