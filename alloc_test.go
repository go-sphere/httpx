//go:build !race

package httpx

import (
	"errors"
	"fmt"
	"testing"
)

type discardBuffer struct{}

func (discardBuffer) Write(p []byte) (int, error) { return len(p), nil }

// Classifying an error is on every error response of every adapter; it must
// not allocate, whatever the error's shape.
func TestClassifyErrorDoesNotAllocate(t *testing.T) {
	for name, err := range map[string]error{
		"plain":   errors.New("boom"),
		"typed":   NewNotFoundError("missing"),
		"wrapped": fmt.Errorf("ctx: %w", BadRequestError(errors.New("bad"))),
	} {
		if n := testing.AllocsPerRun(100, func() { ClassifyError(err) }); n != 0 {
			t.Errorf("%s: ClassifyError allocated %v times, want 0", name, n)
		}
	}
}

func TestSSEWriterZeroAlloc(t *testing.T) {
	// Pre-grow writer buffer once, then assert zero allocs on Send/Comment.
	var sink discardBuffer
	sinkWriter := NewSSEWriter(&sink)
	ev := &SSEEvent{Event: "tick", Data: "line1\r\nline2"}
	allocs := testing.AllocsPerRun(20, func() {
		if err := sinkWriter.Send(ev); err != nil {
			t.Fatal(err)
		}
		if err := sinkWriter.Comment("keep\r\nalive"); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("SSE Send+Comment allocs = %.1f, want 0", allocs)
	}
}

func TestToAnonymousWildcardPathZeroAlloc(t *testing.T) {
	for _, p := range []string{"/users/profile", "/*filepath", "/files/*path", "/files/*"} {
		allocs := testing.AllocsPerRun(50, func() {
			if got := toAnonymousWildcardPath(p); got == "" {
				t.Fatal("unexpected empty path")
			}
		})
		if allocs != 0 {
			t.Fatalf("toAnonymousWildcardPath(%q) allocs = %.1f, want 0", p, allocs)
		}
	}
}
