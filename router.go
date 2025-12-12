package httpx

import "net/http"

// Handler is the canonical function signature for framework adapters.
type Handler func(Context) error

// ErrorHandler receives the terminal error from a Handler.
type ErrorHandler func(Context, error)

// Router is the common routing interface backed by gin/fiber/echo/chi/hertz.
type Router interface {
	Use(...Middleware)
	Group(prefix string, m ...Middleware) Router
	Handle(method, path string, h Handler)
	Any(path string, h Handler)
	Static(prefix, root string)
	Mount(path string, h http.Handler)
}

// Engine defines a lightweight interface for routing HTTP requests with extensible error and fallback handling capabilities.
type Engine interface {
	Router
	http.Handler
	RegisterErrorHandler(ErrorHandler)
}

// Config controls router adapter creation.
type Config[E any] struct {
	ErrorHandler ErrorHandler
	Middleware   MiddlewareChain
	Engine       E // framework-specific passthrough (e.g., *gin.Engine, *fiber.App, *echo.Echo, *chi.Mux)
}

// Option configures a EngineFactory Config.
type Option[E any] func(*Config[E])

// NewConfig builds a Config with the given options.
func NewConfig[E any](opts ...Option[E]) *Config[E] {
	var cfg Config[E]
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &cfg
}

// WithErrorHandler installs a terminal error handler.
func WithErrorHandler[E any](h ErrorHandler) Option[E] {
	return func(cfg *Config[E]) {
		cfg.ErrorHandler = h
	}
}

// WithMiddleware appends global middleware.
func WithMiddleware[E any](m ...Middleware) Option[E] {
	return func(cfg *Config[E]) {
		cfg.Middleware.Use(m...)
	}
}

// WithEngine passes a framework-native engine into the factory.
func WithEngine[E any](engine E) Option[E] {
	return func(cfg *Config[E]) {
		cfg.Engine = engine
	}
}

// EngineFactory constructs a Router for the chosen framework.
type EngineFactory[E any] func(opts ...Option[E]) (Engine, error)
