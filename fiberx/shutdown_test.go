package fiberx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// The classification keeps a failed shutdown from being reported as a successful
// one. It is pinned here because only one of its three inputs can be produced on
// demand from a live server: fasthttp decides between the context error and the
// listener error, and the caller does not get to choose.
func TestClassifyShutdownError(t *testing.T) {
	lnErr := errors.New("close tcp 127.0.0.1:0: boom")

	expired, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want error
	}{
		{"clean stop", context.Background(), nil, nil},
		{"clean stop, deadline already spent", expired, nil, nil},
		{"listener close failed", context.Background(), lnErr, lnErr},
		// fasthttp closes the listeners and collects their errors before
		// consulting the context, so a real close failure can arrive with the
		// deadline already spent — the case a ctx.Err()-only classifier misses.
		{"listener close failed, deadline already spent", expired, lnErr, lnErr},
		// The drain outlived the caller's deadline: degraded, but the listeners
		// are down, so httpx.Close's semantic is success.
		{"drain cut short", expired, context.Canceled, nil},
		{"drain cut short, wrapped", expired, errors.Join(context.Canceled), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyShutdownError(tc.ctx, tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("classifyShutdownError = %v, want %v", got, tc.want)
			}
		})
	}
}

// A listener whose Accept reports its own error once closed, rather than one
// fasthttp recognizes as "use of closed network connection" — a wrapping or
// third-party listener, say.
type opaqueCloseListener struct {
	net.Listener
}

var errOpaqueClosed = errors.New("fiberx test: listener shut")

func (l opaqueCloseListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if errors.Is(err, net.ErrClosed) {
		return nil, errOpaqueClosed
	}
	return conn, err
}

// Start returns nil after a graceful Stop whatever error the listener reports
// for having been closed: fasthttp passes any accept error it does not
// recognize as a closed listener through Serve, and net/http's Serve answers
// ErrServerClosed for every accept error after Shutdown, which the net/http
// adapters turn into nil.
func TestStartReturnsNilAfterStopWithOpaqueListener(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serving := make(chan struct{})
	engine := New(WithListener(opaqueCloseListener{base}, fiber.ListenConfig{
		DisableStartupMessage: true,
		BeforeServeFunc:       func(*fiber.App) error { close(serving); return nil },
	}))

	served := make(chan error, 1)
	go func() { served <- engine.Start() }()
	<-serving
	// BeforeServeFunc runs just before Serve; give the accept loop a moment.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Stop(stopCtx); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Start after graceful Stop = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}
