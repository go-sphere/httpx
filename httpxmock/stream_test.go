package httpxmock_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

func TestFlushIsANoOpNotAnError(t *testing.T) {
	ctx := httpxmock.New(nil)
	flusher, ok := httpx.AsFlusher(ctx)
	if !ok {
		t.Fatal("AsFlusher: want supported")
	}

	// There is no connection to push to, and the contract makes that a no-op
	// rather than an error — the rule stdx violated by answering SSE
	// requests with an empty 200.
	if err := flusher.Flush(); err != nil {
		t.Fatalf("Flush = %v, want nil", err)
	}
	if !ctx.Committed() {
		t.Error("the first flush must commit the response")
	}
	if got := ctx.Flushes(); got != 1 {
		t.Errorf("Flushes = %d, want 1", got)
	}
}

func TestStream(t *testing.T) {
	ctx := httpxmock.New(nil)

	err := ctx.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
		for _, chunk := range []string{"one", "two", "three"} {
			if _, err := io.WriteString(w, chunk); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := ctx.StatusCode(); got != http.StatusOK {
		t.Errorf("status = %d", got)
	}
	if got := ctx.ResponseHeader("Content-Type"); got != "text/plain" {
		t.Errorf("content-type = %q", got)
	}
	if got := ctx.BodyString(); got != "onetwothree" {
		t.Errorf("body = %q", got)
	}
	// The boundaries are the point: the same bytes written in one call would
	// be indistinguishable without them.
	want := []int{0, 3, 6, 11}
	got := ctx.FlushOffsets()
	if len(got) != len(want) {
		t.Fatalf("FlushOffsets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FlushOffsets = %v, want %v", got, want)
		}
	}
}

func TestStreamErrorPropagates(t *testing.T) {
	ctx := httpxmock.New(nil)
	want := errors.New("client gone")

	err := ctx.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("Stream = %v, want %v", err, want)
	}
	// The response committed before the callback ran, so the error cannot
	// take it back.
	if !ctx.Committed() {
		t.Error("Committed = false after a failed stream")
	}
	if got := ctx.BodyString(); got != "partial" {
		t.Errorf("body = %q", got)
	}
}

func TestStreamCallbackMayReenterTheContext(t *testing.T) {
	// The callback is caller code and may touch the Context; the lock must
	// not be held across it.
	ctx := httpxmock.New(nil, httpxmock.WithState("user", "alice"))
	err := ctx.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
		user, _ := ctx.Get("user")
		_, err := io.WriteString(w, user.(string))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ctx.BodyString(); got != "alice" {
		t.Errorf("body = %q", got)
	}
}

// TestServerSentEvents drives the shared SSE layer, which is the reason to
// implement Streamer here at all: it is what a downstream SSE wrapper is
// built on and could not otherwise be unit-tested without an engine.
func TestServerSentEvents(t *testing.T) {
	ctx := httpxmock.New(nil)

	err := httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
		if err := w.SendData("first"); err != nil {
			return err
		}
		if err := w.SendJSON("tick", map[string]int{"n": 1}); err != nil {
			return err
		}
		return w.Comment("keep-alive")
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := ctx.StatusCode(); got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
	if got := ctx.ResponseHeader("Content-Type"); got != httpx.ContentTypeEventStream {
		t.Errorf("content-type = %q", got)
	}
	// The headers SSE sets before committing must be in the snapshot.
	if got := ctx.ResponseHeader("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := ctx.ResponseHeader("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q", got)
	}

	want := "data: first\n\nevent: tick\ndata: {\"n\":1}\n\n: keep-alive\n\n"
	if got := ctx.BodyString(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	// One flush committing the response plus one per event: an SSE writer
	// that buffered its events would look identical in the body alone.
	if got := ctx.Flushes(); got != 4 {
		t.Errorf("Flushes = %d, want 4 (commit + three events)", got)
	}
	if strings.Count(ctx.BodyString(), "\n\n") != 3 {
		t.Errorf("body does not carry three framed events: %q", ctx.BodyString())
	}

	writes := ctx.Writes()
	if len(writes) != 1 || writes[0].Kind != httpxmock.KindStream {
		t.Fatalf("Writes = %+v, want one Stream", writes)
	}
	if string(writes[0].Body) != want {
		t.Errorf("recorded stream body = %q", writes[0].Body)
	}
}

func TestStreamWithNilCallbackCommitsOnly(t *testing.T) {
	ctx := httpxmock.New(nil)
	if err := ctx.Stream(http.StatusAccepted, "text/event-stream", nil); err != nil {
		t.Fatal(err)
	}
	if got := ctx.StatusCode(); got != http.StatusAccepted {
		t.Errorf("status = %d", got)
	}
	if got := ctx.BodyString(); got != "" {
		t.Errorf("body = %q, want empty", got)
	}
}
