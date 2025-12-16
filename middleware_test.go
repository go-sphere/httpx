package httpx

import (
	"errors"
	"testing"
)

func TestMiddlewareChainExecutionOrder(t *testing.T) {
	var order []string

	record := func(label string) Middleware {
		return func(next Handler) Handler {
			return func(ctx Context) error {
				order = append(order, "before "+label)
				if err := next(ctx); err != nil {
					return err
				}
				order = append(order, "after "+label)
				return nil
			}
		}
	}

	chain := NewMiddlewareChain()
	chain.Use(record("A"), record("B"))

	h := chain.Then(func(Context) error {
		order = append(order, "handler")
		return nil
	})

	if err := h(nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	expected := []string{
		"before A",
		"before B",
		"handler",
		"after B",
		"after A",
	}

	if len(order) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(order), order)
	}

	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("order mismatch at %d: want %q got %q (full order %v)", i, expected[i], order[i], order)
		}
	}
}

func TestMiddlewareChainStopsOnError(t *testing.T) {
	var order []string
	targetErr := errors.New("boom")

	mwA := func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "before A")
			if err := next(ctx); err != nil {
				return err
			}
			order = append(order, "after A")
			return nil
		}
	}

	mwB := func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "before B")
			return targetErr
		}
	}

	chain := NewMiddlewareChain(mwA, mwB)
	h := chain.Then(func(Context) error {
		order = append(order, "handler")
		return nil
	})

	err := h(nil)
	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}

	expected := []string{"before A", "before B"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(order), order)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("order mismatch at %d: want %q got %q (full order %v)", i, expected[i], order[i], order)
		}
	}
}
