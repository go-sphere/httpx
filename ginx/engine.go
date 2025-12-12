package ginx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Option = httpx.Option[*gin.Engine]
type Engine struct {
	engine       *gin.Engine
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = gin.Default()
	}
	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)
	return &Engine{
		engine:       conf.Engine,
		middleware:   middleware,
		errorHandler: conf.ErrorHandler,
	}
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.middleware.Use(middleware...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	middleware := e.middleware.Clone()
	middleware.Use(m...)
	return &Router{
		group:        e.engine.Group(prefix),
		middleware:   middleware,
		errorHandler: e.errorHandler,
	}
}
func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	e.engine.ServeHTTP(w, req)
}
