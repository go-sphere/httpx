package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx/testutil"
)

func Test_ginContext_Abort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	backend := gin.New()
	engine := New(WithEngine(backend))

	tracker := testutil.NewAbortTracker()
	testutil.SetupAbortEngine(engine, tracker)

	t.Run("abort stops chain", func(t *testing.T) {
		tracker.Reset()

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		backend.ServeHTTP(w, req)

		if got, want := tracker.Steps, []string{"before auth", "after abort"}; !testutil.EqualSlices(got, want) {
			t.Fatalf("unexpected steps: %v", tracker.Steps)
		}
		if len(tracker.AbortedStates) != 1 || !tracker.AbortedStates[0] {
			t.Fatalf("abort flag not set: %v", tracker.AbortedStates)
		}
	})

	t.Run("next continues when allowed", func(t *testing.T) {
		tracker.Reset()

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("token", "123")
		backend.ServeHTTP(w, req)

		if body := w.Body.String(); body != "success" {
			t.Fatalf("unexpected body: %q", body)
		}

		if got, want := tracker.Steps, []string{"before auth", "second middleware", "group middleware", "handler", "after auth"}; !testutil.EqualSlices(got, want) {
			t.Fatalf("unexpected steps: %v", tracker.Steps)
		}
		if len(tracker.AbortedStates) != 0 {
			t.Fatalf("unexpected abort flags: %v", tracker.AbortedStates)
		}
	})
}
