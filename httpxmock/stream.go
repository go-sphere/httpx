package httpxmock

import "io"

// Flush implements httpx.Flusher. The first flush commits the response.
//
// It always reports nil. There is no connection behind a mock context, so
// there is nothing to push, and the httpx.Flusher contract makes a writer
// that cannot flush a no-op rather than an error — a rule this repository
// arrived at the hard way, after stdx answered an SSE request with an empty
// 200 by treating an unflushable writer as a failure. What the flush leaves
// behind is the boundary, readable with FlushOffsets.
func (c *Context) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Flush")
	c.res.Flush()
	return nil
}

// Stream implements httpx.Streamer: it commits the response with the given
// status and content type, then runs fn with a writer whose every write is
// recorded as a flush boundary.
//
// fn runs synchronously, before Stream returns, and its error is returned
// unchanged. That matches the net/http-backed adapters; on fiber the callback
// runs after the handler returns and its error cannot reach the caller, so a
// test asserting on that error is asserting something fiber does not promise.
//
// A nil fn commits the response and does nothing else, which is how a test
// checks the headers a stream sets without producing one.
func (c *Context) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	c.mu.Lock()
	c.note("Stream")
	if contentType != "" {
		c.res.Header().Set("Content-Type", contentType)
	}
	c.res.WriteHeader(code)
	c.res.Flush()
	start := c.res.body.Len()
	c.mu.Unlock()

	// The record is deferred so a callback that panics — which a recovery
	// middleware test will make one do — still leaves the write behind.
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.writes = append(c.writes, ResponseWrite{
			Kind:        KindStream,
			Code:        code,
			ContentType: contentType,
			Body:        c.bodySince(start),
		})
	}()

	if fn == nil {
		return nil
	}
	// The lock is released for the duration of fn: it is caller code, it may
	// call back into this Context, and holding the lock across it would
	// deadlock. streamWriter retakes the lock per write.
	return fn(streamWriter{c: c})
}

// FlushOffsets returns the response body length at each flush, in order.
//
// It is what makes an incremental response testable. Three server-sent events
// written and flushed one at a time produce the same bytes as three written
// in one go, and differ only in these boundaries — so an SSE test that only
// compares bodies cannot tell a working stream from a buffered one.
func (c *Context) FlushOffsets() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.res.flushes) == 0 {
		return nil
	}
	return append([]int(nil), c.res.flushes...)
}

// Flushes returns how many times the response was flushed.
func (c *Context) Flushes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.res.flushes)
}

// streamWriter is the writer Stream hands to its callback: each write appends
// to the response and flushes, which is the delivery guarantee
// httpx.Streamer makes.
type streamWriter struct{ c *Context }

func (w streamWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	n, err := w.c.res.Write(p)
	if err != nil {
		return n, err
	}
	w.c.res.Flush()
	return n, nil
}
