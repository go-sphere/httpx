package httpxtest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Stream", casesStream)
}

// Streaming and SSE under in-process dispatch: the final response must be the
// same on every adapter. Incremental *delivery* needs a real connection and
// stays in the four-framework module.
func casesStream(t *testing.T, r runner) {
	t.Run("StreamBuffered", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/stream/buffered", func(ctx httpx.Context) error {
				s, ok := httpx.AsStreamer(ctx)
				if !ok {
					return httpx.NewInternalServerError("Streamer not supported")
				}
				return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
					if _, err := io.WriteString(w, "data: one\n\n"); err != nil {
						return err
					}
					_, err := io.WriteString(w, "data: two\n\n")
					return err
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/stream/buffered", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d; body=%q", got.Status, got.Body)
		}
		if got.Body != "data: one\n\ndata: two\n\n" {
			t.Fatalf("body = %q, want the two events in order", got.Body)
		}
		if ct := got.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("content-type = %q, want text/event-stream", ct)
		}
	})

	// The WHATWG event-stream framing is byte-exact: comments, bare data,
	// multi-line data, id/event/retry fields and JSON payloads.
	t.Run("ServerSentEvents", func(t *testing.T) {
		const wantBody = ": ready\n\n" +
			"data: plain\n\n" +
			"event: update\nid: 1\nretry: 3000\ndata: line one\ndata: line two\n\n" +
			"event: delta\ndata: {\"text\":\"chunk\"}\n\n"

		got := r.serve(t, func(router httpx.Router) {
			router.GET("/sse/events", func(ctx httpx.Context) error {
				return httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
					if err := w.Comment("ready"); err != nil {
						return err
					}
					if err := w.SendData("plain"); err != nil {
						return err
					}
					if err := w.Send(&httpx.SSEEvent{
						ID:    "1",
						Event: "update",
						Data:  "line one\nline two",
						Retry: 3 * time.Second,
					}); err != nil {
						return err
					}
					return w.SendJSON("delta", map[string]string{"text": "chunk"})
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/sse/events", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d; body=%q", got.Status, got.Body)
		}
		if got.Body != wantBody {
			t.Fatalf("body = %q,\nwant %q", got.Body, wantBody)
		}
		for _, h := range []struct{ key, want string }{
			{"Content-Type", httpx.ContentTypeEventStream},
			{"Cache-Control", "no-cache"},
			{"X-Accel-Buffering", "no"},
		} {
			if v := got.Headers.Get(h.key); v != h.want {
				t.Fatalf("%s = %q, want %q", h.key, v, h.want)
			}
		}
	})

	// Flush is optional; where it is supported it must not disturb the
	// response that follows. See Caps.Flusher.
	t.Run("FlushDoesNotDisturbResponse", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/flush/cap", func(ctx httpx.Context) error {
				fl, ok := httpx.AsFlusher(ctx)
				if ok != r.suite.Caps.Flusher {
					return httpx.NewInternalServerError("Flusher support does not match Caps.Flusher")
				}
				if ok {
					ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
					ctx.Status(http.StatusOK)
					if err := fl.Flush(); err != nil {
						return err
					}
				}
				return ctx.Text(http.StatusOK, "after-flush")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/flush/cap", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d; body=%q", got.Status, got.Body)
		}
		if got.Body != "after-flush" {
			t.Fatalf("body = %q, want %q", got.Body, "after-flush")
		}
	})
}

func init() {
	register("ErrorHandling", casesErrorHandling)
}

// The error handler is the single place a failure becomes a response, so both
// the default and a configured one are part of the contract.
func casesErrorHandling(t *testing.T, r runner) {
	// The default handler renders status, success and message — and must not
	// leak the raw error string.
	t.Run("DefaultHandlerUsesStatusAndDoesNotLeak", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				return httpx.NewUnauthorizedError("login required")
			})
			router.GET("/mw/unauth", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/unauth", nil))

		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusUnauthorized, got.Body)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload["success"] != false {
			t.Fatalf("success = %v, want false", payload["success"])
		}
		if payload["message"] != "login required" {
			t.Fatalf("message = %v, want %q", payload["message"], "login required")
		}
		if _, leaked := payload["error"]; leaked {
			t.Fatalf("the raw error leaked into the body: %v", payload["error"])
		}
	})

	// A configured handler that renders without aborting must still stop the
	// chain: stopping is the adapter's job, not the handler's.
	t.Run("CustomHandlerWithoutAbortStillStopsChain", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				return errors.New("middleware boom")
			})
			router.Use(func(ctx httpx.Context) error {
				ctx.SetHeader("X-Should-Not-Run", "1")
				return ctx.Next()
			})
			router.GET("/mw/error/custom", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Handler-Ran", "1")
				return ctx.Text(http.StatusOK, "handler-ran")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/error/custom", nil))

		if got.Status != http.StatusTeapot {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusTeapot, got.Body)
		}
		if got.Headers.Get("X-Should-Not-Run") != "" || got.Headers.Get("X-Handler-Ran") != "" {
			t.Fatalf("the chain continued after the error: headers=%v", got.Headers)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload["error"] != "middleware boom" {
			t.Fatalf("error = %q, want %q", payload["error"], "middleware boom")
		}
	})

	// A configured handler that only records a status and a header — the
	// shape of a handler that leaves the body to a proxy or writes nothing on
	// purpose — still owes that response. An adapter that only commits on a
	// body write would answer 200 here.
	statusOnlyHandler := func(ctx httpx.Context, err error) {
		ctx.SetHeader("X-Trace", "error:"+err.Error())
		ctx.Status(http.StatusBadGateway)
	}
	t.Run("StatusOnlyHandlerCommitsStatus", func(t *testing.T) {
		r.assertGoldenWith(t, Options{ErrorHandler: statusOnlyHandler}, func(router httpx.Router) {
			router.GET("/mw/error/status-only", func(ctx httpx.Context) error {
				return errors.New("upstream down")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/error/status-only", nil))
	})

	// The same handler on the middleware error path, which every adapter
	// implements separately from the route path.
	t.Run("StatusOnlyHandlerCommitsStatusFromMiddleware", func(t *testing.T) {
		r.assertGoldenWith(t, Options{ErrorHandler: statusOnlyHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				return errors.New("upstream down")
			})
			router.GET("/mw/error/status-only-mw", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "never")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/error/status-only-mw", nil))
	})
}
