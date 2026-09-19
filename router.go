package httpx

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
)

// Handler is the canonical function signature for framework adapters.
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
// Wildcard paths must satisfy ValidateWildcardPath (a single named wildcard
// as the final path segment); adapters panic at registration for other
// shapes, matching gin/hertz native behavior.
type Registrar interface {
	Handle(method, path string, h Handler)
	Any(path string, h Handler)
	Static(prefix, root string)
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
	SupportsRouterFeature(feature RouterFeature) bool
}

// Router is a full-featured route scope.
type Router interface {
	Registrar
	MiddlewareScope
	RouterFeatureProvider

	BasePath() string

	// Group returns a nested scope under prefix, with m registered on it as if
	// by Use.
	Group(prefix string, m ...Middleware) Router

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
// Lifecycle contract: Start blocks while serving and returns nil after a
// graceful Stop. Engines are single-use — calling Start after Stop (in any
// order, including Stop before the first Start) returns ErrEngineClosed.
// IsRunning is best-effort: on net/http based adapters it becomes true only
// after the listener is bound; on fiber/hertz it may become true slightly
// before binding completes.
//
// Middleware registered on the Engine — and only on the Engine — also covers
// the paths no route matched, so an access log or a recovery layer sees a 404.
// See Middleware.
type Engine interface {
	MiddlewareScope
	Group(prefix string, m ...Middleware) Router

	Start() error
	Stop(ctx context.Context) error
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
func AsTestRequester(e Engine) (TestRequester, bool) {
	tr, ok := e.(TestRequester)
	return tr, ok
}

// The success-envelope wrapper deliberately does not live here: what a
// handler's return value looks like on the wire is a convention, not part of
// the framework-agnostic transport contract. It belongs in
// sphere/server/httpz.WithJson, where one definition can evolve without two
// packages disagreeing about what "success" serializes to.
