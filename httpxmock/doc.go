// Package httpxmock provides a complete in-memory httpx.Context for unit
// tests of middleware and handlers.
//
// It exists because of what test authors reach for otherwise: a struct that
// embeds httpx.Context and overrides the two or three methods the test needs.
// The embedded nil interface fills out the method set so the type compiles,
// and every method the code under test touches but the test did not
// anticipate panics with a nil dereference. Seventeen such fakes had
// accumulated across the sphere repository, each a slightly different subset
// of the same surface, several of them duplicated a second time with
// sync/atomic fields so a stress test could share one. This package is the
// one implementation they collapse into.
//
// The rule that makes it worth having is that nothing here is left unfilled:
// every method of httpx.Context has a real body, so a method the test never
// thought about returns a sane zero value instead of taking the test process
// down. That is the entire point — a mock whose unexercised corners panic is
// only marginally better than no mock at all, because it turns "the code now
// reads one more header" into a crash rather than a passing test.
//
// # What it is not
//
// This is a unit-test double, not an adapter. It is not in the conformance
// suite (httpxtest), which exists for adapter authors and drives real
// engines; a downstream consumer testing its own middleware should not have
// to depend on that machinery. It implements no router, so FullPath and Param
// report whatever the test configured and nothing else, and it deliberately
// does not implement httpx.NativeContextProvider — there is no native context
// to hand out, and httpx.AsNativeContext correctly reports false. The Binder
// methods all return [ErrBindUnsupported]; see it for why.
//
// Anything that depends on real routing or real decoding — what a router
// reports as FullPath for a wildcard route, what go-playground/form does to a
// query string — must be tested against a real engine. stdx is the cheapest
// one: it needs no framework and its Engine is itself an http.Handler.
//
// # Usage
//
// Build a Context with [NewRequest] (or [New] around an existing request),
// drive the layer under test with [Run] and a recording [Handler], then read
// the response back off the same Context. Nothing needs closing.
//
//	import (
//		"net/http"
//		"testing"
//
//		"github.com/go-sphere/httpx/httpxmock"
//	)
//
//	func TestAuth(t *testing.T) {
//		ctx := httpxmock.NewRequest(http.MethodGet, "/users/42", nil,
//			httpxmock.WithFullPath("/users/:id"),
//			httpxmock.WithParam("id", "42"),
//		)
//		next := &httpxmock.Handler{}
//		if err := httpxmock.Run(ctx, next.Handle, auth); err != nil {
//			t.Fatal(err)
//		}
//		if !next.Called() {
//			t.Fatalf("the layer stopped the chain: %d %s", ctx.StatusCode(), ctx.BodyString())
//		}
//	}
//
// Run and Handler are the second half of the package: a middleware cannot be
// driven by a Context alone, it needs something at the bottom of the chain,
// and "did the request get past this layer" is the first thing every such
// test asks. See Run for the four downstream copies of those three lines.
//
// The Context is both the thing under test and the recorder: response state
// is read back off the same object that was passed in. A separate recorder
// in the spirit of httptest.ResponseRecorder was considered and rejected,
// because a middleware only ever receives the Context — anything it can
// observe about the response, it observes through ResponseInfo on that
// value — so splitting the two would mean handing tests a second object that
// exists only to be read. Every one of the fakes this replaces already read
// its response state off the fake itself.
//
// # Concurrency
//
// A Context is safe for concurrent use. That is deliberately stricter than
// the real adapters, whose contexts are single-goroutine and may be pooled,
// and it is not a statement about them: it is here so that a stress test can
// share one Context without the test author writing a second, atomic copy of
// the type, which is exactly what happened three times downstream. Code that
// passes a real httpx.Context between goroutines is still wrong.
package httpxmock
