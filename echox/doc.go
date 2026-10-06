// Package echox implements the httpx interfaces over Echo v4
// (github.com/labstack/echo/v4).
//
// Use it to run httpx handlers and middleware on an Echo instance, either one
// the adapter creates or an existing *echo.Echo passed with [WithEngine].
// [New] builds the engine; [WithAddr], [WithServer], [WithErrorHandler] and
// [WithTrustedProxies] configure it. Echo's own middleware is mounted with
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
//     the registered name and pattern.
//   - Implements httpx.Flusher, httpx.Streamer, httpx.StdHandlerMounter and
//     httpx.TestRequester.
//   - httpx middleware runs inside every UseNative layer on the same scope,
//     whatever the registration order.
//   - Errors echo raises itself (404, 405) are rendered by the configured
//     httpx error handler too, because the adapter owns Echo.HTTPErrorHandler.
//   - Stop cuts connections still in flight once its context expires.
package echox
