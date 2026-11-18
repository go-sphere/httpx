//go:build !race

package hertzx

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

// Text costs no more allocations than hertz's own String: committing a
// non-empty body records nothing in the request's key map.
func TestTextAllocatesNoMoreThanNative(t *testing.T) {
	rc := app.NewContext(0)
	native := testing.AllocsPerRun(100, func() {
		rc.Reset()
		rc.String(http.StatusOK, "ok")
	})
	hc := newHertzContext(context.Background(), rc)
	got := testing.AllocsPerRun(100, func() {
		rc.Reset()
		_ = hc.Text(http.StatusOK, "ok")
	})
	t.Logf("Text: %v allocs, native String: %v", got, native)
	if got > native {
		t.Fatalf("Text allocates %v times per call, hertz's String %v", got, native)
	}
}
