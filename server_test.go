package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestStartAndCloseNilServer(t *testing.T) {
	if err := Start(nil); err != nil {
		t.Fatalf("Start(nil) expected nil error, got: %v", err)
	}
	if err := Close(context.Background(), nil); err != nil {
		t.Fatalf("Close(ctx, nil) expected nil error, got: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Close(cancelled, nil); err != nil {
		t.Fatalf("Close(cancelled, nil) expected nil error, got: %v", err)
	}
	// The nil server short-circuits before the context is used.
	if err := Close(nil, nil); err != nil { //nolint:staticcheck // Intentionally verifies the nil-server short-circuit.
		t.Fatalf("Close(nil, nil) expected nil error, got: %v", err)
	}
}

type failingCloseListener struct {
	net.Listener
	closeErr error
}

func (l failingCloseListener) Close() error {
	_ = l.Listener.Close()
	return l.closeErr
}

func TestCloseGracefulAndForcedAndListenerError(t *testing.T) {
	t.Run("GracefulAndForcedCutInFlight", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		inHandler := make(chan struct{})
		release := make(chan struct{})
		srv := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(inHandler)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}),
		}
		serveErr := make(chan error, 1)
		go func() {
			serveErr <- srv.Serve(ln)
		}()

		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		<-inHandler

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := Close(ctx, srv); err != nil {
			t.Fatalf("Close with expired ctx = %v, want nil (degraded forced close)", err)
		}
		close(release)
		if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve = %v, want http.ErrServerClosed", err)
		}
	})

	t.Run("NonContextShutdownErrorPreservedWhenCtxCancelled", func(t *testing.T) {
		rawLn, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		wantErr := errors.New("listener close boom")
		ln := failingCloseListener{Listener: rawLn, closeErr: wantErr}
		srv := &http.Server{Handler: http.NotFoundHandler()}
		serveErr := make(chan error, 1)
		go func() {
			serveErr <- srv.Serve(ln)
		}()
		// Wait briefly until Serve registers ln.
		time.Sleep(15 * time.Millisecond)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = Close(ctx, srv)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Close = %v, want errors.Is(_, %v)", err, wantErr)
		}
		if sErr := <-serveErr; sErr != nil && !errors.Is(sErr, http.ErrServerClosed) {
			t.Fatalf("Serve = %v", sErr)
		}
	})
}

func TestStartGracefulCloseAndListenError(t *testing.T) {
	t.Run("GracefulCloseReturnsNil", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}

		srv := &http.Server{
			Addr:              addr,
			Handler:           http.NotFoundHandler(),
			ReadHeaderTimeout: time.Second,
		}
		errCh := make(chan error, 1)
		go func() {
			errCh <- Start(srv)
		}()

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := Close(ctx, srv); err != nil {
			t.Fatalf("Close = %v, want nil", err)
		}

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Start after Close = %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return after Close")
		}
	})

	t.Run("ListenErrorReturned", func(t *testing.T) {
		srv := &http.Server{
			Addr:              "127.0.0.1:-1",
			Handler:           http.NotFoundHandler(),
			ReadHeaderTimeout: time.Second,
		}
		if err := Start(srv); err == nil {
			t.Fatal("Start with invalid port = nil, want non-nil error")
		}
	})
}
