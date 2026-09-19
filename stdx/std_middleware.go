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
// response-writer wrapping reach the downstream httpx chain, and a middleware
// that responds without calling next short-circuits it.
//
// The middleware runs on the server's own writer and request, with no
// translation in either direction. The downstream context owns a copy of
// request-local state, so a timed-out handler cannot reach a reused context,
// and state changes propagate back only when the handler finishes before the
// middleware returns.
//
// Unlike the other four adapters there is no UseNative here: stdx has no
// framework chain and no second middleware type, so this function is not a
// bridge but the implementation. Write Use(AdaptStdMiddleware(mw)).
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(next httpx.Handler) httpx.Handler { return next }
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			native, ok := httpx.AsNativeContext[*Native](ctx)
			if !ok {
				return errors.New("AdaptStdMiddleware: invalid context type")
			}
			parent := native.c
			// The continuation owns its mutable state. TimeoutHandler can return while
			// inner is still running; neither that goroutine nor its writer may refer
			// to the parent's pooled context after this function returns. The rest of
			// the chain therefore runs on child, not on the context handed in.
			child := &stdContext{
				engine: parent.engine, req: parent.req, route: parent.route,
				values: slices.Clone(parent.values), keys: maps.Clone(parent.keys),
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
				done <- next(child)
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
}

// commitRecorder tracks actual output independently of either Context. It
// retains the real writer's optional capabilities through responseWriter.
type commitRecorder struct{ responseWriter }
