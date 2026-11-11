package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// benchmarkHarness is an engine serving on a real listener, measured through
// an HTTP client.
type benchmarkHarness struct {
	router  httpx.Router
	engine  httpx.Engine
	addr    string
	baseURL string
	client  *http.Client
}

const benchmarkNoiseRoutes = 1200

func BenchmarkFrameworkRouting(b *testing.B) {
	for _, a := range adapters() {
		b.Run(a.Name, func(b *testing.B) {
			h := newBenchmarkHarness(b, a)
			registerNoiseRoutes(h.router, benchmarkNoiseRoutes)
			registerBenchmarkRoute(h.router)
			startBenchmarkHarness(b, h)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req, err := http.NewRequest(http.MethodGet, h.baseURL+"/bench/42?name=tom", nil)
				if err != nil {
					b.Fatalf("build request failed: %v", err)
				}
				req.Header.Set("X-Bench", "1")
				status, err := doRequest(h.client, req)
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

	for _, a := range adapters() {
		b.Run(a.Name, func(b *testing.B) {
			h := newBenchmarkHarness(b, a)
			registerNoiseRoutes(h.router, benchmarkNoiseRoutes)

			h.router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.Set("trace", "bench-complex")
					return next(ctx)
				}
			})
			api := h.router.Group("/api")
			api.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.Set("scope", "api")
					return next(ctx)
				}
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
			startBenchmarkHarness(b, h)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req, err := http.NewRequest(
					http.MethodPost,
					h.baseURL+"/api/v1/orgs/org-01/users/u-88/orders?mode=sync&locale=zh-CN",
					bytes.NewReader(bodyBytes),
				)
				if err != nil {
					b.Fatalf("build request failed: %v", err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Req-ID", "req-123")

				status, err := doRequest(h.client, req)
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
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			ctx.Set("trace", "v1")
			return next(ctx)
		}
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

	for i := range count {
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

func newBenchmarkHarness(b *testing.B, a adapter) benchmarkHarness {
	b.Helper()
	engine, addr := a.serve(b, serveConfig{})
	router := engine.Group("")
	router.GET("/__ready", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
	return benchmarkHarness{
		router:  router,
		engine:  engine,
		addr:    addr,
		baseURL: "http://" + addr,
		client:  &http.Client{Timeout: 2 * time.Second},
	}
}

// startBenchmarkHarness serves once every route is registered. fiberx and
// hertzx accept connections on their pre-bound listener before Start serves,
// so readiness is one answered request rather than a dial.
func startBenchmarkHarness(b *testing.B, h benchmarkHarness) {
	b.Helper()
	startServing(b, h.engine, h.addr)
	req, err := http.NewRequest(http.MethodGet, h.baseURL+"/__ready", nil)
	if err != nil {
		b.Fatalf("build ready request: %v", err)
	}
	if status, err := doRequest(h.client, req); err != nil || status != http.StatusNoContent {
		b.Fatalf("ready request: status=%d err=%v, want 204", status, err)
	}
}

func doRequest(client *http.Client, req *http.Request) (int, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	if resp == nil || resp.Body == nil {
		return 0, errors.New("nil response or body")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
