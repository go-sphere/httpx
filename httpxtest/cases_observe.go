package httpxtest

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Observe", casesObserve)
}

// The Observe cases pin what a layer sees when it looks at the response after
// next returns — the read an access log, a metrics collector or an audit trail
// makes. That reading is not the response the client gets, in both directions.
//
// An error is rendered where the chain was composed — at the route, after every
// layer has returned — so while a layer between the failure and the route
// unwinds, the status is still the framework's untouched default. And a handler
// that wrote before failing leaves the status at what it wrote, with the error
// no longer able to change it. A layer that logs StatusCode() as the outcome
// therefore records a 200 for a rejected request. The reliable signal is the
// error next returned: ClassifyError turns it into the status the client will
// get while nothing has been written yet.
type observeRecorder struct {
	before     int
	after      int
	err        error
	layerRan   int
	handlerRan int
}

// layer records the status on both sides of next. Before next it is the
// framework default; after it, it is what the layers below committed, which is
// the final status only when one of them committed a response.
func (rec *observeRecorder) layer(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		rec.layerRan++
		rec.before = ctx.StatusCode()
		err := next(ctx)
		rec.after = ctx.StatusCode()
		rec.err = err
		return err
	}
}

// tracing is layer plus the observation published through X-Trace, the header
// the golden contracts record, so every adapter is compared on what its layers
// saw and not only on what the client got.
func (rec *observeRecorder) tracing(next httpx.Handler) httpx.Handler {
	inner := rec.layer(next)
	return func(ctx httpx.Context) error {
		err := inner(ctx)
		rec.trace(ctx)
		return err
	}
}

// classifiedStatus is the status the client will get for the error next
// returned, or 0 when there was none.
func (rec *observeRecorder) classifiedStatus() int {
	if rec.err == nil {
		return 0
	}
	_, status, _ := httpx.ClassifyError(rec.err)
	return int(status)
}

func (rec *observeRecorder) trace(ctx httpx.Context) {
	ctx.SetHeader("X-Trace", fmt.Sprintf("before=%d after=%d error=%d", rec.before, rec.after, rec.classifiedStatus()))
}

func (rec *observeRecorder) requireObserved(t *testing.T, wantBefore, wantAfter, wantError int) {
	t.Helper()
	if rec.before != wantBefore || rec.after != wantAfter || rec.classifiedStatus() != wantError {
		t.Fatalf("observed before=%d after=%d error=%d, want before=%d after=%d error=%d",
			rec.before, rec.after, rec.classifiedStatus(), wantBefore, wantAfter, wantError)
	}
}

// denyLayer fails the request the way an authorization layer does, with an
// error and before anything is written.
func denyLayer(message string) httpx.Middleware {
	return func(httpx.Handler) httpx.Handler {
		return func(httpx.Context) error { return httpx.NewForbiddenError(message) }
	}
}

func casesObserve(t *testing.T, r runner) {
	// The layer outside a failing layer sees the unrendered response: the
	// client gets the error's 403, while StatusCode() still reports the 200 the
	// framework put there before anything ran. Both readings are asserted so an
	// adapter cannot pass by rendering early — which would break the layers
	// below, whose headers must still be able to reach the response — nor by
	// reporting the rendered status through StatusCode(), which would make it
	// depend on where the layer sits.
	t.Run("ErrorIsRenderedAfterTheLayersReturn", func(t *testing.T) {
		rec := &observeRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.tracing, denyLayer("observe-denied"))
			router.GET("/observe/rejected", func(ctx httpx.Context) error {
				rec.handlerRan++
				return ctx.Text(http.StatusOK, "never")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/observe/rejected", nil))

		rec.requireObserved(t, http.StatusOK, http.StatusOK, http.StatusForbidden)
		if rec.layerRan != 1 {
			t.Fatalf("the observing layer ran %d times, want 1", rec.layerRan)
		}
		if rec.handlerRan != 0 {
			t.Fatalf("the handler ran %d times, want 0: the failing layer must stop the chain", rec.handlerRan)
		}
		if got.Status != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%q: the error is rendered at the route, after the chain returns",
				got.Status, http.StatusForbidden, got.Body)
		}
		r.compareGolden(t, got)
	})

	// The other half: once a handler has committed a response, the status the
	// outer layer reads is the committed one, and the error it also gets can no
	// longer be rendered. A logging layer sees both signals at once and must
	// report the failure from the error; a layer that re-renders the error
	// would corrupt the response the client already started receiving.
	t.Run("CommittedResponseKeepsItsStatus", func(t *testing.T) {
		rec := &observeRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.layer)
			router.GET("/observe/committed", func(ctx httpx.Context) error {
				rec.handlerRan++
				if err := ctx.Text(http.StatusOK, "partial"); err != nil {
					return err
				}
				return errors.New("failed after writing")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/observe/committed", nil))

		rec.requireObserved(t, http.StatusOK, http.StatusOK, http.StatusInternalServerError)
		if rec.handlerRan != 1 {
			t.Fatalf("the handler ran %d times, want 1", rec.handlerRan)
		}
		if got.Status != http.StatusOK || got.Body != "partial" {
			t.Fatalf("response = %d %q, want 200 %q: an error after a committed response must not overwrite it",
				got.Status, got.Body, "partial")
		}
		r.compareGolden(t, got)
	})

	// A layer that logs the error and returns nil swallows it: the adapter sees
	// a chain that succeeded, renders nothing, and the client gets a 200 with no
	// body for a request that was rejected. Pinned because it is the failure
	// mode a logging layer is one missing return away from, and because it must
	// behave the same on every adapter rather than one of them inventing a 403
	// from a status the layer touched.
	t.Run("SwallowedErrorLeavesAnUnrenderedResponse", func(t *testing.T) {
		rec := &observeRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					rec.layerRan++
					err := next(ctx)
					rec.err = err
					ctx.SetHeader("X-Trace", fmt.Sprintf("swallowed=%v error=%d status=%d",
						err != nil, rec.classifiedStatus(), ctx.StatusCode()))
					return nil
				}
			}, denyLayer("observe-denied"))
			router.GET("/observe/swallowed", func(ctx httpx.Context) error {
				rec.handlerRan++
				return ctx.Text(http.StatusOK, "never")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/observe/swallowed", nil))

		if rec.err == nil {
			t.Fatal("the outer layer got nil from next: the failing layer's error did not reach it")
		}
		if rec.handlerRan != 0 {
			t.Fatalf("the handler ran %d times, want 0", rec.handlerRan)
		}
		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200: the chain returned nil, so there is nothing to render; body=%q", got.Status, got.Body)
		}
		if got.Body != "" {
			t.Fatalf("body = %q, want empty: nothing may be written for a swallowed error", got.Body)
		}
		r.compareGolden(t, got)
	})

	// The success path, for contrast: before next nothing is written either, but
	// after it the status is the handler's own. Asserted without a golden
	// because the response is an ordinary 201, already covered elsewhere; what
	// this case adds is the pair of readings.
	t.Run("SuccessPathStatusIsTheHandlers", func(t *testing.T) {
		rec := &observeRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.layer)
			router.GET("/observe/created", func(ctx httpx.Context) error {
				rec.handlerRan++
				return ctx.Text(http.StatusCreated, "created")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/observe/created", nil))

		rec.requireObserved(t, http.StatusOK, http.StatusCreated, 0)
		if got.Status != http.StatusCreated || got.Body != "created" {
			t.Fatalf("response = %d %q, want 201 %q", got.Status, got.Body, "created")
		}
	})
}
