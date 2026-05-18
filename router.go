package httpx

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
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

// WithJson wraps a handler with a JSON success envelope.
// A panic inside handler is recovered into a 500 Error and returned through
// the normal Handler error path so the engine's configured error handler runs.
func WithJson[T any](handler func(ctx Context) (T, error)) Handler {
	return func(ctx Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = recoverToError(r)
			}
		}()
		var data T
		data, err = handler(ctx)
		if err != nil {
			return err
		}
		return ctx.JSON(200, H{
			"success": true,
			"data":    data,
		})
	}
}

func recoverToError(r any) Error {
	switch v := r.(type) {
	case Error:
		return v
	case error:
		return NewError(http.StatusInternalServerError, http.StatusInternalServerError, "", v)
	case string:
		return NewWithStatus(http.StatusInternalServerError, v)
	default:
		return NewWithStatus(http.StatusInternalServerError, fmt.Sprintf("%v", v))
	}
}
