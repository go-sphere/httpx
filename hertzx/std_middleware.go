package hertzx

import (
	"bytes"
	"errors"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware (func(http.Handler)
// http.Handler) as httpx.Middleware, giving access to the otel/chi/gzip
// ecosystem. Because hertz buffers the response, the downstream chain runs
// first into hertz's buffer, which is then replayed through the middleware's
// (possibly wrapped) writer, so body/header transformations apply. Streaming
// responses (DataFromReader with a body stream, Stream, Flush) are not replayed
// through the wrapper; the headers the middleware set before calling next,
// except Content-Length and Content-Encoding, are added where the handler did
// not set the same key when the stream's header block is sent, and whatever the
// middleware writes after that is dropped. If the middleware responds without
// calling the next handler, the chain is short-circuited. A downstream error
// that left the response uncommitted is not replayed: only the headers the
// middleware staged are applied, and the error is returned to be rendered as on
// the other adapters.
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
			var nextErr error
			ran := false
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = true
				ctx.SetContext(r.Context())
				nextErr = next(ctx)
				if nextErr != nil && !ctx.Committed() {
					return
				}
				if rc.Response.GetHijackWriter() != nil {
					// Streamed: the response already left through the hijack
					// writer, and replaying would reset it.
					return
				}
				replayHertzResponse(rc, w)
			})
			w := &stdResponseWriter{rc: rc}
			pushStdWriter(rc, w)
			defer popStdWriter(rc)
			middleware(inner).ServeHTTP(w, req)
			if !w.wroteHeader {
				if nextErr != nil {
					// Replaying would commit an empty 200 and the error would
					// never be rendered.
					w.applyHeader()
				} else {
					w.WriteHeader(rc.Response.StatusCode())
				}
			}
			if !ran && !rc.IsAborted() {
				rc.Abort()
			}
			return nextErr
		}
	}
}

// replayHertzResponse moves the buffered downstream response out of the
// hertz context and replays it through w (the middleware's view of the
// response), so wrapping middleware observes and may transform it.
func replayHertzResponse(rc *app.RequestContext, w http.ResponseWriter) {
	status := rc.Response.StatusCode()
	header := w.Header()
	rc.Response.Header.VisitAll(func(k, v []byte) {
		if textproto.CanonicalMIMEHeaderKey(string(k)) == "Content-Length" {
			return
		}
		header.Add(string(k), string(v))
	})
	body := bytes.Clone(rc.Response.Body())
	rc.Response.Reset()
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
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

// passStreamHeader adds the staged headers to hertz's response for the keys
// the handler left unset.
func (w *stdResponseWriter) passStreamHeader() {
	for key, values := range w.header {
		// The stream bypasses the middleware's writer, so a length or encoding
		// it staged would not describe the body.
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") || len(w.rc.Response.Header.Peek(key)) > 0 {
			continue
		}
		for _, value := range values {
			w.rc.Response.Header.Add(key, value)
		}
	}
}
