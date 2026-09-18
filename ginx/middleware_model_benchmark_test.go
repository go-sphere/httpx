package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// Diagnostic: what the shape of a middleware costs, independent of the
// adapters. The two forms deliver the same behavior but not the same call
// graph.
//
//	httpx.Middleware — continues the chain with ctx.Next()
//	  per layer: Next frame -> middleware frame          = 2 calls, 2 frames
//	  per request: one chain object holding the layer index
//
//	httpx.Interceptor — takes the rest of the chain and calls it
//	  per layer: middleware frame calls the next closure = 1 call, 1 frame
//	  per request: nothing, the chain is composed into the route once
//
// Both run through the same gin dispatcher and the same ginx Context, so the
// difference is the form, not the adapter. The layers here are bare
// pass-throughs, which is the upper bound on what the form can buy: real
// middleware does work of its own that this dispatch cost disappears into —
// see BenchmarkRealStack in sphere/server/middleware for the same comparison
// over the access-log, recovery, auth and permission middlewares.
func BenchmarkMiddlewareModel(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	middlewareForm := func(ctx httpx.Context) error { return ctx.Next() }
	interceptorForm := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}
	leaf := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }

	for _, layers := range []int{4, 10, 20, 24, 32, 40} {
		for _, mode := range []string{"native", "middleware", "interceptor"} {
			b.Run(fmt.Sprintf("layers=%d/mode=%s", layers, mode), func(b *testing.B) {
				ge := gin.New()
				switch mode {
				case "native":
					for range layers {
						ge.Use(func(c *gin.Context) { c.Next() })
					}
					ge.GET("/bench", func(c *gin.Context) { c.Status(http.StatusNoContent) })
				case "middleware":
					app := New(WithEngine(ge))
					r := app.Group("")
					for range layers {
						r.Use(middlewareForm)
					}
					r.GET("/bench", leaf)
				case "interceptor":
					app := New(WithEngine(ge))
					r := app.Group("")
					for range layers {
						if !httpx.UseInterceptor(r, interceptorForm) {
							b.Fatal("router did not register interceptors natively")
						}
					}
					r.GET("/bench", leaf)
				}
				run := benchmarkRunner(b, ge, "/bench", http.StatusNoContent)
				b.ReportAllocs()
				for b.Loop() {
					run()
				}
			})
		}
	}
}
