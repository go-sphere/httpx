package stdx

import (
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware
// (func(http.Handler) http.Handler) as httpx.Middleware, giving access to the
// otel/chi/gzip ecosystem. Request mutations (including context values) and
// response-writer wrapping are propagated to the downstream httpx chain. If
// the middleware responds without calling the next handler, the chain is
// short-circuited.
//
// On this adapter the middleware runs on the server's own writer and request,
// with no translation in either direction. The downstream context owns a copy
// of request-local state, so a timed-out handler cannot access a reused context.
// State changes propagate back only when the handler finishes before the
// middleware returns.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		native, ok := httpx.AsNativeContext[*Native](ctx)
		if !ok {
			return errors.New("AdaptStdMiddleware: invalid context type")
		}
		parent := native.c
		// The continuation owns its mutable state. TimeoutHandler can return while
		// inner is still running; neither that goroutine nor its writer may refer
		// to the parent's pooled context after this function returns.
		child := &stdContext{
			engine: parent.engine, req: parent.req, route: parent.route,
			values: slices.Clone(parent.values), chain: parent.chain, leaf: parent.leaf,
			index: parent.index, keys: maps.Clone(parent.keys),
		}
		child.native.c = child
		recorder := &commitRecorder{responseWriter: responseWriter{
			ResponseWriter: parent.rw.ResponseWriter, status: parent.rw.status, written: parent.rw.written,
		}}
		parentStatus, parentWritten := parent.rw.status, parent.rw.written
		done := make(chan error, 1)
		inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			child.req = req
			child.rw = responseWriter{ResponseWriter: w, status: parentStatus, written: parentWritten}
			var err error
			if continuation, ok := ctx.(interface{ NextWithContext(httpx.Context) error }); ok {
				err = continuation.NextWithContext(child)
			} else {
				err = child.Next()
			}
			done <- err
		})
		middleware(inner).ServeHTTP(recorder, parent.req)
		// Only a completed continuation can be merged. A timed-out continuation
		// stays isolated and becomes garbage once its handler eventually exits.
		var nextErr error
		select {
		case nextErr = <-done:
			parent.req, parent.keys = child.req, child.keys
			if !recorder.written {
				parent.rw.status = child.rw.status
			}
		default:
		}
		if recorder.written {
			parent.rw.markWritten(recorder.status)
		}
		return nextErr
	}
}

// commitRecorder tracks actual output independently of either Context. It
// retains the real writer's optional capabilities through responseWriter.
type commitRecorder struct{ responseWriter }
