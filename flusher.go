package httpx

import "io"

// Flusher is an optional Context capability that sends buffered response
// data to the client immediately, enabling progressive responses.
//
// Adapters over net/http (gin, echo) and hertz (via a chunked body writer
// hijack on first flush) implement it. Fiber's response model buffers until
// the handler returns, so the fiber adapter does not implement Flusher —
// probe with AsFlusher and fall back, or use Streamer, which every adapter
// implements.
type Flusher interface {
	// Flush writes any buffered response data to the client. The first
	// flush commits the status line and headers. In in-process test
	// dispatch (httpx.TestRequester) there is no live connection; Flush is
	// then a no-op and the response stays buffered.
	Flush() error
}

// AsFlusher returns the incremental-flush capability when supported.
func AsFlusher(ctx Context) (Flusher, bool) {
	f, ok := ctx.(Flusher)
	return f, ok
}

// Streamer is an optional Context capability for incremental response
// streaming such as server-sent events. Every official adapter implements it.
type Streamer interface {
	// Stream commits the response with the given status code and content
	// type, then invokes fn with a writer whose writes are delivered to the
	// client incrementally (each write is flushed).
	//
	// On adapters with a buffered response model (fiber), fn runs after the
	// handler returns; in that case Stream returns nil immediately and an
	// error from fn only terminates the stream — it cannot change the
	// status or reach the caller. Treat fn's error as best-effort cleanup
	// on every adapter: by the time fn runs, the response is committed.
	//
	// In in-process test dispatch there is no live connection; writes are
	// buffered and returned as the final response body.
	Stream(code int, contentType string, fn func(w io.Writer) error) error
}

// AsStreamer returns the incremental-streaming capability when supported.
func AsStreamer(ctx Context) (Streamer, bool) {
	s, ok := ctx.(Streamer)
	return s, ok
}
