package hertzx

import (
	"bytes"
	"errors"
	"net/http"
	"net/textproto"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware
// (func(http.Handler) http.Handler) as httpx.Middleware, giving access to the
// otel/chi/gzip ecosystem. Because hertz buffers the response, the downstream
// chain runs first into hertz's buffer, which is then replayed through the
// middleware's (possibly wrapped) writer, so body/header transformations
// apply. Streaming responses (DataFromReader with a body stream) are not
// replayed through the wrapper. If the middleware responds without calling
// the next handler, the chain is short-circuited.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
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
			nextErr = ctx.Next()
			replayHertzResponse(rc, w)
		})
		w := &stdResponseWriter{rc: rc}
		middleware(inner).ServeHTTP(w, req)
		if !w.wroteHeader {
			w.WriteHeader(rc.Response.StatusCode())
		}
		if !ran && !rc.IsAborted() {
			rc.Abort()
		}
		return nextErr
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
