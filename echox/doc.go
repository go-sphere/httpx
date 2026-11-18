// Package echox implements the httpx interfaces over Echo v4
// (github.com/labstack/echo/v4).
//
// Support level: experimental. echox runs the full shared conformance suite
// and is fairly complete, but it is not held to production standards and is
// not recommended for production use; use stdx or ginx there.
//
// Use it to run httpx handlers and middleware on an Echo instance, either one
// the adapter creates or an existing *echo.Echo passed with [WithEngine].
// [New] builds the engine; [WithAddr], [WithServer], [WithErrorHandler],
// [WithTrustedProxies] and [WithMaxBodySize] configure it. Echo's own middleware is mounted with
// [Engine.UseNative] or [Router.UseNative], plain net/http middleware with
// [AdaptStdMiddleware], and [FromEcho] wraps an echo.Context as an
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
//		"github.com/go-sphere/httpx/echox"
//	)
//
//	engine := echox.New(echox.WithAddr(":8080"))
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
//   - The native context is echo.Context: httpx.AsNativeContext[echo.Context](ctx).
//   - Echo has no named wildcards; the adapter rewrites /files/*name to echo's
//     anonymous form and keeps Param("name"), BindURI and FullPath reporting
//     the name and pattern each method registered.
//   - Implements httpx.Flusher, httpx.Streamer, httpx.ResponseHeaderEditor,
//     httpx.StdHandlerMounter and httpx.TestRequester.
//   - httpx middleware runs inside every UseNative layer on the same scope,
//     whatever the registration order.
//   - Errors echo raises itself (404, 405) are rendered by the configured
//     httpx error handler too, because the adapter owns Echo.HTTPErrorHandler.
//   - An error a route returns is rendered inside the route, by the configured
//     error handler (echo's HTTPErrorHandler without WithErrorHandler), so a
//     UseNative layer sees the rendered response and a nil error after next.
//   - Stop cuts connections still in flight once its context expires.
//   - A server the adapter creates bounds header reads (10s) and idle
//     keep-alive connections (120s); one supplied with [WithServer] is used as
//     configured.
package echox
