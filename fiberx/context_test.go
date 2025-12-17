package fiberx

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx/testutil"
	"github.com/gofiber/fiber/v3"
)

func Test_fiberContext_Abort(t *testing.T) {
	app := fiber.New()
	engine := New(WithEngine(app))

	tracker := testutil.NewAbortTracker()
	testutil.SetupAbortEngine(engine, tracker)

	t.Run("abort stops chain", func(t *testing.T) {
		tracker.Reset()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test error: %v", err)
		}
		defer resp.Body.Close()
		if _, err = io.ReadAll(resp.Body); err != nil {
			t.Fatalf("read body: %v", err)
		}

		if got, want := tracker.Steps, []string{"before auth", "after abort"}; !testutil.EqualSlices(got, want) {
			t.Fatalf("unexpected steps: %v", tracker.Steps)
		}
		if len(tracker.AbortedStates) != 1 || !tracker.AbortedStates[0] {
			t.Fatalf("abort flag not set: %v", tracker.AbortedStates)
		}
	})

	t.Run("next continues when allowed", func(t *testing.T) {
		tracker.Reset()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("token", "123")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test error: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "success" {
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
