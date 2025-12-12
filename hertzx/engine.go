package hertzx

import (
	"net/http"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*engine)(nil)

type engine struct {
	*router
}

func New(opts ...httpx.Option[*server.Hertz]) httpx.Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = server.Default()
	}

	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)
	errorHandler := resolveErrorHandler(conf.ErrorHandler)

	return &engine{
		router: &router{
			engine:       conf.Engine,
			group:        conf.Engine.Group("/"),
			middleware:   middleware,
			errorHandler: errorHandler,
		},
	}
}

func (e *engine) RegisterErrorHandler(h httpx.ErrorHandler) {
	e.errorHandler = resolveErrorHandler(h)
}

func resolveErrorHandler(h httpx.ErrorHandler) httpx.ErrorHandler {
	if h != nil {
		return h
	}
	return func(ctx httpx.Context, err error) {
		if err == nil {
			return
		}
		if !ctx.IsAborted() {
			_ = ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
}
