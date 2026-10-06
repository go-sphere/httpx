// Package fiberx implements the httpx interfaces over Fiber v3
// (github.com/gofiber/fiber/v3).
//
// Use it to run httpx handlers and middleware on a Fiber app, either one the
// adapter creates or an existing *fiber.App passed with [WithEngine]. [New]
// builds the engine; [WithAddr] (or [WithListen], [WithListener]),
// [WithErrorHandler] and [WithTrustedProxies] configure it. Fiber's own
// middleware is mounted with [Engine.UseNative] or [Router.UseNative], plain
// net/http middleware with [AdaptStdMiddleware], and [FromFiber] wraps a
// fiber.Ctx as an httpx.Context.
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
//		"github.com/go-sphere/httpx/fiberx"
//	)
//
//	engine := fiberx.New(fiberx.WithAddr(":8080"))
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
//   - The native context is fiber.Ctx: httpx.AsNativeContext[fiber.Ctx](ctx).
//   - Fiber has no named wildcards; the adapter rewrites /files/*name to
//     fiber's anonymous form and keeps Param("name"), BindURI and FullPath
//     reporting the registered name and pattern.
//   - Implements httpx.Streamer, httpx.StdHandlerMounter and
//     httpx.TestRequester, but not httpx.Flusher: fiber buffers the response
//     until the handler returns, and a Stream callback runs after that.
//   - fiber.Config is immutable after fiber.New, so an app supplied with
//     WithEngine keeps its own ErrorHandler and UnescapePath settings and
//     cannot be combined with WithTrustedProxies. httpx handler errors are
//     still rendered by the adapter.
//   - Register UseNative middleware before the routes it should wrap: fiber
//     matches its stack in registration order.
//   - Stop closes the listeners but cannot cut connections still in flight.
//     Start returns nil once Stop ended it, even when a WithListener listener
//     reports its own error for having been closed; Stop before Start
//     returns nil.
package fiberx
