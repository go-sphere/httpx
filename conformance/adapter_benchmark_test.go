package conformance

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
	"github.com/labstack/echo/v4"
	"github.com/valyala/fasthttp"
)

// BenchmarkAdapter compares native and adapted routes through the same native
// dispatcher. Each worker owns reusable request/response buffers. Sockets and
// clients are excluded; native context resets and routing remain in the loop.
// Responses are validated before timing; both state handlers check their reads.
// StateParallel measures cross-request contention separately from serial latency.
func BenchmarkAdapter(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	hlog.SetLevel(hlog.LevelError)
	for _, framework := range []string{"gin", "echo", "fiber", "hertz"} {
		for _, scenario := range []string{"Empty", "Middleware1", "Middleware5", "Middleware10", "Middleware20", "JSON1K", "StateParallel"} {
			for _, mode := range []string{"native", "httpx"} {
				b.Run(fmt.Sprintf("framework=%s/scenario=%s/mode=%s", framework, scenario, mode), func(b *testing.B) {
					factory := adapterBenchmarkFactory(b, framework, scenario, mode == "httpx")
					b.ReportAllocs()
					if scenario == "StateParallel" {
						b.ResetTimer()
						b.RunParallel(func(pb *testing.PB) {
							run := factory()
							for pb.Next() {
								run()
							}
						})
					} else {
						run := factory()
						for b.Loop() {
							run()
						}
					}
				})
			}
		}
	}
}

// A compiled test binary doubles as the network target, sharing exactly the
// same route setup as the allocation benchmarks. Ordinary test runs skip it.
var benchmarkAddr = flag.String("httpx-bench-addr", "", "serve benchmark routes at this address")
var benchmarkFramework = flag.String("httpx-bench-framework", "gin", "gin, echo, fiber, or hertz")
var benchmarkMode = flag.String("httpx-bench-mode", "httpx", "native or httpx")
var benchmarkScenario = flag.String("httpx-bench-scenario", "JSON1K", "Empty, Middleware1/5/10/20, JSON1K, or StateParallel")

func TestAdapterBenchmarkServer(t *testing.T) {
	if *benchmarkAddr == "" {
		t.Skip("network benchmark target is opt-in")
	}
	if *benchmarkMode != "native" && *benchmarkMode != "httpx" {
		t.Fatal("invalid benchmark mode")
	}
	switch *benchmarkScenario {
	case "Empty", "Middleware1", "Middleware5", "Middleware10", "Middleware20", "JSON1K", "StateParallel":
	default:
		t.Fatal("invalid benchmark scenario")
	}
	gin.SetMode(gin.ReleaseMode)
	hlog.SetLevel(hlog.LevelError)
	adapterBenchmarkFactory(t, *benchmarkFramework, *benchmarkScenario, *benchmarkMode == "httpx", *benchmarkAddr)
}

type benchmarkJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"`
}

func adapterBenchmarkFactory(tb testing.TB, framework, scenario string, adapted bool, addr ...string) func() func() {
	return adapterBenchmarkFactoryWithRegistration(tb, framework, scenario, adapted, nil, addr...)
}

// A registration override lets API experiments share the exact dispatcher,
// request reset, and response validation used by the native/current controls.
func adapterBenchmarkFactoryWithRegistration(tb testing.TB, framework, scenario string, adapted bool, registration func(httpx.Engine), addr ...string) func() func() {
	tb.Helper()
	payload := &benchmarkJSON{ID: 42, Name: "benchmark", Data: strings.Repeat("x", 1024)}
	layers := 0
	switch scenario {
	case "Middleware1":
		layers = 1
	case "Middleware5":
		layers = 5
	case "Middleware10":
		layers = 10
	case "Middleware20":
		layers = 20
	}
	jsonResponse := scenario == "JSON1K"
	state := scenario == "StateParallel"
	register := func(e httpx.Engine) {
		r := e.Group("")
		for range layers {
			r.Use(func(c httpx.Context) error { return c.Next() })
		}
		r.GET("/bench", func(c httpx.Context) error {
			if state {
				c.Set("key", "value")
				if v, ok := c.Get("key"); !ok || v != "value" {
					panic("invalid state")
				}
			}
			if jsonResponse {
				return c.JSON(200, payload)
			}
			return c.NoContent(204)
		})
	}
	if registration != nil {
		register = registration
	}
	// Validate an actual response once, outside measurement, including JSON bytes.
	check := func(status int, body []byte) {
		tb.Helper()
		if jsonResponse {
			expected := fmt.Sprintf(`{"id":42,"name":"benchmark","data":"%s"}`, payload.Data)
			if status != 200 || strings.TrimSpace(string(body)) != expected {
				tb.Fatalf("invalid JSON response: %d %q", status, body)
			}
		} else if status != 204 || len(body) != 0 {
			tb.Fatalf("invalid empty response: %d %q", status, body)
		}
	}
	switch framework {
	case "gin":
		e := gin.New()
		if adapted {
			register(ginx.New(ginx.WithEngine(e)))
		} else {
			for range layers {
				e.Use(func(c *gin.Context) { c.Next() })
			}
			e.GET("/bench", func(c *gin.Context) {
				if state {
					c.Set("key", "value")
					if v, ok := c.Get("key"); !ok || v != "value" {
						panic("invalid state")
					}
				}
				if jsonResponse {
					c.JSON(200, payload)
				} else {
					c.Status(204)
				}
			})
		}
		if len(addr) != 0 {
			tb.Fatal(http.ListenAndServe(addr[0], e))
		}
		return netHTTPBenchmarkFactory(e, check)
	case "echo":
		e := echo.New()
		if adapted {
			register(echox.New(echox.WithEngine(e)))
		} else {
			r := e.Group("")
			for range layers {
				r.Use(func(next echo.HandlerFunc) echo.HandlerFunc { return func(c echo.Context) error { return next(c) } })
			}
			r.GET("/bench", func(c echo.Context) error {
				if state {
					c.Set("key", "value")
					if v := c.Get("key"); v != "value" {
						panic("invalid state")
					}
				}
				if jsonResponse {
					return c.JSON(200, payload)
				}
				return c.NoContent(204)
			})
		}
		if len(addr) != 0 {
			tb.Fatal(http.ListenAndServe(addr[0], e))
		}
		return netHTTPBenchmarkFactory(e, check)
	case "fiber":
		e := fiber.New()
		if adapted {
			register(fiberx.New(fiberx.WithEngine(e)))
		} else {
			for range layers {
				e.Use(func(c fiber.Ctx) error { return c.Next() })
			}
			e.Get("/bench", func(c fiber.Ctx) error {
				if state {
					c.Locals("key", "value")
					if v := c.Locals("key"); v != "value" {
						panic("invalid state")
					}
				}
				if jsonResponse {
					return c.JSON(payload)
				}
				c.Status(204)
				return nil
			})
		}
		if len(addr) != 0 {
			tb.Fatal(e.Listen(addr[0], fiber.ListenConfig{DisableStartupMessage: true}))
		}
		handler := e.Handler()
		probe := &fasthttp.RequestCtx{}
		probe.Request.SetRequestURI("/bench")
		handler(probe)
		check(probe.Response.StatusCode(), probe.Response.Body())
		return func() func() {
			c := &fasthttp.RequestCtx{}
			c.Request.SetRequestURI("/bench")
			return func() { c.ResetUserValues(); c.Response.Reset(); handler(c) }
		}
	case "hertz":
		var opts []config.Option
		if len(addr) != 0 {
			opts = append(opts, server.WithHostPorts(addr[0]))
		}
		e := server.New(opts...)
		if adapted {
			register(hertzx.New(hertzx.WithEngine(e)))
		} else {
			for range layers {
				e.Use(func(ctx context.Context, c *app.RequestContext) { c.Next(ctx) })
			}
			e.GET("/bench", func(_ context.Context, c *app.RequestContext) {
				if state {
					c.Set("key", "value")
					if v, ok := c.Get("key"); !ok || v != "value" {
						panic("invalid state")
					}
				}
				if jsonResponse {
					c.JSON(200, payload)
				} else {
					c.Status(204)
				}
			})
		}
		if len(addr) != 0 {
			tb.Fatal(e.Run())
		}
		probe := e.NewContext()
		probe.Request.SetRequestURI("/bench")
		e.ServeHTTP(context.Background(), probe)
		check(probe.Response.StatusCode(), probe.Response.Body())
		return func() func() {
			c := e.NewContext()
			return func() { c.ResetWithoutConn(); c.Request.SetRequestURI("/bench"); e.ServeHTTP(context.Background(), c) }
		}
	default:
		tb.Fatalf("unknown framework %q", framework)
		return nil
	}
}

// This writer retains its header capacity but never buffers response bodies.
// Both sides of each net/http pair use the same writer and request.
type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func (w *benchmarkResponseWriter) Header() http.Header         { return w.header }
func (w *benchmarkResponseWriter) WriteHeader(status int)      { w.status = status }
func (w *benchmarkResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func netHTTPBenchmarkFactory(h http.Handler, check func(int, []byte)) func() func() {
	return netHTTPBenchmarkFactoryForPath(h, "/bench", check)
}

func netHTTPBenchmarkFactoryForPath(h http.Handler, path string, check func(int, []byte)) func() func() {
	probe := httptest.NewRecorder()
	h.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, path, nil))
	check(probe.Code, probe.Body.Bytes())
	return func() func() {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := &benchmarkResponseWriter{header: make(http.Header)}
		return func() { clear(w.header); h.ServeHTTP(w, req) }
	}
}
