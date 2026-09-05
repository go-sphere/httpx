package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ContentTypeEventStream is the Content-Type of a server-sent events response.
const ContentTypeEventStream = "text/event-stream"

// ErrStreamerNotSupported is returned by ServerSentEvents when the Context
// does not implement the Streamer capability. Every official adapter
// implements Streamer, so this is only reachable with third-party Contexts.
var ErrStreamerNotSupported = errors.New("httpx: context does not support streaming")

// SSEEvent is a single server-sent event in the WHATWG event stream format.
//
// The zero value is not a valid event: at least one of ID, Event, Data, or
// Retry must be set or SSEWriter.Send reports an error.
type SSEEvent struct {
	// ID sets the event's "id:" field, which updates the client's last event
	// ID (echoed back in the Last-Event-ID request header on reconnect).
	// It must not contain CR, LF, or NUL characters.
	ID string

	// Event sets the "event:" field, the event type dispatched to the
	// client. Empty means the default "message" type.
	// It must not contain CR or LF characters.
	Event string

	// Data is the event payload, emitted as one "data:" line per line of
	// text (CRLF, CR, and LF are all treated as line breaks). Clients join
	// the lines back together with LF. An empty Data emits no "data:" lines;
	// standard EventSource clients do not dispatch such an event, though its
	// ID and Retry are still processed.
	Data string

	// Retry sets the "retry:" field, the client's reconnection delay. It is
	// encoded with millisecond resolution; values below one millisecond
	// (including zero and negative) are omitted.
	Retry time.Duration
}

// dataLineBreaks translates the three event-stream line break forms to LF so
// payload lines can be split uniformly.
var dataLineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// encode appends the wire form of e to buf. It reports an error for field
// values that would break event framing instead of writing them.
func (e *SSEEvent) encode(buf *bytes.Buffer) error {
	if e.ID == "" && e.Event == "" && e.Data == "" && e.Retry < time.Millisecond {
		return errors.New("httpx: SSE event is empty")
	}
	if strings.ContainsAny(e.ID, "\r\n\x00") {
		return fmt.Errorf("httpx: SSE event ID %q contains CR, LF, or NUL", e.ID)
	}
	if strings.ContainsAny(e.Event, "\r\n") {
		return fmt.Errorf("httpx: SSE event type %q contains CR or LF", e.Event)
	}
	if e.Event != "" {
		buf.WriteString("event: ")
		buf.WriteString(e.Event)
		buf.WriteByte('\n')
	}
	if e.ID != "" {
		buf.WriteString("id: ")
		buf.WriteString(e.ID)
		buf.WriteByte('\n')
	}
	if e.Retry >= time.Millisecond {
		buf.WriteString("retry: ")
		buf.WriteString(strconv.FormatInt(int64(e.Retry/time.Millisecond), 10))
		buf.WriteByte('\n')
	}
	if e.Data != "" {
		for line := range strings.SplitSeq(dataLineBreaks.Replace(e.Data), "\n") {
			buf.WriteString("data: ")
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	buf.WriteByte('\n')
	return nil
}

// SSEWriter encodes server-sent events onto a stream. Each event (or
// comment) is delivered with a single Write on the underlying writer, so on
// a Streamer-provided writer every event is flushed to the client as a unit.
//
// SSEWriter is not safe for concurrent use: like the Context it is built on,
// it must be driven from one goroutine at a time.
//
// A write error means the client is unreachable (e.g. it disconnected);
// callers should stop producing events and return the error.
type SSEWriter struct {
	w   io.Writer
	buf bytes.Buffer
}

// NewSSEWriter returns an SSEWriter that encodes events onto w. Handlers
// normally obtain one via ServerSentEvents; NewSSEWriter is the escape hatch
// for writing the event stream format onto an arbitrary writer.
func NewSSEWriter(w io.Writer) *SSEWriter {
	return &SSEWriter{w: w}
}

// Send encodes e and writes it to the stream as one flushed unit.
// It reports an error if e is empty, if a field value would break event
// framing (see the SSEEvent field docs), or if the underlying write fails.
func (s *SSEWriter) Send(e *SSEEvent) error {
	s.buf.Reset()
	if err := e.encode(&s.buf); err != nil {
		return err
	}
	return s.flushBuf()
}

// SendData sends an event of the default "message" type with the given
// payload. It is shorthand for Send(&SSEEvent{Data: data}).
func (s *SSEWriter) SendData(data string) error {
	return s.Send(&SSEEvent{Data: data})
}

// SendJSON marshals v as JSON and sends it as the payload of an event with
// the given type; an empty event means the default "message" type. JSON
// escapes line breaks, so the payload always encodes as a single data line.
func (s *SSEWriter) SendJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Send(&SSEEvent{Event: event, Data: string(data)})
}

// Comment writes a comment (a line starting with ":") to the stream.
// Clients ignore comments; a periodic Comment("") is the conventional
// keep-alive that defeats idle timeouts on proxies and load balancers.
// Line breaks in text produce one comment line each.
func (s *SSEWriter) Comment(text string) error {
	s.buf.Reset()
	for line := range strings.SplitSeq(dataLineBreaks.Replace(text), "\n") {
		s.buf.WriteString(": ")
		s.buf.WriteString(line)
		s.buf.WriteByte('\n')
	}
	s.buf.WriteByte('\n')
	return s.flushBuf()
}

func (s *SSEWriter) flushBuf() error {
	_, err := s.w.Write(s.buf.Bytes())
	return err
}

// ServerSentEvents commits a 200 "text/event-stream" response and invokes fn
// with an SSEWriter whose events are delivered to the client incrementally.
// It is the SSE layer over the Streamer capability and works on every
// official adapter.
//
// Before committing it sets "Cache-Control: no-cache" (SSE responses must
// not be cached) and "X-Accel-Buffering: no" (disables response buffering in
// nginx-style proxies, which would otherwise defeat streaming).
//
// Stream's execution model applies: on buffered adapters (fiber) fn runs
// after the handler returns and its error cannot reach the caller, so treat
// fn's error as best-effort cleanup — by the time fn runs, the response is
// committed. fn should return promptly when a send fails or the request
// context is done; there is no way to "un-commit" the stream.
//
// If ctx does not implement Streamer, ServerSentEvents returns
// ErrStreamerNotSupported without writing to the response.
func ServerSentEvents(ctx Context, fn func(w *SSEWriter) error) error {
	streamer, ok := AsStreamer(ctx)
	if !ok {
		return ErrStreamerNotSupported
	}
	ctx.SetHeader("Cache-Control", "no-cache")
	ctx.SetHeader("X-Accel-Buffering", "no")
	return streamer.Stream(http.StatusOK, ContentTypeEventStream, func(w io.Writer) error {
		return fn(NewSSEWriter(w))
	})
}
