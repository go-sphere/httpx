package httpx

import (
	"context"
	"io/fs"
)

// Handler is the canonical function signature for framework adapters.
type Handler func(Context)

// Middleware shares the same signature as Handler and drives the chain via ctx.Next().
type Middleware func(Context)

// MiddlewareScope attaches middleware to the current scope.
type MiddlewareScope interface {
	Use(...Middleware)
}

// Registrar registers handlers on a router scope.
type Registrar interface {
	Handle(method, path string, h Handler)
	Any(path string, h Handler)
	Static(prefix, root string)
	StaticFS(prefix string, fs fs.FS)
}

// Router is a full-featured route scope.
type Router interface {
	MiddlewareScope
	Registrar
	BasePath() string
	Group(prefix string, m ...Middleware) Router
}

// Engine is the entrypoint: it can serve HTTP, apply global middleware,
// and create groups, but cannot register routes directly.
type Engine interface {
	MiddlewareScope
	Start() error
	Stop(ctx context.Context) error
	Group(prefix string, m ...Middleware) Router
}
