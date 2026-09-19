//go:build !race

package fiberx

import (
	"net/http"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// A named wildcard costs one request-local entry: fiberContext holds a single
// pointer so it fits in an interface without a heap wrapper, so the matched
// route's wildcard mapping goes into fiber's Locals (see markWildcardRoute).
// Not a benchmark — the conformance table measures the scenarios — and !race like
// ginx/middleware_alloc_test.go: the race detector perturbs allocation counts.

// wildcardAllocRunner drives fiber's own dispatcher with a reusable
// fasthttp.RequestCtx; app.Test would bury the asserted count under
// request/response conversion allocations.
func wildcardAllocRunner(t *testing.T, register func(httpx.Router), target string) func() {
	t.Helper()
	app := fiber.New()
	register(New(WithEngine(app)).Group(""))
	handler := app.Handler()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(http.MethodGet)
	ctx.Request.SetRequestURI(target)
	ctx.Request.Header.SetHost("example.com")

	run := func() {
		ctx.Response.Reset()
		// fasthttp recycles the request-local store between requests; without
		// this the Locals entry would accumulate instead of being rewritten.
		ctx.ResetUserValues()
		handler(ctx)
	}
	run()
	if status := ctx.Response.StatusCode(); status != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", status, ctx.Response.Body())
	}
	return run
}

func TestWildcardRouteAllocations(t *testing.T) {
	quiet := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }
	reading := func(ctx httpx.Context) error {
		// Both readings go through the marked route, so the wildcard path is
		// exercised, not just registered.
		if got := ctx.Param("filepath"); got != "a/b.txt" {
			t.Errorf("Param(filepath) = %q", got)
		}
		if got := ctx.FullPath(); got != "/files/*filepath" {
			t.Errorf("FullPath = %q", got)
		}
		return ctx.NoContent(http.StatusNoContent)
	}

	for _, tc := range []struct {
		name    string
		pattern string
		target  string
		handler httpx.Handler
		want    float64
	}{
		{"plain route", "/plain", "/plain", quiet, 1},
		{"named wildcard route", "/bare/*filepath", "/bare/a/b.txt", quiet, 2},
		{"named wildcard route, parameter read", "/files/*filepath", "/files/a/b.txt", reading, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := wildcardAllocRunner(t, func(r httpx.Router) { r.GET(tc.pattern, tc.handler) }, tc.target)
			if allocs := testing.AllocsPerRun(200, run); allocs != tc.want {
				t.Fatalf("%v allocations per request through %s, want %v", allocs, tc.pattern, tc.want)
			}
		})
	}
}
