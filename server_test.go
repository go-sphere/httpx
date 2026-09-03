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
}
