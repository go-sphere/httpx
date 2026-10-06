// Package httpxtest is the shared conformance suite for httpx adapters.
//
// An adapter provides a [Suite] — a name, a [Caps] declaration, and a function
// that builds an Engine — and calls [Run] from its own module's tests. Each
// case builds a fresh engine, registers its own routes and serves requests
// in-process through httpx.TestRequester, so no listener is opened and nothing
// needs stopping. [RunBenchmarks] measures the shared [Scenarios] table
// against the same Suite.
//
// # Usage
//
//	import (
//		"testing"
//
//		"github.com/go-sphere/httpx"
//		"github.com/go-sphere/httpx/httpxtest"
//		"github.com/go-sphere/httpx/stdx"
//	)
//
//	func TestConformance(t *testing.T) {
//		httpxtest.Run(t, httpxtest.Suite{
//			Name: "stdx",
//			Caps: httpxtest.Caps{NamedWildcard: true, Flusher: true, InProcessUnknownLengthBody: true},
//			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
//				var engineOpts []stdx.Option
//				if opts.ErrorHandler != nil {
//					engineOpts = append(engineOpts, stdx.WithErrorHandler(opts.ErrorHandler))
//				}
//				return stdx.New(engineOpts...)
//			},
//			StdMiddleware: stdx.AdaptStdMiddleware,
//		})
//	}
//
// Response shape is checked against the golden contracts embedded in this
// package, so the suite states what a response must look like instead of
// asserting that the adapters agree with each other. Set HTTPX_UPDATE_GOLDEN=1
// to rewrite them from a source checkout, then run the suite for every adapter
// to confirm they all still match. The package is deliberately importable from
// outside this repository, so a third-party adapter can certify itself against
// the same contract as the official five.
//
// To unit-test an application's own middleware or handlers, use
// github.com/go-sphere/httpx/httpxmock instead; this package is for adapter
// authors.
package httpxtest
