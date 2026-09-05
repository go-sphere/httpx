package conformance

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// TestServerSentEventsConformance covers the SSE layer over Streamer: every
// adapter must produce the identical event-stream body, Content-Type, and
// the anti-buffering headers set by httpx.ServerSentEvents.
func TestServerSentEventsConformance(t *testing.T) {
	const wantBody = ": ready\n\n" +
		"data: plain\n\n" +
		"event: update\nid: 1\nretry: 3000\ndata: line one\ndata: line two\n\n" +
		"event: delta\ndata: {\"text\":\"chunk\"}\n\n"

	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/sse/events", func(ctx httpx.Context) error {
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

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/sse/events", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d; body=%q", name, got.Status, got.Body)
			}
			if got.Body != wantBody {
				t.Fatalf("%s body = %q, want %q", name, got.Body, wantBody)
			}
			if ct := got.Headers.Get("Content-Type"); ct != httpx.ContentTypeEventStream {
				t.Fatalf("%s content-type = %q, want %q", name, ct, httpx.ContentTypeEventStream)
			}
			if cc := got.Headers.Get("Cache-Control"); cc != "no-cache" {
				t.Fatalf("%s cache-control = %q, want %q", name, cc, "no-cache")
			}
			if ab := got.Headers.Get("X-Accel-Buffering"); ab != "no" {
				t.Fatalf("%s x-accel-buffering = %q, want %q", name, ab, "no")
			}
		})
	}
}
