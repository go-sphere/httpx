package hertzx

import (
	"bytes"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware (func(http.Handler)
// http.Handler) as httpx.Middleware, giving access to the otel/chi/gzip
// ecosystem. The middleware's writer starts with the headers the response
// already carries, and the changes it makes replace hertz's values for those
// keys.
//
// Because hertz buffers the response, the downstream chain runs first into
// hertz's buffer, which is then replayed through the middleware's (possibly
// wrapped) writer, so body/header transformations apply. A streamed response
// (Stream, Flush, DataFromReader or File with a body stream) is passed through,
// not replayed or read into memory: the headers the middleware set before
// calling next, except Content-Length and Content-Encoding, are added where the
// handler did not set the same key, and whatever the middleware writes after
// that is dropped.
//
// A response the chain left uncommitted, after an error or because nothing
// wrote, is not replayed: the headers the middleware changed are applied
// without committing, and an error is returned to be rendered as on the other
// adapters. The same holds when the middleware stops the chain without writing,
// so a layer above can still answer.
//
// Request bridging is one-way except for context values: r.Context() propagates
// downstream to the httpx chain, while in-place mutations to other
// *http.Request fields (such as r.Header, r.URL, or r.Body) do not write back
// to Hertz.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(next httpx.Handler) httpx.Handler { return next }
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			rc, ok := httpx.AsNativeContext[*app.RequestContext](ctx)
			if !ok {
				return errors.New("AdaptStdMiddleware: invalid context type")
			}
			req, err := compatRequest(ctx.Context(), rc)
			if err != nil {
				return err
			}
			w := newStdResponseWriter(rc)
			var nextErr error
			replayed := false
			inner := http.HandlerFunc(func(iw http.ResponseWriter, r *http.Request) {
				ctx.SetContext(r.Context())
				nextErr = next(ctx)
				switch {
				case w.streaming || w.late || rc.Response.GetHijackWriter() != nil:
					// Streamed past w, or committed before it: the bytes the
					// chain added are already where they belong.
				case rc.Response.IsBodyStream():
					// Replaying would read the whole stream into memory before
					// the first byte reached the client.
					w.passStream()
				case ctx.Committed():
					w.replay(iw)
					replayed = true
				default:
					w.mergeHertzHeader()
				}
			})
			pushStdWriter(rc, w)
			defer popStdWriter(rc)
			middleware(inner).ServeHTTP(w, req)
			if !w.wroteHeader {
				if replayed {
					// The middleware swallowed a committed response.
					w.WriteHeader(rc.Response.StatusCode())
				} else {
					w.applyHeader()
				}
			}
			return nextErr
		}
	}
}

// replay moves the response the chain committed out of hertz's buffer and
// writes it again through iw (the middleware's view of the response), so a
// wrapping middleware observes and may transform it. A wrapping writer that
// keeps a header map of its own gets the header changes the chain made, which
// is where the chain would have written them on net/http.
func (w *stdResponseWriter) replay(iw http.ResponseWriter) {
	before := w.base.Clone()
	w.mergeHertzHeader()
	if h := iw.Header(); !sameHeaderMap(h, w.header) {
		for key, values := range w.base {
			if !slices.Equal(values, before[key]) {
				h[key] = slices.Clone(values)
			}
		}
		for key := range before {
			if _, ok := w.base[key]; !ok {
				delete(h, key)
			}
		}
	}
	status := w.rc.Response.StatusCode()
	body := bytes.Clone(w.rc.Response.Body())
	w.rc.Response.ResetBody()
	iw.WriteHeader(status)
	if len(body) > 0 {
		_, _ = iw.Write(body)
	}
}

// sameHeaderMap reports whether a and b are the same map, not merely equal.
func sameHeaderMap(a, b http.Header) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

// passStream commits w over a streamed downstream response without touching
// its body.
func (w *stdResponseWriter) passStream() {
	w.wroteHeader = true
	w.streaming = true
	w.passStreamHeader()
}

// pendingStdWritersKey holds the writers of the AdaptStdMiddleware layers
// running on the request, outermost first.
const pendingStdWritersKey = "httpx.hertzx.pendingStdWriters"

func pushStdWriter(rc *app.RequestContext, w *stdResponseWriter) {
	writers, _ := rc.Value(pendingStdWritersKey).([]*stdResponseWriter)
	rc.Set(pendingStdWritersKey, append(writers, w))
}

func popStdWriter(rc *app.RequestContext) {
	writers, _ := rc.Value(pendingStdWritersKey).([]*stdResponseWriter)
	if len(writers) > 0 {
		rc.Set(pendingStdWritersKey, writers[:len(writers)-1])
	}
}

// commitPendingStdWriters commits the writers of the enclosing
// AdaptStdMiddleware layers over a streamed response, so the headers they
// staged are in hertz's response before its header block is written ahead of
// the handler returning. Staged headers are added only for the keys the
// handler left unset; Content-Length and Content-Encoding are dropped.
func commitPendingStdWriters(rc *app.RequestContext) {
	writers, _ := rc.Value(pendingStdWritersKey).([]*stdResponseWriter)
	for _, w := range writers {
		if !w.wroteHeader {
			w.wroteHeader = true
			rc.Set(responseCommittedKey, true)
			w.passStreamHeader()
		}
		w.streaming = true
	}
}

// passStreamHeader adds the headers the middleware changed to hertz's
// response for the keys the handler left unset.
func (w *stdResponseWriter) passStreamHeader() {
	header := &w.rc.Response.Header
	for key, values := range w.header {
		// The stream bypasses the middleware's writer, so a length or encoding
		// it staged would not describe the body.
		if slices.Equal(values, w.base[key]) || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") {
			continue
		}
		set := header.Peek(key)
		if strings.EqualFold(key, "Content-Type") {
			set = explicitContentType(header)
		}
		if len(set) > 0 {
			continue
		}
		for _, value := range values {
			header.Add(key, value)
		}
	}
}
