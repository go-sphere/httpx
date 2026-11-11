package fiberx

import (
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/gofiber/fiber/v3"
)

// TestConformance holds fiberx to the shared contract in httpxtest, once with
// the app the adapter builds and once with a caller-built fiber.App:
// fiber.Config is immutable after fiber.New, so that is the one configuration
// the adapter cannot fix up at construction time (UnescapePath in particular).
func TestConformance(t *testing.T) {
	suite := httpxtest.Suite{
		Name: "fiberx",
		NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
			return New(conformanceOptions(opts)...)
		},
		StdMiddleware: AdaptStdMiddleware,
		NativeMiddleware: func(scope any, mark func(string)) {
			native := func(c fiber.Ctx) error {
				mark("native-pre")
				err := c.Next()
				mark("native-post")
				return err
			}
			switch s := scope.(type) {
			case *Engine:
				s.UseNative(native)
			case *Router:
				s.UseNative(native)
			default:
				panic("fiberx conformance: unexpected scope")
			}
		},
	}
	t.Run("adapter-built", func(t *testing.T) { httpxtest.Run(t, suite) })

	own := suite
	own.Name = "fiberx-own-engine"
	own.Caps.RawPathRouting = true
	own.NewEngine = func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
		// Routing is the caller's configuration on such an app; these three
		// are what the route grammar needs (see WithEngine). The rest,
		// UnescapePath included, stays at fiber's defaults.
		app := fiber.New(fiber.Config{
			CaseSensitive:           true,
			StrictRouting:           true,
			DisableHeadAutoRegister: true,
		})
		return New(append([]Option{WithEngine(app)}, conformanceOptions(opts)...)...)
	}
	t.Run("caller-built", func(t *testing.T) { httpxtest.Run(t, own) })
}

func conformanceOptions(opts httpxtest.Options) []Option {
	var out []Option
	if opts.ErrorHandler != nil {
		out = append(out, WithErrorHandler(opts.ErrorHandler))
	}
	if opts.MaxBodySize > 0 {
		out = append(out, WithMaxBodySize(opts.MaxBodySize))
	}
	return out
}
