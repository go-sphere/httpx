package httpxtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("RequestID", casesRequestID)
}

// The RequestID cases pin the shape of the correlation middleware every service
// grows: take the caller's ID or mint one, hand it to the layers below through
// both channels — the state store for the httpx chain, the standard context for
// everything that only receives a context.Context — and echo it back on the
// response, including on responses the service never handled itself, so a
// rejected request can still be correlated with its log line.
//
// The suite's own middleware is written out here rather than imported from a
// package: httpxtest is part of the root module, which depends on nothing
// outside the standard library.
const (
	requestIDHeader   = "X-Request-ID"
	requestIDStoreKey = "request.id"
)

// requestIDCtxKey is the standard-context key, distinct from the store's string
// key so the two channels cannot be confused for each other in an assertion.
type requestIDCtxKey struct{}

type requestIDRecorder struct {
	// generated counts what the middleware minted. The values are deterministic
	// here so a golden can pin them; the contract under test is that the ID the
	// response carries is the one the layers below saw, not how it was produced.
	generated  int
	issued     string
	storeSeen  string
	ctxSeen    string
	handlerRan int
}

func (rec *requestIDRecorder) middleware(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		id := ctx.Header(requestIDHeader)
		if id == "" {
			rec.generated++
			id = fmt.Sprintf("generated-%d", rec.generated)
		}
		rec.issued = id
		ctx.Set(requestIDStoreKey, id)
		ctx.SetContext(context.WithValue(ctx.Context(), requestIDCtxKey{}, id))
		ctx.SetHeader(requestIDHeader, id)
		return next(ctx)
	}
}

func (rec *requestIDRecorder) handler(ctx httpx.Context) error {
	rec.handlerRan++
	stored, _ := ctx.Get(requestIDStoreKey)
	rec.storeSeen, _ = stored.(string)
	rec.ctxSeen, _ = ctx.Context().Value(requestIDCtxKey{}).(string)
	return ctx.JSON(http.StatusOK, map[string]string{
		"store":   rec.storeSeen,
		"context": rec.ctxSeen,
	})
}

// requireIssuedEverywhere asserts the ID the middleware issued reached the
// handler through both channels and came back on the response header.
func (rec *requestIDRecorder) requireIssuedEverywhere(t *testing.T) {
	t.Helper()
	if rec.issued == "" {
		t.Fatal("the middleware issued no ID")
	}
	if rec.storeSeen != rec.issued {
		t.Fatalf("the handler read %q from the state store, want the issued %q", rec.storeSeen, rec.issued)
	}
	if rec.ctxSeen != rec.issued {
		t.Fatalf("the handler read %q from the standard context, want the issued %q: a value only reachable through the state store cannot be handed to downstream calls",
			rec.ctxSeen, rec.issued)
	}
}

func casesRequestID(t *testing.T, r runner) {
	t.Run("SuppliedIDIsKept", func(t *testing.T) {
		rec := &requestIDRecorder{}
		req := httptest.NewRequest(http.MethodGet, "http://example.com/request-id/supplied", nil)
		req.Header.Set(requestIDHeader, "client-id-42")
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.middleware)
			router.GET("/request-id/supplied", rec.handler)
		}, req)

		rec.requireIssuedEverywhere(t)
		if rec.issued != "client-id-42" {
			t.Fatalf("the middleware issued %q, want the caller's %q: a supplied ID must not be replaced", rec.issued, "client-id-42")
		}
		if rec.generated != 0 {
			t.Fatalf("the middleware generated %d IDs for a request that carried one", rec.generated)
		}
		if got.Headers.Get(requestIDHeader) != "client-id-42" {
			t.Fatalf("response %s = %q, want %q", requestIDHeader, got.Headers.Get(requestIDHeader), "client-id-42")
		}
		r.compareGolden(t, got)
	})

	t.Run("MissingIDIsGenerated", func(t *testing.T) {
		rec := &requestIDRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.middleware)
			router.GET("/request-id/generated", rec.handler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/request-id/generated", nil))

		rec.requireIssuedEverywhere(t)
		if rec.generated != 1 {
			t.Fatalf("the middleware generated %d IDs, want 1", rec.generated)
		}
		if got.Headers.Get(requestIDHeader) != rec.issued {
			t.Fatalf("response %s = %q, want the issued %q", requestIDHeader, got.Headers.Get(requestIDHeader), rec.issued)
		}
		r.compareGolden(t, got)
	})

	// The ID must label the response even when the request never reaches a
	// handler: a rejected request whose response carries no ID cannot be
	// correlated with the log line that explains it.
	t.Run("IDIsOnErrorResponses", func(t *testing.T) {
		rec := &requestIDRecorder{}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(rec.middleware, denyLayer("request-id-denied"))
			router.GET("/request-id/denied", rec.handler)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/request-id/denied", nil))

		if rec.handlerRan != 0 {
			t.Fatalf("the handler ran %d times, want 0", rec.handlerRan)
		}
		if got.Status != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusForbidden, got.Body)
		}
		if got.Headers.Get(requestIDHeader) != rec.issued {
			t.Fatalf("response %s = %q, want the issued %q: the layer above the failure set it before the error was rendered",
				requestIDHeader, got.Headers.Get(requestIDHeader), rec.issued)
		}
		r.compareGolden(t, got)
	})
}
