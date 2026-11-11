package httpx

import (
	"context"
	"testing"
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
