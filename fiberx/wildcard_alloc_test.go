//go:build !race

package fiberx

import (
	"net/http"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// A named wildcard costs one request-local entry on this adapter: fiberContext
// holds a single pointer so it fits in an interface without a heap wrapper, so
// the matched route's wildcard mapping goes into fiber's Locals instead of a
// second field (see markWildcardRoute). That is one allocation per request on
// every route registered with one, and it is the only per-request cost the
// mapping has — which is worth pinning, because the cheap-looking way to add the
// next piece of per-route state is to put it in Locals too.
//
// Not a benchmark: the shared table in conformance/ measures the scenarios, and a
// number that only moves when someone reads a benchmark is not a guard.
//
// Build-constrained to non-race builds for the same reason
// ginx/interceptor_alloc_test.go is: the race detector perturbs allocation
// counts, and a guard that reddens CI at random teaches people to ignore it.

// wildcardAllocRunner drives fiber's own dispatcher with a reusable
// fasthttp.RequestCtx, the way conformance's fiberRunner does. Going through
// app.Test instead would bury the count being asserted under ~24 allocations of
// request/response conversion, so a regression of one would not be visible.
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
		// Both readings resolve through the marked route, so the wildcard path is
		// exercised end to end and not just registered.
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
		// The baseline the wildcard cost is measured against.
		{"plain route", "/plain", "/plain", quiet, 1},
		// One more than the baseline: the Locals entry markWildcardRoute writes.
		{"named wildcard route", "/bare/*filepath", "/bare/a/b.txt", quiet, 2},
		// Reading the wildcard back costs two more, for the parameter value and
		// the registered pattern.
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
