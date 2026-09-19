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
	register("Selector", casesSelector)
}

// The Selector cases pin the routing decision a layer makes from the matched
// route rather than from the request: ctx.FullPath() is the pattern the route
// was registered with — group prefixes included — and a layer can read it
// before calling next. Keying authorization or a rate limit on the pattern,
// rather than on the request path, is what makes the decision follow the route
// the service actually registered.
//
// Two shapes sit beside the ordinary one because they are where such a layer
// goes wrong: a wildcard route is a single pattern however many paths it
// matches, and a path no route matched has no pattern at all, so a selector
// must be able to tell "unlisted route" from "no route".
type patternSelector struct {
	allowed map[string]bool
	seen    []string
	// skipUnmatched lets an unmatched path through. No pattern means the
	// router's fallback owns the request, and a layer that answered 401 there
	// would hide a 404 behind an authorization failure.
	skipUnmatched bool
	handlerRan    int
}

func (sel *patternSelector) middleware(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		pattern := ctx.FullPath()
		sel.seen = append(sel.seen, pattern)
		// Published through X-Trace, the header the golden contracts record, so
		// what the layer saw is compared on every adapter.
		ctx.SetHeader("X-Trace", fmt.Sprintf("full-path=%q", pattern))
		if pattern == "" && sel.skipUnmatched {
			return next(ctx)
		}
		if !sel.allowed[pattern] {
			return httpx.NewUnauthorizedError("route not allowed")
		}
		return next(ctx)
	}
}

func (sel *patternSelector) handler(label string) httpx.Handler {
	return func(ctx httpx.Context) error {
		sel.handlerRan++
		return ctx.JSON(http.StatusOK, map[string]string{"route": label})
	}
}

// requireSeen asserts the patterns the layer observed, in order and before the
// handlers ran.
func (sel *patternSelector) requireSeen(t *testing.T, want string) {
	t.Helper()
	if got := strings.Join(sel.seen, ","); got != want {
		t.Fatalf("the layer saw FullPath %q, want %q", got, want)
	}
}

// selectorRoutes registers one route per allowlist decision: /admin/secret is
// never in an allowlist, /public/open is.
func selectorRoutes(sel *patternSelector) func(httpx.Router) {
	return func(router httpx.Router) {
		router.Use(sel.middleware)
		admin := router.Group("/admin")
		admin.GET("/secret", sel.handler("admin"))
		public := router.Group("/public")
		public.GET("/open", sel.handler("public"))
	}
}

func casesSelector(t *testing.T, r runner) {
	// The layer runs on the engine and the route is three scopes down, so the
	// pattern it reads has to carry the prefixes the groups added.
	t.Run("LayerSeesTheRegisteredPattern", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{"/api/v1/thing": true}}
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(sel.middleware)
			engine.Group("/api").Group("/v1").GET("/thing", sel.handler("thing"))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/thing", nil))

		sel.requireSeen(t, "/api/v1/thing")
		if sel.handlerRan != 1 {
			t.Fatalf("the handler ran %d times, want 1", sel.handlerRan)
		}
		if got.Status != http.StatusOK {
			t.Fatalf("response = %d, want 200 for the allowed pattern; body=%q", got.Status, got.Body)
		}
		// The body is compared as a contract by the golden, which canonicalizes
		// JSON: adapters differ on the encoder's trailing newline.
		r.compareGolden(t, got)
	})

	// A route the allowlist does not name is refused before its handler runs,
	// and the pattern in the observation is the reason it was refused.
	t.Run("PatternAllowlistDenies", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{"/public/open": true}}
		got := r.serve(t, selectorRoutes(sel),
			httptest.NewRequest(http.MethodGet, "http://example.com/admin/secret", nil))

		sel.requireSeen(t, "/admin/secret")
		if sel.handlerRan != 0 {
			t.Fatalf("the protected handler ran %d times, want 0", sel.handlerRan)
		}
		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusUnauthorized, got.Body)
		}
		r.compareGolden(t, got)
	})

	// The same layer and the same registration, one request over: this is what
	// makes the denial above the allowlist's decision rather than the layer
	// refusing everything.
	t.Run("PatternAllowlistAllows", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{"/public/open": true}}
		got := r.serve(t, selectorRoutes(sel),
			httptest.NewRequest(http.MethodGet, "http://example.com/public/open", nil))

		sel.requireSeen(t, "/public/open")
		if sel.handlerRan != 1 {
			t.Fatalf("the handler ran %d times, want 1", sel.handlerRan)
		}
		if got.Status != http.StatusOK {
			t.Fatalf("response = %d, want 200 for the allowed pattern; body=%q", got.Status, got.Body)
		}
		r.compareGolden(t, got)
	})

	// A wildcard route answers for a whole subtree under one registered pattern,
	// so a selector sees the pattern and cannot tell two paths under it apart —
	// which is the property to know before allowlisting one.
	t.Run("WildcardRouteIsOnePattern", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{"/files/*filepath": true}}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(sel.middleware)
			router.GET("/files/*filepath", func(ctx httpx.Context) error {
				sel.handlerRan++
				return ctx.JSON(http.StatusOK, map[string]string{
					"filepath": ctx.Param("filepath"),
					"fullPath": ctx.FullPath(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/files/a/b.txt", nil))

		sel.requireSeen(t, "/files/*filepath")
		if sel.handlerRan != 1 {
			t.Fatalf("the handler ran %d times, want 1", sel.handlerRan)
		}
		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		r.compareGolden(t, got)
	})

	// An unmatched path is covered by the engine's chain but belongs to no
	// route, so FullPath is empty and the layer must let the fallback answer:
	// 404, not 401. Pinned with an allowlist that names nothing, which is the
	// strictest a selector gets.
	t.Run("UnmatchedPathHasNoPattern", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{}, skipUnmatched: true}
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(sel.middleware)
			engine.Group("/api").GET("/known", sel.handler("known"))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/nope", nil))

		sel.requireSeen(t, "")
		if sel.handlerRan != 0 {
			t.Fatalf("a handler ran %d times for an unmatched path", sel.handlerRan)
		}
		if got.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d: the layer must not answer for a path no route matched; body=%q",
				got.Status, http.StatusNotFound, got.Body)
		}
		r.compareGolden(t, got)
	})

	// The same reading for a path a route knows but the method it was asked
	// with does not: no route accepted the request, so there is no pattern for
	// a selector to key on and the 405 stands.
	t.Run("MethodNotAllowedHasNoPattern", func(t *testing.T) {
		sel := &patternSelector{allowed: map[string]bool{}, skipUnmatched: true}
		got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
			engine.Use(sel.middleware)
			engine.Group("/api").POST("/known", sel.handler("known"))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/known", nil))

		sel.requireSeen(t, "")
		if sel.handlerRan != 0 {
			t.Fatalf("a handler ran %d times for a method the route does not accept", sel.handlerRan)
		}
		if got.Status != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusMethodNotAllowed, got.Body)
		}
		r.compareGolden(t, got)
	})
}
