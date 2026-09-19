package fiberx

import (
	"context"
	"errors"
	"testing"
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
