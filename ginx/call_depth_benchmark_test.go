package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// The sink makes the synthetic allocation observable; it is cleared after each
// sub-benchmark.
var diagnosticAllocation *diagnosticGinFrame

type diagnosticGinFrame struct {
	ctx        *gin.Context
	nextCalled bool
}

// Force a 16-byte escaping wrapper without adding request state or a pool.
//
//go:noinline
func diagnosticGinAllocate(c *gin.Context) {
	diagnosticAllocation = &diagnosticGinFrame{ctx: c, nextCalled: false}
}

// The forwarding functions deliberately add two calls without allocations.
//
//go:noinline
func diagnosticGinForwardOne(c *gin.Context) { diagnosticGinForwardTwo(c) }

//go:noinline
func diagnosticGinForwardTwo(c *gin.Context) { c.Next() }

func BenchmarkGinCallDepth(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	for _, layers := range []int{5, 8, 9, 10, 15, 20} {
		for _, mode := range []string{"native", "forwarded", "allocated", "forwardedAllocated", "httpx"} {
			b.Run(fmt.Sprintf("layers=%d/mode=%s", layers, mode), func(b *testing.B) {
				b.Cleanup(func() { diagnosticAllocation = nil })
				e := gin.New()
				if mode == "httpx" {
					r := New(WithEngine(e)).Group("")
					for range layers {
						r.Use(func(next httpx.Handler) httpx.Handler {
							return func(c httpx.Context) error { return next(c) }
						})
					}
					r.GET("/bench", func(c httpx.Context) error { return c.NoContent(204) })
				} else {
					for range layers {
						switch mode {
						case "forwarded":
							e.Use(func(c *gin.Context) { diagnosticGinForwardOne(c) })
						case "allocated":
							e.Use(func(c *gin.Context) { diagnosticGinAllocate(c); c.Next() })
						case "forwardedAllocated":
							e.Use(func(c *gin.Context) { diagnosticGinAllocate(c); diagnosticGinForwardOne(c) })
						default:
							e.Use(func(c *gin.Context) { c.Next() })
						}
					}
					e.GET("/bench", func(c *gin.Context) { c.Status(204) })
				}
				run := benchmarkRunner(b, e, "/bench", http.StatusNoContent)
				b.ReportAllocs()
				for b.Loop() {
					run()
				}
			})
		}
	}
}
