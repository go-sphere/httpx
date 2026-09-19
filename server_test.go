package httpx

import (
	"context"
	"sync"
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
}

func TestStartAndCloseNilServerConcurrent(t *testing.T) {
	const goroutines = 100
	const iterations = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iterations*3)

	for range goroutines {
		wg.Go(func() {
			for i := 0; i < iterations; i++ {
				if err := Start(nil); err != nil {
					errCh <- err
					return
				}

				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				if err := Close(ctx, nil); err != nil {
					cancel()
					errCh <- err
					return
				}
				cancel()

				cancelledCtx, cancelNow := context.WithCancel(context.Background())
				cancelNow()
				if err := Close(cancelledCtx, nil); err != nil {
					errCh <- err
					return
				}

				// The nil server short-circuits before the context is used, so this
				// nil context is never dereferenced; Close(nil, srv) with a live
				// server and active connections would panic in net/http's Shutdown.
				if err := Close(nil, nil); err != nil { //nolint:staticcheck // Intentionally verifies the nil-server short-circuit.
					errCh <- err
					return
				}
			}
		})
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("unexpected error from nil server calls: %v", err)
	}
}
