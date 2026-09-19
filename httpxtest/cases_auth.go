package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Auth", casesAuth)
}

// The auth cases pin what an authorization layer is for: a request that does
// not carry valid credentials never reaches the handler the layer guards,
// whatever scope the layer was registered on. The protected handler writes a
// canary body, so an adapter that drops or reorders the layer fails the golden
// contract as well as the inline count of how often the handler ran.
//
// The layers are written out here rather than imported from sphere's auth
// package: httpxtest is part of the root module, which has no dependencies
// outside the standard library and must stay importable by a third-party
// adapter. `auth.NewAuthMiddleware` and `auth.NewPermissionMiddleware` are the
// production versions of the same shape.
const (
	authRoleKey = "auth.role"
	adminToken  = "admin-token"
	viewerToken = "viewer-token"
)

// authRecorder counts what ran, so every case can assert that a denied request
// reached the auth layer exactly once and the protected handler not at all.
type authRecorder struct {
	authRan    int
	handlerRan int
	publicRan  int
	roleSeen   string
}

// authenticate resolves a bearer token into a role and stores it on the
// request. An absent, malformed or unknown token stops the chain with 401, so
// nothing below it runs.
func (a *authRecorder) authenticate(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		a.authRan++
		token, ok := strings.CutPrefix(ctx.Header("Authorization"), "Bearer ")
		if !ok {
			return httpx.NewUnauthorizedError("authentication required")
		}
		var role string
		switch token {
		case adminToken:
			role = "admin"
		case viewerToken:
			role = "viewer"
		default:
			return httpx.NewUnauthorizedError("authentication required")
		}
		ctx.Set(authRoleKey, role)
		return next(ctx)
	}
}

// requireRole is the second half of authorization: 401 means "I do not know
// you", 403 means "I know you and you may not". A layer that conflated them
// would still stop the request, but report the wrong thing.
func requireRole(role string) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			if got, _ := ctx.Get(authRoleKey); got != role {
				return httpx.NewForbiddenError("permission denied")
			}
			return next(ctx)
		}
	}
}

func (a *authRecorder) adminHandler(ctx httpx.Context) error {
	a.handlerRan++
	role, _ := ctx.Get(authRoleKey)
	a.roleSeen, _ = role.(string)
	return ctx.JSON(http.StatusOK, map[string]any{"handler": "admin", "role": role})
}

func (a *authRecorder) publicHandler(ctx httpx.Context) error {
	a.publicRan++
	return ctx.Text(http.StatusOK, "public")
}

// guardedRoutes registers the protected group every case probes plus a public
// group that must stay reachable without credentials: an auth layer that leaked
// onto its siblings would pass every denial case and still break this one.
func guardedRoutes(a *authRecorder) func(httpx.Router) {
	return func(router httpx.Router) {
		admin := router.Group("/admin", a.authenticate, requireRole("admin"))
		admin.GET("/secret", a.adminHandler)

		public := router.Group("/public")
		public.GET("/ping", a.publicHandler)
	}
}

// probe serves the protected route with the given Authorization header and
// asserts the response contract plus the counts: the guard ran, and the handler
// ran only when the request was allowed through.
func (r runner) probe(t *testing.T, rec *authRecorder, header string, wantStatus int) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/admin/secret", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	got := r.serve(t, guardedRoutes(rec), req)

	if got.Status != wantStatus {
		t.Fatalf("status = %d, want %d; body=%q", got.Status, wantStatus, got.Body)
	}
	if rec.authRan != 1 {
		t.Fatalf("the auth layer ran %d times, want 1: a request reached the route without passing the guard", rec.authRan)
	}
	wantHandlerRan := 0
	if wantStatus == http.StatusOK {
		wantHandlerRan = 1
	}
	if rec.handlerRan != wantHandlerRan {
		t.Fatalf("the protected handler ran %d times for a %d response, want %d: the layer decides whether the handler runs",
			rec.handlerRan, wantStatus, wantHandlerRan)
	}
	r.compareGolden(t, got)
	return got
}

func casesAuth(t *testing.T, r runner) {
	t.Run("MissingToken", func(t *testing.T) {
		r.probe(t, &authRecorder{}, "", http.StatusUnauthorized)
	})

	// A request that presents credentials in the wrong scheme must not fall
	// through to a prefix match that accepts anything.
	t.Run("MalformedScheme", func(t *testing.T) {
		r.probe(t, &authRecorder{}, "Basic "+adminToken, http.StatusUnauthorized)
	})

	t.Run("UnknownToken", func(t *testing.T) {
		r.probe(t, &authRecorder{}, "Bearer not-a-token", http.StatusUnauthorized)
	})

	// Authenticated but not authorized: the role carried by the token is below
	// what the route requires, and the handler still must not run.
	t.Run("InsufficientRole", func(t *testing.T) {
		r.probe(t, &authRecorder{}, "Bearer "+viewerToken, http.StatusForbidden)
	})

	t.Run("ValidTokenReachesHandler", func(t *testing.T) {
		rec := &authRecorder{}
		r.probe(t, rec, "Bearer "+adminToken, http.StatusOK)
		if rec.roleSeen != "admin" {
			t.Fatalf("the handler saw role %q, want %q: the auth layer's principal did not reach it", rec.roleSeen, "admin")
		}
	})

	t.Run("PublicRouteStaysPublic", func(t *testing.T) {
		rec := &authRecorder{}
		got := r.serve(t, guardedRoutes(rec), httptest.NewRequest(http.MethodGet, "http://example.com/public/ping", nil))
		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		if rec.publicRan != 1 {
			t.Fatalf("the public handler ran %d times, want 1", rec.publicRan)
		}
		if rec.authRan != 0 {
			t.Fatalf("the auth layer ran %d times for a route outside its group: a group's layers must not cover a sibling group", rec.authRan)
		}
		r.compareGolden(t, got)
	})
}
