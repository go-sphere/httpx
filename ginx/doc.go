// Package ginx implements the httpx interfaces over Gin
// (github.com/gin-gonic/gin).
//
// Support level: production. ginx implements every httpx interface and
// optional capability and behaves as stdx, the reference adapter, does; a
// difference between the two is a ginx bug.
//
// Use it to run httpx handlers and middleware on a Gin engine, either one the
// adapter creates or an existing *gin.Engine passed with [WithEngine]. [New]
// builds the engine; [WithAddr], [WithServer], [WithErrorHandler],
// [WithTrustedProxies] and [WithMaxBodySize] configure it. Gin's own middleware is mounted with
// [Engine.UseNative] or [Router.UseNative], plain net/http middleware with
// [AdaptStdMiddleware], and [FromGin] wraps a *gin.Context as an
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
//		"github.com/go-sphere/httpx/ginx"
//	)
//
//	engine := ginx.New(ginx.WithAddr(":8080"))
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
//   - The native context is *gin.Context: httpx.AsNativeContext[*gin.Context](ctx).
//   - Named wildcards are supported directly (httpx.RouterFeatureNamedWildcard).
//   - Implements httpx.Flusher, httpx.Streamer, httpx.ResponseHeaderEditor,
//     httpx.StdHandlerMounter and httpx.TestRequester.
//   - Binding goes through gin's binding package; gin still runs its validator
//     over `binding` tags, but the adapter drops the verdict so Bind* only
//     reports decode failures, like every other adapter.
//   - httpx middleware runs inside every UseNative layer on the same scope,
//     whatever the registration order.
//   - net/http code mounted with [AdaptStdMiddleware] or HandleStd writes with
//     net/http semantics: WriteHeader commits the response at once, where gin's
//     writer holds the status until the first body write.
//   - The engine starts with no gin middleware; [WithDefaultMiddleware] adds
//     gin's Logger and Recovery.
//   - Stop cuts connections still in flight once its context expires.
//   - A server the adapter creates bounds header reads (10s) and idle
//     keep-alive connections (120s); one supplied with [WithServer] is used as
//     configured.
package ginx
