package ginx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*engine)(nil)

type engine struct {
	*router
}

// New constructs a gin-backed Engine using core options.
func New(opts ...httpx.Option[*gin.Engine]) httpx.Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = gin.Default()
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
	handler := resolveErrorHandler(h)
	e.errorHandler = handler
}

func (e *engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	e.engine.ServeHTTP(w, req)
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
