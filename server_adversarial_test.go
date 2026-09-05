package httpx

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestAdversarialNilServerConcurrent verifies that Start(nil), Close(ctx, nil),
// and Close(nil, nil) are completely safe against data races and panics
// when invoked concurrently across 100 goroutines.
func TestAdversarialNilServerConcurrent(t *testing.T) {
	const goroutines = 100
	const iterations = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iterations*3)

	for range goroutines {
		wg.Go(func() {
			for i := 0; i < iterations; i++ {
				// 1. Concurrent Start(nil)
				if err := Start(nil); err != nil {
					errCh <- err
					return
				}

				// 2. Concurrent Close with background context
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				if err := Close(ctx, nil); err != nil {
					cancel()
					errCh <- err
					return
				}
				cancel()

				// 3. Concurrent Close with cancelled context
				cancelledCtx, cancelNow := context.WithCancel(context.Background())
				cancelNow()
				if err := Close(cancelledCtx, nil); err != nil {
					errCh <- err
					return
				}

				// 4. Concurrent Close with nil context. The nil server short-circuits
				// before the context is used, so the nil context is never dereferenced.
				// Note: Close(nil, srv) with a live server and active connections would
				// panic inside net/http's Shutdown polling; only the nil-server path is safe.
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
