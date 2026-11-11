package hertzx

import (
	"context"
	"io"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

// TestConformance holds hertzx to the shared contract in httpxtest.
func TestConformance(t *testing.T) {
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	httpxtest.Run(t, httpxtest.Suite{
		Name: "hertzx",
		Caps: httpxtest.Caps{
			NamedWildcard:              true,
			Flusher:                    true,
			InProcessUnknownLengthBody: true,
		},
		NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
			var engineOpts []Option
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
			native := func(ctx context.Context, rc *app.RequestContext) {
				mark("native-pre")
				rc.Next(ctx)
				mark("native-post")
			}
			switch s := scope.(type) {
			case *Engine:
				s.UseNative(native)
			case *Router:
				s.UseNative(native)
			default:
				panic("hertzx conformance: unexpected scope")
			}
		},
	})
}
