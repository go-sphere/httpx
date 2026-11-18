// Package fiberx implements the httpx interfaces over Fiber v3
// (github.com/gofiber/fiber/v3).
//
// Support level: reference only. fiberx is an incomplete implementation kept
// as an example of adapting a buffering, fasthttp-based framework to httpx;
// several behaviours are approximated (see the adapter specifics below). Do
// not use it in production; use stdx or ginx.
//
// Use it to run httpx handlers and middleware on a Fiber app, either one the
// adapter creates or an existing *fiber.App passed with [WithEngine]. [New]
// builds the engine; [WithAddr] (or [WithListen], [WithListener]),
// [WithErrorHandler], [WithTrustedProxies] and [WithMaxBodySize] configure it. Fiber's own
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
//   - Implements httpx.Streamer, httpx.ResponseHeaderEditor,
//     httpx.StdHandlerMounter and httpx.TestRequester, but not httpx.Flusher: fiber buffers the response
//     until the handler returns, and a Stream callback runs after that. Stream
//     sends the status line and headers before the callback runs.
//   - An app built by the adapter routes like the other adapters: case
//     sensitive, strict about trailing slashes, no HEAD answered by a GET
//     route, and a literal '+' in the path kept as '+'. It also sets
//     DisableDefaultContentType, so a response nothing labelled carries no
//     Content-Type.
//   - fiber.Config is immutable after fiber.New, so an app supplied with
//     WithEngine keeps its own ErrorHandler, BodyLimit, UnescapePath,
//     CaseSensitive, StrictRouting, DisableHeadAutoRegister and
//     DisableDefaultContentType settings and cannot be combined with
//     WithTrustedProxies. In particular, when passing WithEngine(app)
//     alongside WithMaxBodySize(n) where n > app.Config().BodyLimit, callers
//     must set BodyLimit on fiber.Config when creating app. httpx handler
//     errors are still rendered by the adapter, and fasthttp's default
//     Content-Type is never reported by ResponseHeaderValues nor copied by
//     HandleStd or AdaptStdMiddleware. WithErrorHandler(nil) is ignored.
//   - BodyReader, BodyRaw and BindJSON read the request body as received:
//     unlike fiber's Body, they do not decode a Content-Encoding. BindJSON
//     uses the app's JSONDecoder.
//   - Route precedence does not depend on registration order: the adapter
//     reorders fiber's stack so a static segment beats a parameter and a
//     parameter beats a wildcard.
//   - Register UseNative middleware before the routes it should wrap: fiber
//     matches its stack in registration order. A route that would have to
//     move across such a middleware to keep its precedence panics, unless the
//     middleware's prefix cannot match the route's path.
//   - Behind AdaptStdMiddleware a streamed response (Stream, SSE, a
//     DataFromReader body stream) keeps streaming; the middleware sees its
//     headers but cannot transform its body.
//   - A handler mounted with HandleStd, Static or StaticFS writes under the
//     same post-commit rules as the Responder methods, and its writer starts
//     with the headers the httpx chain set.
//   - Stop closes the listeners but cannot cut connections still in flight.
//     A Stop that lands while Start is still on its way into fasthttp keeps
//     shutting down until Start returns or its context is done. Start returns
//     nil once Stop ended it, even when a WithListener listener reports its
//     own error for having been closed; Stop before Start returns nil.
package fiberx
