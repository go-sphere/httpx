package httpxmock_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

// TestRunContinuesTheChain is the shape the four duplicated downstream
// drivers existed for: a layer that lets the request through.
func TestRunContinuesTheChain(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodGet, "/", nil,
		httpxmock.WithHeader("Authorization", "Bearer good"))
	next := &httpxmock.Handler{}

	pass := func(h httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error {
			c.Set("principal", c.Header("Authorization"))
			return h(c)
		}
	}

	if err := httpxmock.Run(ctx, next.Handle, pass); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if !next.Called() {
		t.Error("Called = false, want the chain to have reached the handler")
	}
	if got := next.Calls(); got != 1 {
		t.Errorf("Calls = %d, want 1", got)
	}
	if got, _ := ctx.Get("principal"); got != "Bearer good" {
		t.Errorf("the layer's state did not survive: %v", got)
	}
}

func TestRunStopsAtALayerThatDoesNotCallNext(t *testing.T) {
	ctx := httpxmock.New(nil)
	next := &httpxmock.Handler{}
	want := httpx.NewUnauthorizedError("no token")

	deny := func(httpx.Handler) httpx.Handler {
		return func(httpx.Context) error { return want }
	}

	err := httpxmock.Run(ctx, next.Handle, deny)
	if !errors.Is(err, want) {
		t.Errorf("Run = %v, want %v", err, want)
	}
	if next.Called() {
		t.Error("Called = true; a denied request must not reach the handler")
	}
	if ctx.Committed() {
		t.Error("the layer returned an error rather than writing, so nothing should be committed")
	}
}

func TestHandlerFnSuppliesTheTerminalBehavior(t *testing.T) {
	ctx := httpxmock.New(nil)
	want := errors.New("handler failed")
	next := &httpxmock.Handler{
		Fn: func(c httpx.Context) error {
			c.Status(http.StatusBadRequest)
			return want
		},
	}

	var seen error
	observe := func(h httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error {
			seen = h(c)
			return seen
		}
	}

	if err := httpxmock.Run(ctx, next.Handle, observe); !errors.Is(err, want) {
		t.Errorf("Run = %v, want %v", err, want)
	}
	if !errors.Is(seen, want) {
		t.Errorf("the layer saw %v, want %v", seen, want)
	}
	// What the observing layer reads off the Context is the pending status,
	// because the error has not been rendered yet.
	if got := ctx.StatusCode(); got != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", got)
	}
}

func TestRunComposesSeveralLayersOutermostFirst(t *testing.T) {
	ctx := httpxmock.New(nil)
	next := &httpxmock.Handler{
		Fn: func(httpx.Context) error { return nil },
	}

	var order []string
	layer := func(name string) httpx.Middleware {
		return func(h httpx.Handler) httpx.Handler {
			return func(c httpx.Context) error {
				order = append(order, name+"-pre")
				err := h(c)
				order = append(order, name+"-post")
				return err
			}
		}
	}

	if err := httpxmock.Run(ctx, next.Handle, layer("outer"), nil, layer("inner")); err != nil {
		t.Fatal(err)
	}
	want := []string{"outer-pre", "inner-pre", "inner-post", "outer-post"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if !next.Called() {
		t.Error("the chain did not reach the handler")
	}
}

func TestRunWithoutLayersOrTerminus(t *testing.T) {
	ctx := httpxmock.New(nil)

	// No layers: the terminus runs on its own.
	next := &httpxmock.Handler{}
	if err := httpxmock.Run(ctx, next.Handle); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if got := next.Calls(); got != 1 {
		t.Errorf("Calls = %d, want 1", got)
	}

	// No terminus: the chain still runs and the layer's response stands.
	respond := func(httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { return c.Text(http.StatusOK, "answered") }
	}
	if err := httpxmock.Run(ctx, nil, respond); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if got := ctx.BodyString(); got != "answered" {
		t.Errorf("body = %q", got)
	}
}

func TestHandlerIsRaceFree(t *testing.T) {
	// The property that removes the plain/atomic duplication downstream: one
	// Handler shared by every goroutine of a stress test.
	next := &httpxmock.Handler{}
	const workers = 8
	done := make(chan struct{}, workers)
	for range workers {
		go func() {
			defer func() { done <- struct{}{} }()
			ctx := httpxmock.New(nil)
			_ = httpxmock.Run(ctx, next.Handle)
			_ = next.Called()
		}()
	}
	for range workers {
		<-done
	}
	if got := next.Calls(); got != workers {
		t.Errorf("Calls = %d, want %d", got, workers)
	}
}
