package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
	"github.com/labstack/echo/v4"
	"testing"
)

type benchmarkHarness struct {
	router httpx.Router
	do     func(*http.Request) (int, error)
}

const benchmarkNoiseRoutes = 1200

func BenchmarkFrameworkRouting(b *testing.B) {
	frameworks := []string{"ginx", "fiberx", "echox", "hertzx"}
	for _, name := range frameworks {
		b.Run(name, func(b *testing.B) {
			h := newBenchmarkHarness(b, name)
			registerNoiseRoutes(h.router, benchmarkNoiseRoutes)
			registerBenchmarkRoute(h.router)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, "http://example.com/bench/42?name=tom", nil)
				req.Header.Set("X-Bench", "1")
				status, err := h.do(req)
				if err != nil {
					b.Fatalf("request failed: %v", err)
				}
				if status != http.StatusOK {
					b.Fatalf("unexpected status: %d", status)
				}
			}
		})
	}
}

func BenchmarkFrameworkComplexRequest(b *testing.B) {
	type payload struct {
		Profile struct {
			Name    string   `json:"name"`
			Tags    []string `json:"tags"`
			Address struct {
				City string `json:"city"`
				Zip  string `json:"zip"`
			} `json:"address"`
		} `json:"profile"`
		Items []struct {
			SKU   string `json:"sku"`
			Qty   int    `json:"qty"`
			Price int    `json:"price"`
		} `json:"items"`
	}
	type uriBind struct {
		OrgID  string `uri:"orgID"`
		UserID string `uri:"userID"`
	}
	type queryBind struct {
		Mode   string `query:"mode"`
		Locale string `query:"locale"`
	}
	type headerBind struct {
		ReqID string `header:"X-Req-ID"`
	}

	bodyBytes, err := json.Marshal(map[string]any{
		"profile": map[string]any{
			"name": "benchmark-user",
			"tags": []string{"a", "b", "c", "d", "e"},
			"address": map[string]any{
				"city": "Shanghai",
				"zip":  "200000",
			},
		},
		"items": []map[string]any{
			{"sku": "SKU-1", "qty": 1, "price": 29},
			{"sku": "SKU-2", "qty": 2, "price": 39},
			{"sku": "SKU-3", "qty": 3, "price": 49},
			{"sku": "SKU-4", "qty": 4, "price": 59},
		},
	})
	if err != nil {
		b.Fatalf("marshal benchmark body failed: %v", err)
	}

	frameworks := []string{"ginx", "fiberx", "echox", "hertzx"}
	for _, name := range frameworks {
		b.Run(name, func(b *testing.B) {
			h := newBenchmarkHarness(b, name)
			registerNoiseRoutes(h.router, benchmarkNoiseRoutes)

			h.router.Use(func(ctx httpx.Context) error {
				ctx.Set("trace", "bench-complex")
				return ctx.Next()
			})
			api := h.router.Group("/api", func(ctx httpx.Context) error {
				ctx.Set("scope", "api")
				return ctx.Next()
			})
			v1 := api.Group("/v1")
			v1.POST("/orgs/:orgID/users/:userID/orders", func(ctx httpx.Context) error {
				var p payload
				var u uriBind
				var q queryBind
				var hd headerBind

				if err := ctx.BindURI(&u); err != nil {
					return err
				}
				if err := ctx.BindQuery(&q); err != nil {
					return err
				}
				if err := ctx.BindHeader(&hd); err != nil {
					return err
				}
				if err := ctx.BindJSON(&p); err != nil {
					return err
				}

				total := 0
				for _, item := range p.Items {
					total += item.Qty * item.Price
				}

				return ctx.JSON(200, map[string]any{
					"route":  map[string]any{"org": u.OrgID, "user": u.UserID},
					"query":  map[string]any{"mode": q.Mode, "locale": q.Locale},
					"header": hd.ReqID,
					"profile": map[string]any{
						"name": p.Profile.Name,
						"city": p.Profile.Address.City,
						"tags": len(p.Profile.Tags),
					},
					"total": total,
					"trace": valueOrEmpty(ctx, "trace"),
				})
			})

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(
					http.MethodPost,
					"http://example.com/api/v1/orgs/org-01/users/u-88/orders?mode=sync&locale=zh-CN",
					bytes.NewReader(bodyBytes),
				)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Req-ID", "req-123")

				status, err := h.do(req)
				if err != nil {
					b.Fatalf("request failed: %v", err)
				}
				if status != http.StatusOK {
					b.Fatalf("unexpected status: %d", status)
				}
			}
		})
	}
}

func registerBenchmarkRoute(r httpx.Router) {
	r.Use(func(ctx httpx.Context) error {
		ctx.Set("trace", "v1")
		return ctx.Next()
	})
	r.GET("/bench/:id", func(ctx httpx.Context) error {
		return ctx.JSON(200, map[string]any{
			"id":    ctx.Param("id"),
			"query": ctx.Query("name"),
			"trace": valueOrEmpty(ctx, "trace"),
		})
	})
}

func valueOrEmpty(ctx httpx.Context, key string) string {
	v, ok := ctx.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func registerNoiseRoutes(r httpx.Router, count int) {
	templates := []string{
		"/noise/static/v1/projects/:projectID/releases/:releaseID/files/:fileID",
		"/noise/api/v2/tenants/:tenantID/users/:userID/permissions/:permID",
		"/noise/internal/region/:region/service/:service/op/:operation",
		"/noise/gateway/:gw/cluster/:cluster/node/:nodeID/metric/:metric",
		"/noise/data/v3/org/:orgID/team/:teamID/member/:memberID/action/:action",
	}

	for i := 0; i < count; i++ {
		base := templates[i%len(templates)]
		path := fmt.Sprintf("%s/slot/%04d", base, i)

		switch i % 5 {
		case 0:
			r.GET(path, func(ctx httpx.Context) error { return ctx.NoContent(204) })
		case 1:
			r.POST(path, func(ctx httpx.Context) error { return ctx.NoContent(204) })
		case 2:
			r.PUT(path, func(ctx httpx.Context) error { return ctx.NoContent(204) })
		case 3:
			r.PATCH(path, func(ctx httpx.Context) error { return ctx.NoContent(204) })
		default:
			r.DELETE(path, func(ctx httpx.Context) error { return ctx.NoContent(204) })
		}
	}
}

func newBenchmarkHarness(b *testing.B, name string) benchmarkHarness {
	b.Helper()

	switch name {
	case "ginx":
		gin.SetMode(gin.ReleaseMode)
		g := gin.New()
		g.Use(gin.Recovery())
		engine := ginx.New(ginx.WithEngine(g), ginx.WithServerAddr(":0"))
		return benchmarkHarness{
			router: engine.Group(""),
			do: func(req *http.Request) (int, error) {
				rr := httptest.NewRecorder()
				g.ServeHTTP(rr, req)
				return rr.Code, nil
			},
		}
	case "fiberx":
		app := fiber.New(fiber.Config{
			ErrorHandler: func(ctx fiber.Ctx, err error) error {
				return ctx.Status(500).JSON(fiber.Map{"error": err.Error()})
			},
		})
		engine := fiberx.New(fiberx.WithEngine(app), fiberx.WithListen(":0"))
		return benchmarkHarness{
			router: engine.Group(""),
			do: func(req *http.Request) (int, error) {
				resp, err := app.Test(req)
				if err != nil {
					return 0, err
				}
				defer resp.Body.Close()
				_, _ = io.Copy(io.Discard, resp.Body)
				return resp.StatusCode, nil
			},
		}
	case "echox":
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			_ = c.JSON(500, echo.Map{"error": err.Error()})
		}
		engine := echox.New(echox.WithEngine(e), echox.WithServerAddr(":0"))
		return benchmarkHarness{
			router: engine.Group(""),
			do: func(req *http.Request) (int, error) {
				rr := httptest.NewRecorder()
				e.ServeHTTP(rr, req)
				return rr.Code, nil
			},
		}
	case "hertzx":
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
		h := server.Default(server.WithHostPorts("127.0.0.1:0"), server.WithDisablePrintRoute(true))
		engine := hertzx.New(hertzx.WithEngine(h))
		return benchmarkHarness{
			router: engine.Group(""),
			do: func(req *http.Request) (int, error) {
				urlStr := req.URL.String()
				if !req.URL.IsAbs() {
					urlStr = "http://example.com" + req.URL.RequestURI()
				}

				var bodyBytes []byte
				if req.Body != nil {
					var err error
					bodyBytes, err = io.ReadAll(req.Body)
					if err != nil {
						return 0, err
					}
				}

				hctx := h.Engine.NewContext()
				hctx.Request.Header.SetMethod(req.Method)
				hctx.Request.SetRequestURI(urlStr)
				if len(bodyBytes) > 0 {
					hctx.Request.SetBodyStream(bytes.NewReader(bodyBytes), len(bodyBytes))
				}
				for key, values := range req.Header {
					for _, value := range values {
						hctx.Request.Header.Add(key, value)
					}
				}
				h.Engine.ServeHTTP(context.Background(), hctx)
				_ = len(hctx.Response.Body())
				return hctx.Response.StatusCode(), nil
			},
		}
	default:
		b.Fatalf("unknown framework: %s", name)
		return benchmarkHarness{}
	}
}
