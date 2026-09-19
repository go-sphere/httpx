package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("MiddlewareScope", casesMiddlewareScope)
}

// Which requests a middleware chain covers, and which scope's chain covers
// them. Every registered route is covered by its scope's chain and every parent
// scope's, mounts included; a path no route matched is covered by the **engine**
// scope only, so an access log, panic recovery or CORS layer sees a 404; and a
// **group's** chain never covers an unmatched path — a 404 belongs to no group,
// and picking one by prefix would make the answer depend on how the route table
// happens to be split up.
func casesMiddlewareScope(t *testing.T, r runner) {
	// One route makes the three request kinds distinguishable: /known matches,
	// /nope matches nothing, /known under another method is a 405.
	registerKnown := func(router httpx.Router) {
		router.POST("/known", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "known")
		})
	}

	// 404 and 405 are separate fallbacks inside every adapter, so both are asked.
	unmatched := []struct {
		name, method, target string
		wantStatus           int
	}{
		{"NotFound", http.MethodGet, "/nope", http.StatusNotFound},
		{"MethodNotAllowed", http.MethodGet, "/known", http.StatusMethodNotAllowed},
	}

	t.Run("EngineCoversUnmatchedPath", func(t *testing.T) {
		for _, tc := range unmatched {
			t.Run(tc.name, func(t *testing.T) {
				var ran int
				got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
					engine.Use(func(next httpx.Handler) httpx.Handler {
						return func(ctx httpx.Context) error {
							ran++
							return next(ctx)
						}
					})
					registerKnown(engine.Group(""))
				}, httptest.NewRequest(tc.method, "http://example.com"+tc.target, nil))

				if got.Status != tc.wantStatus {
					t.Fatalf("status = %d, want %d; body=%q", got.Status, tc.wantStatus, got.Body)
				}
				if ran != 1 {
					t.Fatalf("the engine layer ran %d times for %s, want exactly 1: an engine-scope chain must cover a path no route matched",
						ran, tc.target)
				}
			})
		}
	})

	t.Run("GroupDoesNotCoverUnmatchedPath", func(t *testing.T) {
		for _, tc := range []struct{ name, target string }{
			// Under the group's own prefix is the case that would tempt an
			// implementation into picking a scope by prefix.
			{"UnderTheGroupPrefix", "/api/nope"},
			{"OutsideTheGroupPrefix", "/nope"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var engineRan, groupRan int
				got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
					engine.Use(func(next httpx.Handler) httpx.Handler {
						return func(ctx httpx.Context) error {
							engineRan++
							return next(ctx)
						}
					})
					g := engine.Group("/api")
					g.Use(func(next httpx.Handler) httpx.Handler {
						return func(ctx httpx.Context) error {
							groupRan++
							return next(ctx)
						}
					})
					g.GET("/known", func(ctx httpx.Context) error {
						return ctx.Text(http.StatusOK, "known")
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))

				if got.Status != http.StatusNotFound {
					t.Fatalf("status = %d, want 404; body=%q", got.Status, got.Body)
				}
				if engineRan != 1 {
					t.Fatalf("the engine layer ran %d times, want 1", engineRan)
				}
				if groupRan != 0 {
					t.Fatalf("a group's layer ran for an unmatched path (%d times): a 404 belongs to no group", groupRan)
				}
			})
		}
	})

	// An engine layer that answers instead of continuing owns the response,
	// exactly as it would on a route: the CORS-preflight and SPA-rewrite case,
	// and the reason covering unmatched paths is worth anything.
	t.Run("EngineMiddlewareCanAnswerUnmatchedPath", func(t *testing.T) {
		for _, tc := range unmatched {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
					engine.Use(func(next httpx.Handler) httpx.Handler {
						return func(ctx httpx.Context) error {
							ctx.SetHeader("X-Answered", "1")
							return ctx.Text(http.StatusTeapot, "rewritten")
						}
					})
					registerKnown(engine.Group(""))
				}, httptest.NewRequest(tc.method, "http://example.com"+tc.target, nil))

				if got.Status != http.StatusTeapot || got.Body != "rewritten" {
					t.Fatalf("response = %d %q, want 418 %q: the layer's answer did not win over the fallback",
						got.Status, got.Body, "rewritten")
				}
				if got.Headers.Get("X-Answered") != "1" {
					t.Fatal("the layer's header did not reach the response")
				}
			})
		}
	})

	// A layer that stops the chain without answering is a bug in the layer, but
	// it must not turn a 404 into a 200: the fallback's own status is the floor,
	// the same rule a silent error handler gets. The body is deliberately not
	// asserted — gin and hertz write their framework's plain-text default here
	// and the other three leave it empty.
	t.Run("SwallowedUnmatchedPathKeepsItsStatus", func(t *testing.T) {
		for _, tc := range unmatched {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
					engine.Use(func(next httpx.Handler) httpx.Handler {
						return func(ctx httpx.Context) error { return nil }
					})
					registerKnown(engine.Group(""))
				}, httptest.NewRequest(tc.method, "http://example.com"+tc.target, nil))

				if got.Status != tc.wantStatus {
					t.Fatalf("status = %d, want %d: a layer that answered nothing must not leave an unmatched path at 200; body=%q",
						got.Status, tc.wantStatus, got.Body)
				}
			})
		}
	})

	// A group holds a reference to its parent's chain, not a copy: a layer
	// registered on the engine after the group was created still reaches the
	// routes that group registers afterwards. A copy would make it reach nothing,
	// silently, and the order of two unrelated setup functions load-bearing.
	t.Run("LateEngineRegistrationReachesAnExistingGroup", func(t *testing.T) {
		var order []string
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(mark(&order, "early"))
			g := engine.Group("/api")
			engine.Use(mark(&order, "late"))
			g.GET("/x", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/x", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		if joined := strings.Join(order, ","); joined != "early,late" {
			t.Fatalf("order = %q, want %q: an engine-scope layer registered after the group did not reach its routes",
				joined, "early,late")
		}
	})

	// The half that is deliberately kept: a route already registered stays on the
	// chain it was registered with, which is what makes a registered route
	// immutable — the rule every framework applies to its own Use.
	t.Run("RouteKeepsTheChainItWasRegisteredWith", func(t *testing.T) {
		var order []string
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(mark(&order, "early"))
			g := engine.Group("/api")
			g.GET("/x", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
			engine.Use(mark(&order, "after-the-route"))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/x", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		if joined := strings.Join(order, ","); joined != "early" {
			t.Fatalf("order = %q, want %q: a layer registered after the route reached it", joined, "early")
		}
	})

	// Group's variadic takes middleware: the one-line form of Group(prefix)
	// followed by Use.
	t.Run("GroupVariadicRegistersMiddleware", func(t *testing.T) {
		var order []string
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			g := engine.Group("/api", mark(&order, "from-engine-group"))
			nested := g.Group("/v1", mark(&order, "from-router-group"))
			nested.GET("/x", func(ctx httpx.Context) error {
				order = append(order, "handler")
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/x", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		want := "from-engine-group,from-router-group,handler"
		if joined := strings.Join(order, ","); joined != want {
			t.Fatalf("order = %q, want %q", joined, want)
		}
	})

	// Both interfaces include MiddlewareScope, so a caller can register on either
	// without knowing which it holds. Asserted separately because an adapter
	// could satisfy Router through an embedded type that is not a scope itself.
	t.Run("RouterAndEngineAreMiddlewareScopes", func(t *testing.T) {
		engine := r.suite.NewEngine(t, Options{})
		var _ httpx.MiddlewareScope = engine
		var _ httpx.MiddlewareScope = engine.Group("")
	})
}

// mark returns a middleware that records name when it runs.
func mark(order *[]string, name string) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			*order = append(*order, name)
			return next(ctx)
		}
	}
}
