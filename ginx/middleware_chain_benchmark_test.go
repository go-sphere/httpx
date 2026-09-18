package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// BenchmarkNestedScopes measures the registration shape a service actually
// uses: two engine-level middlewares (access log, recovery) plus one per nested
// group (auth, permission). BenchmarkAdapter only covers a flat group, where
// nothing has to be carried across group inheritance.
func BenchmarkNestedScopes(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	pass := func(ctx httpx.Context) error { return ctx.Next() }
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
				app.Group("/a", pass).Group("/b", pass).GET("/route", leaf)
			}
			run := benchmarkRunner(b, ge, "/a/b/route", http.StatusNoContent)
			b.ReportAllocs()
			for b.Loop() {
				run()
			}
		})
	}
}
