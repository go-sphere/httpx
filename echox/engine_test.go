package echox

import (
	"context"
	"net"
	"testing"
	"time"
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

	engine := New(WithServerAddr(addr))
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
