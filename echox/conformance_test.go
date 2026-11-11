package echox

import (
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/labstack/echo/v4"
)

// TestConformance holds echox to the shared contract in httpxtest.
func TestConformance(t *testing.T) {
	httpxtest.Run(t, httpxtest.Suite{
		Name: "echox",
		Caps: httpxtest.Caps{
			Flusher:                    true,
			InProcessUnknownLengthBody: true,
			ForcedStopCutsConnections:  true,
		},
		NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
			engineOpts := []Option{WithEngine(echo.New())}
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
			native := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					mark("native-pre")
					err := next(c)
					mark("native-post")
					return err
				}
			}
			switch s := scope.(type) {
			case *Engine:
				s.UseNative(native)
			case *Router:
				s.UseNative(native)
			default:
				panic("echox conformance: unexpected scope")
			}
		},
	})
}
