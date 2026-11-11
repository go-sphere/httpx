package hertzx

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/go-sphere/httpx"
)

// A Stop issued while Start is still on its way to the listener must still end
// Start: hertz marks itself running only after its hooks and binds after
// that, so a single shutdown can land before there is anything to close.
func TestStopRightAfterStartEndsStart(t *testing.T) {
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	for i := range 30 {
		engine := New(WithAddr("127.0.0.1:0")).(*Engine)
		started := make(chan error, 1)
		go func() { started <- engine.Start() }()
		for !engine.IsRunning() {
			runtime.Gosched()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stopErr := engine.Stop(ctx)
		cancel()
		select {
		case err := <-started:
			if err != nil {
				t.Fatalf("iteration %d: Start returned %v, want nil", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Start did not return after Stop (Stop returned %v, IsRunning %v)", i, stopErr, engine.IsRunning())
		}
		if stopErr != nil {
			t.Fatalf("iteration %d: Stop returned %v, want nil", i, stopErr)
		}
		if engine.IsRunning() {
			t.Fatalf("iteration %d: IsRunning is true after Start returned", i)
		}
	}
}

// Stop's drain is bounded by the caller's context, not by hertz's
// ExitWaitTimeout default of 5 seconds.
func TestShutdownWaitFollowsContext(t *testing.T) {
	engine := New().(*Engine)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The engine is not running, so hertz refuses; the option is set first.
	_ = engine.shutdown(ctx)
	if got := engine.engine.GetOptions().ExitWaitTimeout; got < 59*time.Second || got > time.Minute {
		t.Fatalf("ExitWaitTimeout = %v with a one-minute deadline", got)
	}
	_ = engine.shutdown(context.Background())
	if got := engine.engine.GetOptions().ExitWaitTimeout; got != math.MaxInt64 {
		t.Fatalf("ExitWaitTimeout = %v without a deadline, want no limit", got)
	}
}

// WithErrorHandler(nil) keeps the handler configured before it.
func TestWithErrorHandlerNilIsIgnored(t *testing.T) {
	engine := New(
		WithErrorHandler(func(ctx httpx.Context, err error) {
			_ = ctx.Text(http.StatusTeapot, "kept")
		}),
		WithErrorHandler(nil),
	)
	engine.Group("").GET("/fail", func(httpx.Context) error {
		return errors.New("boom")
	})
	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "http://example.com/fail", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusTeapot || string(body) != "kept" {
		t.Fatalf("status=%d body=%q, want 418 %q", resp.StatusCode, body, "kept")
	}
}
