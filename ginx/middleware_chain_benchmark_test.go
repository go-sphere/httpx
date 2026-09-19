package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

func BenchmarkNestedScopes(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	pass := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}
	leaf := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }
	for _, mode := range []string{"native", "httpx"} {
		b.Run(fmt.Sprintf("mode=%s", mode), func(b *testing.B) {
			ge := gin.New()
			if mode == "native" {
				ge.Use(func(c *gin.Context) { c.Next() }, func(c *gin.Context) { c.Next() })
				g1 := ge.Group("/a", func(c *gin.Context) { c.Next() })
				g2 := g1.Group("/b", func(c *gin.Context) { c.Next() })
				g2.GET("/route", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			} else {
				app := New(WithEngine(ge))
				app.Use(pass, pass)
				g1 := app.Group("/a")
				g1.Use(pass)
				g2 := g1.Group("/b")
				g2.Use(pass)
				g2.GET("/route", leaf)
			}
			run := benchmarkRunner(b, ge, "/a/b/route", http.StatusNoContent)
			b.ReportAllocs()
			for b.Loop() {
				run()
			}
		})
	}
}
