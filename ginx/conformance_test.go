package ginx

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

// TestConformance holds ginx to the shared contract in httpxtest.
func TestConformance(t *testing.T) {
	httpxtest.Run(t, httpxtest.Suite{
		Name: "ginx",
		Caps: httpxtest.Caps{
			NamedWildcard:              true,
			Flusher:                    true,
			InProcessUnknownLengthBody: true,
			ForcedStopCutsConnections:  true,
		},
		NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
			gin.SetMode(gin.ReleaseMode)
			engineOpts := []Option{WithEngine(gin.New())}
			if opts.ErrorHandler != nil {
				engineOpts = append(engineOpts, WithErrorHandler(opts.ErrorHandler))
			}
			if opts.MaxBodySize > 0 {
				engineOpts = append(engineOpts, WithMaxBodySize(opts.MaxBodySize))
			}
			return New(engineOpts...)
		},
		StdMiddleware: AdaptStdMiddleware,
		NativeMiddleware: func(scope any, mark func(string)) {
			native := func(c *gin.Context) {
				mark("native-pre")
				c.Next()
				mark("native-post")
			}
			switch s := scope.(type) {
			case *Engine:
				s.UseNative(native)
			case *Router:
				s.UseNative(native)
			default:
				panic("ginx conformance: unexpected scope")
			}
		},
	})
}
