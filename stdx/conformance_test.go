package stdx

import (
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

// TestConformance holds stdx to the shared contract in httpxtest.
func TestConformance(t *testing.T) {
	httpxtest.Run(t, httpxtest.Suite{
		Name: "stdx",
		Caps: httpxtest.Caps{
			NamedWildcard:              true,
			Flusher:                    true,
			InProcessUnknownLengthBody: true,
			ForcedStopCutsConnections:  true,
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
	})
}
