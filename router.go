package httpx

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
)

// Handler serves one request through ctx. Returning a non-nil error asks the
// adapter to render it with the configured error handler; an error returned
// after the response was committed does not change the response, but still
// reaches the middleware layers above the handler as next's return value.
type Handler func(Context) error

// Registrar registers handlers on a router scope.
//
// Method names are case-insensitive: adapters upper-case them before
// registration. The portable method set is the nine standard HTTP methods
// (GET, HEAD, POST, PUT, PATCH, DELETE, CONNECT, OPTIONS, TRACE); passing a
// non-standard method (e.g. PROPFIND) is framework-dependent and may panic
// at registration time. Which methods Any matches beyond the standard set is
// also framework-dependent.
//
// The portable path grammar is three shapes and nothing else: a static
// segment, a ":name" parameter filling exactly one segment, and a single
// "*name" wildcard as the final segment. stdx is the reference implementation
// — what its router accepts is what httpx promises. The wildcard rule is
// enforced by ValidateWildcardPath, which every adapter calls from every
// registration entry point, so all five panic identically, with that error
// rather than a framework's, on any other wildcard shape.
//
// Any other path syntax is unspecified: no restriction, and no promise. The
// case that comes up is a literal colon inside a segment
// ("/v1/reports:generate", the custom-method form google.api.http uses), and
// the five adapters disagree about all of it — whether the colon introduces a
// parameter or is matched literally, whether two such routes sharing a prefix
// register or panic, and which of them a request reaches when they do. httpx
// neither rejects these paths nor makes them agree. A path outside the grammar
// may panic at registration, match requests it should not, or silently collide
// with a sibling route; no conformance case covers it, and none will.
type Registrar interface {
	// Handle registers h for method and path, joined onto this scope's prefix.
	// Middleware registered on this scope and its parents so far wraps h; see
	// MiddlewareChain. An unsupported wildcard shape panics with the error
	// from ValidateWildcardPath.
	Handle(method, path string, h Handler)

	// Any registers h for path under every standard HTTP method.
	Any(path string, h Handler)

	// Static serves the files under the root directory at prefix; it is
	// StaticFS with os.DirFS(root).
	Static(prefix, root string)

	// StaticFS serves fs at prefix through StaticFileHandler, registered as
	// GET and HEAD routes, so middleware on this scope wraps static requests.
	// Directory listings are never served: a directory without index.html
	// answers 404.
	StaticFS(prefix string, fs fs.FS)
}

// StdHandlerMounter is an optional Registrar capability for mounting plain
// net/http handlers (e.g. httputil.ReverseProxy, pprof, http.ServeMux) on a
// route without leaving the httpx abstraction.
type StdHandlerMounter interface {
	// HandleStd registers h for the given method and path. The path uses the
	// same syntax as Registrar.Handle for the underlying adapter.
	HandleStd(method, path string, h http.Handler)
}

// MountStd mounts h on r when the Registrar supports StdHandlerMounter and
// reports whether the handler was mounted.
func MountStd(r Registrar, method, path string, h http.Handler) bool {
	if m, ok := r.(StdHandlerMounter); ok {
		m.HandleStd(method, path, h)
		return true
	}
	return false
}

// RouterFeature identifies an optional router capability.
type RouterFeature string

const (
	// RouterFeatureNamedWildcard indicates support for named wildcard params in paths,
	// for example, /files/*filepath
	RouterFeatureNamedWildcard RouterFeature = "named_wildcard"
)

// RouterFeatureProvider exposes optional router capability detection.
type RouterFeatureProvider interface {
	// SupportsRouterFeature reports whether the underlying router natively
	// supports feature. A false answer does not make the feature unusable
	// through httpx: adapters emulate named wildcards, for example.
	SupportsRouterFeature(feature RouterFeature) bool
}

// Router is a route scope: a path prefix plus the middleware registered on it
// and its parents. Obtain the first one from Engine.Group and nest further
// scopes with Group. Register routes and middleware before the engine starts
// serving.
type Router interface {
	Registrar
	MiddlewareScope
	RouterFeatureProvider

	// BasePath returns this scope's absolute path prefix: the group prefixes
	// joined, keeping a trailing slash the caller wrote.
	BasePath() string

	// Group returns a nested scope under prefix, with m registered on it as if
	// by Use.
	Group(prefix string, m ...Middleware) Router

	// GET, POST, PUT, DELETE, PATCH, HEAD and OPTIONS are shorthands for
	// Handle with the corresponding method.
	GET(path string, h Handler)
	POST(path string, h Handler)
	PUT(path string, h Handler)
	DELETE(path string, h Handler)
	PATCH(path string, h Handler)
	HEAD(path string, h Handler)
	OPTIONS(path string, h Handler)
}

// ErrEngineClosed is returned by Engine.Start after Stop has been called.
// Engines are single-use: once stopped (even before ever starting), they
// cannot serve again — construct a new Engine instead. This matches
// net/http.Server semantics and prevents the silent "fake start" some
// frameworks exhibit after shutdown.
var ErrEngineClosed = errors.New("httpx: engine closed (engines are single-use; create a new one to serve again)")

// Engine is the entrypoint: it can serve HTTP, apply global middleware,
// and create groups, but cannot register routes directly.
//
// Lifecycle contract: Start blocks while serving and returns nil once Stop
// ended it — never the framework's own "server closed" error. Stop on an
// engine that never started returns nil. Engines are single-use — calling
// Start after Stop (in any order, including Stop before the first Start)
// returns ErrEngineClosed.
// IsRunning is best-effort: on net/http based adapters it becomes true only
// after the listener is bound; on fiber/hertz it may become true slightly
// before binding completes.
//
// Middleware registered on the Engine — and only on the Engine — also covers
// the paths no route matched, so an access log or a recovery layer sees a 404.
// See Middleware.
type Engine interface {
	MiddlewareScope

	// Group returns the Router for prefix ("" or "/" for the root), with m
	// registered on it as if by Use.
	Group(prefix string, m ...Middleware) Router

	// Start listens on the configured address and serves until Stop. It
	// returns nil once Stop ended it, ErrEngineClosed when Stop was already
	// called, and the listener or server error otherwise (for example, an
	// address already in use).
	Start() error

	// Stop shuts the engine down gracefully, waiting for in-flight requests
	// until ctx is done, and marks the engine closed even if it never started,
	// in which case it returns nil. When ctx expires first the listener is
	// still closed; whether connections still being served are cut is
	// adapter-specific. A nil error does not by itself mean every in-flight
	// request finished.
	Stop(ctx context.Context) error

	// IsRunning reports whether Start is currently serving.
	IsRunning() bool
}

// TestRequester is an optional Engine capability that serves a request
// in-process without opening a network listener, for use in tests.
type TestRequester interface {
	// Do dispatches req through the engine's router and returns the
	// response. The response body is fully buffered.
	Do(req *http.Request) (*http.Response, error)
}

// AsTestRequester returns the in-process test capability when supported.
// Every official adapter's Engine supports it.
//
//	if tr, ok := httpx.AsTestRequester(engine); ok {
//		resp, err := tr.Do(httptest.NewRequest(http.MethodGet, "/api/ping", nil))
//		// ...
//	}
func AsTestRequester(e Engine) (TestRequester, bool) {
	tr, ok := e.(TestRequester)
	return tr, ok
}

// The success-envelope wrapper deliberately does not live here: what a
// handler's return value looks like on the wire is a convention, not part of
// the framework-agnostic transport contract. It belongs in
// sphere/server/httpz.WithJson, where one definition can evolve without two
// packages disagreeing about what "success" serializes to.
