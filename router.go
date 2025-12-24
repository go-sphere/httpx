package httpx

import (
	"context"
	"io/fs"
)

type H map[string]any

// Handler is the canonical function signature for framework adapters.
type Handler func(Context) error

// Middleware shares the same signature as Handler and drives the chain via ctx.Next().
type Middleware func(Context) error

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

	// HTTP method shortcuts for ergonomic API

	GET(path string, h Handler)
	POST(path string, h Handler)
	PUT(path string, h Handler)
	DELETE(path string, h Handler)
	PATCH(path string, h Handler)
	HEAD(path string, h Handler)
	OPTIONS(path string, h Handler)
}

// Engine is the entrypoint: it can serve HTTP, apply global middleware,
// and create groups, but cannot register routes directly.
type Engine interface {
	MiddlewareScope
	Group(prefix string, m ...Middleware) Router

	// Enhanced lifecycle management

	Start() error
	Stop(ctx context.Context) error
	IsRunning() bool // Server status check
}

// WithJson wraps a handler with JSON response.
func WithJson[T any](handler func(ctx Context) (T, error)) Handler {
	return func(ctx Context) error {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					_ = ctx.JSON(500, H{"error": e.Error()})
				} else {
					_ = ctx.JSON(500, H{"error": "internal server error"})
				}
			}
		}()
		data, err := handler(ctx)
		if err != nil {
			return err
		}
		return ctx.JSON(200, H{
			"success": true,
			"data":    data,
		})
	}
}
