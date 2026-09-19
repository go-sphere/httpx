package httpxtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Recovery", casesRecovery)
}

// The recovery cases pin what a panic-recovery layer needs from the chain
// design: a panic raised anywhere below it — in the route handler or in another
// layer, on the same scope or an inner one — unwinds to the recovery layer
// instead of the framework, and the layers outside it resume with an ordinary
// error. A recovery layer must be able to answer the request itself too, since
// that is the other shape a recovery layer takes (sphere's logger.RecoveryLog
// writes a 500 and returns nil), and the engine must keep serving afterwards.
//
// The panic value carries a distinctive sentinel so a case can assert it does
// not leak into the response body.
const panicSentinel = "panic-sentinel-b7f4"

type recoveryRecorder struct {
	panics     []any
	handlerRan int
	order      []string
	outerErr   error
}

func (rec *recoveryRecorder) mark(s string) { rec.order = append(rec.order, s) }

func (rec *recoveryRecorder) orderString() string { return strings.Join(rec.order, ",") }

// requireCaught asserts the panic unwound all the way to the recovery layer,
// with its value intact: a framework that recovered the panic itself would
// leave this empty and must fail the case even if it also answered 500.
func (rec *recoveryRecorder) requireCaught(t *testing.T) {
	t.Helper()
	if len(rec.panics) != 1 || rec.panics[0] != panicSentinel {
		t.Fatalf("the recovery layer saw %v, want exactly [%s]: the panic did not unwind to the layer", rec.panics, panicSentinel)
	}
}

// outer is the layer that must see the recovered failure as an ordinary error:
// it resumes after the recovery layer returns, and what it gets back from next
// is not nil.
func (rec *recoveryRecorder) outer(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		rec.mark("outer-pre")
		err := next(ctx)
		rec.outerErr = err
		rec.mark("outer-post")
		return err
	}
}

// recoverTo500 is the recovery layer under test in its error-returning shape:
// the panic value is kept as the error's cause for the log, while the message
// the client sees is fixed, which is what keeps a panic carrying a token or a
// query out of the response body. The error is returned rather than written so
// it is rendered where the chain was composed, like any other handler error.
func recoverTo500(rec *recoveryRecorder) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) (err error) {
			defer func() {
				if v := recover(); v != nil {
					rec.panics = append(rec.panics, v)
					err = httpx.InternalServerError(fmt.Errorf("panic: %v", v), "internal server error")
				}
			}()
			return next(ctx)
		}
	}
}

// recoverToNoContent is the other shape: the layer answers the request itself
// and returns nil, so the layers outside it resume without an error. Both
// shapes have to stop the panic.
func recoverToNoContent(rec *recoveryRecorder) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) (err error) {
			defer func() {
				if v := recover(); v != nil {
					rec.panics = append(rec.panics, v)
					ctx.SetHeader("X-Recovered", "1")
					err = ctx.NoContent(http.StatusInternalServerError)
				}
			}()
			return next(ctx)
		}
	}
}

func (rec *recoveryRecorder) panicHandler(httpx.Context) error {
	rec.handlerRan++
	rec.mark("handler-start")
	panic(panicSentinel)
}

// panicLayer is a middleware that panics before doing anything with next, the
// failure mode an outer recovery layer exists to contain: the layer, not the
// handler, is what blew up.
func (rec *recoveryRecorder) panicLayer(name string) httpx.Middleware {
	return func(httpx.Handler) httpx.Handler {
		return func(httpx.Context) error {
			rec.mark(name)
			panic(panicSentinel)
		}
	}
}

// requireRecoveredResponse asserts the response a recovered panic produced and
// that its body carries nothing of the panic value.
func requireRecoveredResponse(t *testing.T, got response) {
	t.Helper()
	if got.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusInternalServerError, got.Body)
	}
	if strings.Contains(got.Body, panicSentinel) {
		t.Fatalf("the panic value leaked into the response body: %q", got.Body)
	}
}

func casesRecovery(t *testing.T, r runner) {
	t.Run("RecoversHandlerPanic", func(t *testing.T) {
		rec := &recoveryRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.outer, recoverTo500(rec))
			router.GET("/recovery/panic", rec.panicHandler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/recovery/panic", nil))

		rec.requireCaught(t)
		if rec.handlerRan != 1 {
			t.Fatalf("the handler ran %d times, want 1", rec.handlerRan)
		}
		if order := rec.orderString(); order != "outer-pre,handler-start,outer-post" {
			t.Fatalf("order = %q, want %q: the layer outside the recovery layer must resume after the panic",
				order, "outer-pre,handler-start,outer-post")
		}
		if rec.outerErr == nil {
			t.Fatal("the outer layer got nil from next after a recovered panic")
		}
		if _, status, message := httpx.ParseError(rec.outerErr); status != http.StatusInternalServerError || message != "internal server error" {
			t.Fatalf("the outer layer saw status=%d message=%q, want 500 %q", status, message, "internal server error")
		}
		requireRecoveredResponse(t, got)
		r.compareGolden(t, got)
	})

	t.Run("RecoversLayerPanic", func(t *testing.T) {
		rec := &recoveryRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.outer, recoverTo500(rec), rec.panicLayer("layer-panic"))
			router.GET("/recovery/layer-panic", rec.panicHandler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/recovery/layer-panic", nil))

		rec.requireCaught(t)
		if rec.handlerRan != 0 {
			t.Fatalf("the handler ran %d times after a layer above it panicked, want 0", rec.handlerRan)
		}
		if order := rec.orderString(); order != "outer-pre,layer-panic,outer-post" {
			t.Fatalf("order = %q, want %q", order, "outer-pre,layer-panic,outer-post")
		}
		if rec.outerErr == nil {
			t.Fatal("the outer layer got nil from next after a recovered panic")
		}
		requireRecoveredResponse(t, got)
		r.compareGolden(t, got)
	})

	// Engine-scope layers run outside a group's wherever the registration calls
	// came in, so a recovery layer on the engine is what contains a panic in a
	// group's own layer — the composition order this test exists to pin.
	t.Run("EngineLayerRecoversGroupPanic", func(t *testing.T) {
		rec := &recoveryRecorder{}
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(rec.outer, recoverTo500(rec))
			group := engine.Group("/api")
			group.Use(rec.panicLayer("group-layer-panic"))
			group.GET("/panic", rec.panicHandler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/panic", nil))

		rec.requireCaught(t)
		if rec.handlerRan != 0 {
			t.Fatalf("the handler ran %d times after a group layer panicked, want 0", rec.handlerRan)
		}
		if order := rec.orderString(); order != "outer-pre,group-layer-panic,outer-post" {
			t.Fatalf("order = %q, want %q", order, "outer-pre,group-layer-panic,outer-post")
		}
		if rec.outerErr == nil {
			t.Fatal("the outer layer got nil from next after a recovered panic")
		}
		requireRecoveredResponse(t, got)
		r.compareGolden(t, got)
	})

	t.Run("RecoveryLayerOwnsTheResponse", func(t *testing.T) {
		rec := &recoveryRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.outer, recoverToNoContent(rec))
			router.GET("/recovery/answered", rec.panicHandler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/recovery/answered", nil))

		rec.requireCaught(t)
		if rec.outerErr != nil {
			t.Fatalf("the outer layer got %v, want nil: a recovery layer that answers the request itself returns no error", rec.outerErr)
		}
		if order := rec.orderString(); order != "outer-pre,handler-start,outer-post" {
			t.Fatalf("order = %q, want %q", order, "outer-pre,handler-start,outer-post")
		}
		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusInternalServerError, got.Body)
		}
		if got.Body != "" {
			t.Fatalf("body = %q, want empty: the recovery layer answered without a body", got.Body)
		}
		if got.Headers.Get("X-Recovered") != "1" {
			t.Fatal("the recovery layer's header did not reach the response")
		}
		r.compareGolden(t, got)
	})

	// A recovered panic must not leave the engine unable to serve: the next
	// request through the same engine has to be unaffected, which is also what
	// catches a pooled context released twice on the way out.
	t.Run("EngineStaysUsableAfterRecoveredPanic", func(t *testing.T) {
		rec := &recoveryRecorder{}
		engine := r.suite.NewEngine(t, Options{})
		engine.Use(recoverTo500(rec))
		engine.Group("").GET("/recovery/panic", rec.panicHandler)
		engine.Group("").GET("/recovery/ok", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "ok")
		})

		first := r.serveOn(t, engine, httptest.NewRequest(http.MethodGet, "http://example.com/recovery/panic", nil))
		rec.requireCaught(t)
		requireRecoveredResponse(t, first)

		second := r.serveOn(t, engine, httptest.NewRequest(http.MethodGet, "http://example.com/recovery/ok", nil))
		if second.Status != http.StatusOK || second.Body != "ok" {
			t.Fatalf("the request after a recovered panic got %d %q, want 200 %q", second.Status, second.Body, "ok")
		}
		if rec.handlerRan != 1 || len(rec.panics) != 1 {
			t.Fatalf("handlerRan=%d panics=%d after the second request, want 1 and 1: the panic ran again", rec.handlerRan, len(rec.panics))
		}
	})
}
