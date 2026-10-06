// Package stdx implements the httpx interfaces over plain net/http, with no
// web framework dependency.
//
// It is the reference adapter for the httpx route grammar: a hand-written
// route tree matching static segments, ":name" parameters and one final
// "*name" wildcard, with static > parameter > wildcard priority and none of
// http.ServeMux's redirects, path cleaning or case fixups. Choose it when no
// framework is wanted, when an httpx engine must be mounted inside another
// net/http server (the Engine is an http.Handler), or as the cheapest real
// engine for tests.
//
// [New] builds the engine; [WithAddr], [WithServer], [WithErrorHandler] and
// [WithTrustedProxies] configure it. Plain net/http middleware is mounted with
// [AdaptStdMiddleware], and [FromStd] wraps an ordinary handler's request pair
// as an httpx.Context. There is no UseNative: net/http has no middleware type
// other than the one AdaptStdMiddleware takes.
//
// # Usage
//
//	import (
//		"context"
//		"log"
//		"net/http"
//		"time"
//
//		"github.com/go-sphere/httpx"
//		"github.com/go-sphere/httpx/stdx"
//	)
//
//	engine := stdx.New(stdx.WithAddr(":8080"))
//	api := engine.Group("/api")
//	api.GET("/ping", func(ctx httpx.Context) error {
//		return ctx.Text(http.StatusOK, "pong")
//	})
//
//	go func() {
//		if err := engine.Start(); err != nil { // blocks until Stop
//			log.Print(err)
//		}
//	}()
//	// ... on shutdown:
//	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := engine.Stop(stopCtx); err != nil {
//		log.Print(err)
//	}
//
// To serve through another server or a test recorder instead of Start, use
// the engine as a handler: engine.(http.Handler), or the [Engine.Do] method
// through httpx.AsTestRequester.
//
// # Adapter specifics
//
//   - The native context is *[Native]: httpx.AsNativeContext[*stdx.Native](ctx).
//   - Named wildcards are supported directly (httpx.RouterFeatureNamedWildcard).
//   - Implements httpx.Flusher (a writer that cannot flush is a no-op),
//     httpx.Streamer, httpx.StdHandlerMounter and httpx.TestRequester.
//   - Query, form, URI and header binding use github.com/go-playground/form;
//     JSON binding uses encoding/json. Binding decodes and does not validate.
//   - Stop cuts connections still in flight once its context expires.
package stdx
