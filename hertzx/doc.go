// Package hertzx implements the httpx interfaces over CloudWeGo Hertz
// (github.com/cloudwego/hertz).
//
// Use it to run httpx handlers and middleware on a Hertz server, either one
// the adapter creates or an existing *server.Hertz passed with [WithEngine].
// [New] builds the engine; [WithAddr], [WithErrorHandler] and
// [WithTrustedProxies] configure it. Hertz's own middleware is mounted with
// [Engine.UseNative] or [Router.UseNative], plain net/http middleware with
// [AdaptStdMiddleware], and [FromHertz] wraps a hertz request context as an
// httpx.Context.
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
//		"github.com/go-sphere/httpx/hertzx"
//	)
//
//	engine := hertzx.New(hertzx.WithAddr(":8080"))
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
// # Adapter specifics
//
//   - The native context is *app.RequestContext:
//     httpx.AsNativeContext[*app.RequestContext](ctx).
//   - Named wildcards are supported directly (httpx.RouterFeatureNamedWildcard).
//   - Implements httpx.Flusher (switching to a chunked body on the first
//     flush), httpx.Streamer, httpx.StdHandlerMounter and httpx.TestRequester.
//   - httpx middleware runs inside every UseNative layer on the same scope,
//     whatever the registration order.
//   - Without WithAddr the server listens on hertz's default address (":8888").
//   - A 405 answer carries no Allow header: hertz does not report which
//     methods matched.
//   - Stop closes the listener but cannot cut connections still in flight.
//     Stop before Start returns nil rather than hertz's "engine is not
//     running", and Start returns nil once Stop ended it.
package hertzx
