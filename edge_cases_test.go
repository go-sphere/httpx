package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLimitRequestBodyOverContentLengthSetsConnectionClose(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("12345678901234567890"))
	rec := httptest.NewRecorder()
	origBody := req.Body

	err := LimitRequestBody(rec, req, 10)
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) || mbe.Limit != 10 {
		t.Fatalf("LimitRequestBody err = %v, want *http.MaxBytesError{Limit: 10}", err)
	}
	if !req.Close {
		t.Fatal("req.Close = false, want true")
	}
	if got := rec.Header().Get("Connection"); got != "close" {
		t.Fatalf("Connection header = %q, want %q", got, "close")
	}
	// net/http picks how to close the connection from its own body type, so a
	// refused request keeps the body it arrived with.
	if req.Body != origBody {
		t.Fatal("a refused request's body was replaced; net/http could no longer tell it is unread")
	}

	reqNilW := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("12345678901234567890"))
	if err := LimitRequestBody(nil, reqNilW, 10); !errors.As(err, &mbe) || !reqNilW.Close {
		t.Fatalf("LimitRequestBody(nil, ...) err=%v close=%v, want MaxBytesError & Close=true", err, reqNilW.Close)
	}
}

type partialStatusWrapper struct {
	status int32
	err    error
}

func (e partialStatusWrapper) Error() string    { return e.err.Error() }
func (e partialStatusWrapper) Unwrap() error    { return e.err }
func (e partialStatusWrapper) GetStatus() int32 { return e.status }

type partialCodeWrapper struct {
	code int32
	err  error
}

func (e partialCodeWrapper) Error() string  { return e.err.Error() }
func (e partialCodeWrapper) Unwrap() error  { return e.err }
func (e partialCodeWrapper) GetCode() int32 { return e.code }

type partialMsgWrapper struct {
	msg string
	err error
}

func (e partialMsgWrapper) Error() string      { return e.err.Error() }
func (e partialMsgWrapper) Unwrap() error      { return e.err }
func (e partialMsgWrapper) GetMessage() string { return e.msg }

func TestParseErrorOuterPartialWrappersOverrideInnerError(t *testing.T) {
	inner := NewError(http.StatusBadRequest, 1001, "inner message", errors.New("boom"))

	wrapped := partialMsgWrapper{
		msg: "outer message",
		err: partialCodeWrapper{
			code: 2002,
			err: partialStatusWrapper{
				status: http.StatusTeapot,
				err:    inner,
			},
		},
	}

	code, status, msg := ParseError(wrapped)
	if code != 2002 || status != http.StatusTeapot || msg != "outer message" {
		t.Fatalf("ParseError = (%d, %d, %q), want (2002, 418, %q)", code, status, msg, "outer message")
	}

	// Only status overridden; code and message fall back to inner Error.
	statusOnly := partialStatusWrapper{status: http.StatusServiceUnavailable, err: inner}
	code, status, msg = ParseError(statusOnly)
	if code != 1001 || status != http.StatusServiceUnavailable || msg != "inner message" {
		t.Fatalf("ParseError(statusOnly) = (%d, %d, %q), want (1001, 503, %q)", code, status, msg, "inner message")
	}
}

type ctxEmbed interface {
	Context
}

type fakeStreamContext struct {
	ctxEmbed
	headers http.Header
	stream  bool
}

func (f *fakeStreamContext) SetHeader(k, v string) { f.headers.Set(k, v) }
func (f *fakeStreamContext) Stream(int, string, func(io.Writer) error) error {
	f.stream = true
	return nil
}

func TestSSEEdgeAndZeroAlloc(t *testing.T) {
	var buf bytes.Buffer
	w := NewSSEWriter(&buf)

	if err := w.Send(nil); err != nil {
		t.Fatalf("Send(nil) err = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Send(nil) wrote %q", buf.String())
	}

	fc := &fakeStreamContext{headers: make(http.Header)}
	if err := ServerSentEvents(fc, nil); err == nil {
		t.Fatal("ServerSentEvents(ctx, nil) returned nil, want error")
	}
	if fc.stream || len(fc.headers) != 0 {
		t.Fatalf("ServerSentEvents(ctx, nil) touched stream=%v headers=%v", fc.stream, fc.headers)
	}

	if err := w.Send(&SSEEvent{Event: "update", Data: "a\r\nb\rc\n"}); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	wantEvent := "event: update\ndata: a\ndata: b\ndata: c\ndata: \n\n"
	if buf.String() != wantEvent {
		t.Fatalf("Send multiline = %q, want %q", buf.String(), wantEvent)
	}

	buf.Reset()
	if err := w.Comment("x\r\ny"); err != nil {
		t.Fatalf("Comment err = %v", err)
	}
	if buf.String() != ": x\n: y\n\n" {
		t.Fatalf("Comment = %q, want %q", buf.String(), ": x\n: y\n\n")
	}

	buf.Reset()
	if err := w.SendJSON("tick", map[string]int{"n": 7}); err != nil {
		t.Fatalf("SendJSON err = %v", err)
	}
	if buf.String() != "event: tick\ndata: {\"n\":7}\n\n" {
		t.Fatalf("SendJSON = %q", buf.String())
	}
}

func TestMiddlewareChainNilIgnoredAndFixWildcardNil(t *testing.T) {
	chain := NewMiddlewareChain()
	chain.Use(nil, nil)
	if got := chain.Own(); len(got) != 0 {
		t.Fatalf("Own() after nil Use = %d, want 0", len(got))
	}
	fixed, param := FixWildcardPathIfNeed(nil, "/files/*path")
	if fixed != "/files/*" || param != "*" {
		t.Fatalf("FixWildcardPathIfNeed(nil, ...) = (%q, %q), want (%q, %q)", fixed, param, "/files/*", "*")
	}
}

func TestMiddlewareFallbackHandlerSingleLoad(t *testing.T) {
	chain := NewMiddlewareChain()
	leaf := func(Context) error { return nil }
	fb := NewMiddlewareFallback(chain, leaf)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			chain.Use(func(next Handler) Handler {
				return func(c Context) error { return next(c) }
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			if h := fb.Handler(); h == nil {
				t.Error("Handler() = nil")
				return
			}
		}
	}()
	wg.Wait()

	// Final call after mutations must see the latest chain snapshot.
	finalCalls := 0
	chain2 := NewMiddlewareChain()
	fb2 := NewMiddlewareFallback(chain2, func(Context) error { return nil })
	_ = fb2.Handler()
	chain2.Use(func(next Handler) Handler {
		return func(c Context) error {
			finalCalls++
			return next(c)
		}
	})
	_ = fb2.Handler()(nopContext{})
	if finalCalls != 1 {
		t.Fatalf("finalCalls = %d, want 1", finalCalls)
	}
}

type nopContext struct{ ctxEmbed }

func (nopContext) Context() context.Context { return context.Background() }
