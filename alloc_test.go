//go:build !race

package httpx

import (
	"errors"
	"fmt"
	"testing"
)

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
