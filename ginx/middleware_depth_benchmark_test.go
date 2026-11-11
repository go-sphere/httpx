package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// Both modes cross the same gin dispatcher and return the same 204: "native"
// registers one gin middleware per layer, "httpx" composes into a single gin
// handler. BenchmarkGinDepthCurveGrownStack repeats the deeper points on a
// pre-grown stack, separating a cliff caused by runtime stack growth from one
// caused by the call chain itself.
func BenchmarkGinDepthCurve(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	passthrough := func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { return next(c) }
	}
	leaf := func(c httpx.Context) error { return c.NoContent(204) }
	for _, layers := range []int{4, 8, 12, 16, 19, 20, 21, 22, 23, 24, 26} {
		for _, mode := range []string{"native", "httpx"} {
			b.Run(fmt.Sprintf("layers=%d/mode=%s", layers, mode), func(b *testing.B) {
				ge := gin.New()
				if mode == "native" {
					for range layers {
						ge.Use(func(c *gin.Context) { c.Next() })
					}
					ge.GET("/bench", func(c *gin.Context) { c.Status(204) })
				} else {
					app := New(WithEngine(ge))
					r := app.Group("")
					for range layers {
						r.Use(passthrough)
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

// growStack grows the benchmark goroutine's stack before measuring, so a depth
// cliff from runtime stack growth can be told apart from one caused by the call
// chain itself.
//
//go:noinline
func growStack(n int) [64]byte {
	var pad [64]byte
	if n == 0 {
		return pad
	}
	inner := growStack(n - 1)
	pad[0] = inner[0] + 1
	return pad
}

var stackSink [64]byte

func BenchmarkGinDepthCurveGrownStack(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	passthrough := func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { return next(c) }
	}
	leaf := func(c httpx.Context) error { return c.NoContent(204) }
	for _, layers := range []int{18, 20, 24, 32} {
		for _, mode := range []string{"native", "httpx"} {
			b.Run(fmt.Sprintf("layers=%d/mode=%s", layers, mode), func(b *testing.B) {
				ge := gin.New()
				if mode == "native" {
					for range layers {
						ge.Use(func(c *gin.Context) { c.Next() })
					}
					ge.GET("/bench", func(c *gin.Context) { c.Status(204) })
				} else {
					app := New(WithEngine(ge))
					r := app.Group("")
					for range layers {
						r.Use(passthrough)
					}
					r.GET("/bench", leaf)
				}
				run := benchmarkRunner(b, ge, "/bench", http.StatusNoContent)
				stackSink = growStack(4096)
				b.ReportAllocs()
				for b.Loop() {
					run()
				}
			})
		}
	}
}
