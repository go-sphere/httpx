package ginx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// Diagnostics for how middleware cost grows with chain depth. Both modes cross
// the same gin dispatcher and return the same 204; "native" registers one gin
// middleware per layer, "httpx" registers httpx middlewares, which ginx runs as
// a single fused gin handler.
//
// These curves exist to locate the depth at which cost stops being linear. That
// cliff is a property of nested call depth and frame size, not of the adapter:
// native gin reaches it a few layers later, and neither GC nor Go stack growth
// explains it (see BenchmarkGinDepthCurveGrownStack and
// BenchmarkChainFrameSizeCurve, and benchmarks/MIDDLEWARE_FUSION_REPORT.md).
func BenchmarkGinDepthCurve(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	passthrough := func(c httpx.Context) error { return c.Next() }
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

// The same curve with no framework in the picture: a chain of N functions where
// every layer calls Next once. "concrete" drives Next as a direct method call,
// "iface" through an interface, which is what a chain of httpx middlewares has
// to do. Only the shape of the curve is meaningful here; absolute values exclude
// routing and response writing.

type curveNext interface{ Next() error }

type curveChain struct {
	chain []func(curveNext) error
	idx   int
}

func (c *curveChain) Next() error {
	i := c.idx
	if i >= len(c.chain) {
		return nil
	}
	c.idx = i + 1
	return c.chain[i](c)
}

type curveConcrete struct {
	chain []func(*curveConcrete) error
	idx   int
}

func (c *curveConcrete) Next() error {
	i := c.idx
	if i >= len(c.chain) {
		return nil
	}
	c.idx = i + 1
	return c.chain[i](c)
}

var curveSink error

func BenchmarkChainDepthCurve(b *testing.B) {
	for _, layers := range []int{4, 8, 10, 12, 14, 16, 18, 20, 24, 32} {
		ifaceChain := make([]func(curveNext) error, layers)
		for i := range ifaceChain {
			ifaceChain[i] = func(c curveNext) error { return c.Next() }
		}
		concreteChain := make([]func(*curveConcrete) error, layers)
		for i := range concreteChain {
			concreteChain[i] = func(c *curveConcrete) error { return c.Next() }
		}
		b.Run(fmt.Sprintf("layers=%d/mode=iface", layers), func(b *testing.B) {
			for b.Loop() {
				c := &curveChain{chain: ifaceChain}
				curveSink = c.Next()
			}
		})
		b.Run(fmt.Sprintf("layers=%d/mode=concrete", layers), func(b *testing.B) {
			for b.Loop() {
				c := &curveConcrete{chain: concreteChain}
				curveSink = c.Next()
			}
		})
	}
}

// growStack forces the benchmark goroutine's stack to grow before measuring, so
// a depth cliff caused by runtime stack growth can be told apart from one
// caused by the call chain itself.
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
	passthrough := func(c httpx.Context) error { return c.Next() }
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

// The padded variants keep the frame count per layer identical and only grow
// each frame, which separates "too many nested calls" from "too much nested
// stack" as the cause of the depth cliff.
type curvePadded struct {
	chain []func(*curvePadded) error
	idx   int
}

func (c *curvePadded) Next() error {
	i := c.idx
	if i >= len(c.chain) {
		return nil
	}
	c.idx = i + 1
	return c.chain[i](c)
}

var padSink byte

func BenchmarkChainFrameSizeCurve(b *testing.B) {
	for _, layers := range []int{8, 12, 16, 20, 24, 32} {
		for _, pad := range []int{0, 256, 1024} {
			b.Run(fmt.Sprintf("layers=%d/pad=%d", layers, pad), func(b *testing.B) {
				chain := make([]func(*curvePadded) error, layers)
				for i := range chain {
					switch pad {
					case 0:
						chain[i] = func(c *curvePadded) error { return c.Next() }
					case 256:
						chain[i] = func(c *curvePadded) error {
							var local [256]byte
							err := c.Next()
							padSink = local[0]
							return err
						}
					default:
						chain[i] = func(c *curvePadded) error {
							var local [1024]byte
							err := c.Next()
							padSink = local[0]
							return err
						}
					}
				}
				for b.Loop() {
					c := &curvePadded{chain: chain}
					curveSink = c.Next()
				}
			})
		}
	}
}
