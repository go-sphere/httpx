package conformance

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/labstack/echo/v4"
)

// The native side is hand-written per framework: no shared table can express
// "the same scenario without httpx". Scenarios with no honest native
// counterpart (SSE, middleware-chain-only comparisons) are absent.
func BenchmarkNativeVsHTTPX(b *testing.B) {
	scenarios := make(map[string]httpxtest.Scenario, 20)
	for _, sc := range httpxtest.Scenarios() {
		scenarios[sc.Name] = sc
	}

	for _, suite := range httpxtestSuites() {
		native, ok := nativeBuilders[suite.Name]
		if !ok {
			continue
		}
		for _, name := range nativeScenarioNames {
			sc, ok := scenarios[name]
			if !ok {
				b.Fatalf("scenario %q is no longer in the shared table", name)
			}
			nativeReq, nativeRewind := sc.RewindableRequest()
			run, paired := native(b, name, nativeReq)
			if !paired {
				continue
			}
			b.Run("framework="+suite.Name+"/scenario="+name+"/mode=native", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					nativeRewind()
					run()
				}
			})
			b.Run("framework="+suite.Name+"/scenario="+name+"/mode=httpx", func(b *testing.B) {
				req, rewind := sc.RewindableRequest()
				run := suite.Dispatch(b, sc.Register, req)
				b.ReportAllocs()
				for b.Loop() {
					rewind()
					run()
				}
			})
		}
	}
}

var nativeScenarioNames = []string{
	"Empty", "JSON1K", "JSON100K", "State", "BindJSON", "BindFull",
	"LargeBody", "MultipartUpload", "StaticFile",
	"Middleware1", "Middleware5", "Middleware10",
}

var nativeBuilders = map[string]func(tb testing.TB, scenario string, req *http.Request) (func(), bool){
	"ginx":   buildNativeGin,
	"echox":  buildNativeEcho,
	"fiberx": buildNativeFiber,
	"hertzx": buildNativeHertz,
}

// Mirrors the shared scenario payloads so native handlers return identical bytes.
var (
	nativePayload1K   = map[string]any{"id": 42, "name": "benchmark", "data": strings.Repeat("x", 1024)}
	nativePayload100K = map[string]any{"id": 42, "data": strings.Repeat("x", 100*1024)}
	nativeAssets      = fstest.MapFS{
		"asset.txt": &fstest.MapFile{Data: []byte(strings.Repeat("static", 64))},
	}
)

type nativeBody struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type nativeQuery struct {
	Active bool `query:"active" form:"active"`
}

type nativeURI struct {
	ID string `uri:"id" path:"id" param:"id"`
}

type nativeHeader struct {
	Token string `header:"X-Token"`
}

func middlewareLayers(scenario string) int {
	switch scenario {
	case "Middleware1":
		return 1
	case "Middleware5":
		return 5
	case "Middleware10":
		return 10
	default:
		return 0
	}
}

func buildNativeGin(tb testing.TB, scenario string, req *http.Request) (func(), bool) {
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	for range middlewareLayers(scenario) {
		ge.Use(func(c *gin.Context) { c.Next() })
	}
	switch scenario {
	case "JSON1K":
		ge.GET("/scenario", func(c *gin.Context) { c.JSON(http.StatusOK, nativePayload1K) })
	case "JSON100K":
		ge.GET("/scenario", func(c *gin.Context) { c.JSON(http.StatusOK, nativePayload100K) })
	case "State":
		ge.GET("/scenario", func(c *gin.Context) {
			c.Set("key", "value")
			v, _ := c.Get("key")
			c.JSON(http.StatusOK, map[string]any{"value": v})
		})
	case "BindJSON":
		ge.POST("/scenario", func(c *gin.Context) {
			var v struct {
				Name string `json:"name"`
			}
			if err := c.ShouldBindJSON(&v); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			c.JSON(http.StatusOK, map[string]any{"name": v.Name})
		})
	case "BindFull":
		ge.POST("/scenario/:id", func(c *gin.Context) {
			var b nativeBody
			var q nativeQuery
			var u nativeURI
			var h nativeHeader
			if err := c.ShouldBindJSON(&b); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			if err := c.ShouldBindQuery(&q); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			if err := c.ShouldBindUri(&u); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			if err := c.ShouldBindHeader(&h); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			c.JSON(http.StatusOK, map[string]any{
				"name": b.Name, "age": b.Age, "active": q.Active, "id": u.ID, "token": h.Token,
			})
		})
	case "LargeBody":
		ge.POST("/scenario", func(c *gin.Context) {
			raw, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			c.JSON(http.StatusOK, map[string]any{"length": len(raw)})
		})
	case "MultipartUpload":
		ge.POST("/scenario", func(c *gin.Context) {
			f, err := c.FormFile("file")
			if err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			c.JSON(http.StatusOK, map[string]any{
				"filename": f.Filename, "size": f.Size, "title": c.PostForm("title"),
			})
		})
	case "StaticFile":
		ge.StaticFS("/assets", http.FS(nativeAssets))
	default:
		ge.GET("/scenario", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}
	return ginRunner(tb, "gin", ge, req), true
}

func buildNativeEcho(tb testing.TB, scenario string, req *http.Request) (func(), bool) {
	e := echo.New()
	for range middlewareLayers(scenario) {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		})
	}
	switch scenario {
	case "JSON1K":
		e.GET("/scenario", func(c echo.Context) error { return c.JSON(http.StatusOK, nativePayload1K) })
	case "JSON100K":
		e.GET("/scenario", func(c echo.Context) error { return c.JSON(http.StatusOK, nativePayload100K) })
	case "State":
		e.GET("/scenario", func(c echo.Context) error {
			c.Set("key", "value")
			return c.JSON(http.StatusOK, map[string]any{"value": c.Get("key")})
		})
	case "BindJSON":
		e.POST("/scenario", func(c echo.Context) error {
			var v struct {
				Name string `json:"name"`
			}
			if err := c.Bind(&v); err != nil {
				return c.NoContent(http.StatusBadRequest)
			}
			return c.JSON(http.StatusOK, map[string]any{"name": v.Name})
		})
	case "BindFull":
		e.POST("/scenario/:id", func(c echo.Context) error {
			var b nativeBody
			if err := c.Bind(&b); err != nil {
				return c.NoContent(http.StatusBadRequest)
			}
			active, _ := strconv.ParseBool(c.QueryParam("active"))
			return c.JSON(http.StatusOK, map[string]any{
				"name": b.Name, "age": b.Age, "active": active,
				"id": c.Param("id"), "token": c.Request().Header.Get("X-Token"),
			})
		})
	case "LargeBody":
		e.POST("/scenario", func(c echo.Context) error {
			raw, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return c.NoContent(http.StatusBadRequest)
			}
			return c.JSON(http.StatusOK, map[string]any{"length": len(raw)})
		})
	case "MultipartUpload":
		e.POST("/scenario", func(c echo.Context) error {
			f, err := c.FormFile("file")
			if err != nil {
				return c.NoContent(http.StatusBadRequest)
			}
			return c.JSON(http.StatusOK, map[string]any{
				"filename": f.Filename, "size": f.Size, "title": c.FormValue("title"),
			})
		})
	case "StaticFile":
		e.StaticFS("/assets", nativeAssets)
	default:
		e.GET("/scenario", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	}
	return netHTTPRunner(tb, "echo", e, req), true
}

func buildNativeFiber(tb testing.TB, scenario string, req *http.Request) (func(), bool) {
	f := fiber.New()
	for range middlewareLayers(scenario) {
		f.Use(func(c fiber.Ctx) error { return c.Next() })
	}
	switch scenario {
	case "JSON1K":
		f.Get("/scenario", func(c fiber.Ctx) error { return c.JSON(nativePayload1K) })
	case "JSON100K":
		f.Get("/scenario", func(c fiber.Ctx) error { return c.JSON(nativePayload100K) })
	case "State":
		f.Get("/scenario", func(c fiber.Ctx) error {
			c.Locals("key", "value")
			return c.JSON(map[string]any{"value": c.Locals("key")})
		})
	case "BindJSON":
		f.Post("/scenario", func(c fiber.Ctx) error {
			var v struct {
				Name string `json:"name"`
			}
			if err := c.Bind().JSON(&v); err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			return c.JSON(map[string]any{"name": v.Name})
		})
	case "BindFull":
		f.Post("/scenario/:id", func(c fiber.Ctx) error {
			var b nativeBody
			var q nativeQuery
			var h nativeHeader
			if err := c.Bind().JSON(&b); err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			if err := c.Bind().Query(&q); err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			if err := c.Bind().Header(&h); err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			return c.JSON(map[string]any{
				"name": b.Name, "age": b.Age, "active": q.Active,
				"id": c.Params("id"), "token": h.Token,
			})
		})
	case "LargeBody":
		f.Post("/scenario", func(c fiber.Ctx) error {
			return c.JSON(map[string]any{"length": len(c.Body())})
		})
	case "MultipartUpload":
		f.Post("/scenario", func(c fiber.Ctx) error {
			file, err := c.FormFile("file")
			if err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			return c.JSON(map[string]any{
				"filename": file.Filename, "size": file.Size, "title": c.FormValue("title"),
			})
		})
	case "StaticFile":
		f.Use("/assets", static.New("", static.Config{FS: nativeAssets}))
	default:
		f.Get("/scenario", func(c fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })
	}
	return fiberRunner(tb, "fiber", f, req), true
}

func buildNativeHertz(tb testing.TB, scenario string, req *http.Request) (func(), bool) {
	h := newBenchHertz()
	for range middlewareLayers(scenario) {
		h.Use(func(ctx context.Context, rc *app.RequestContext) { rc.Next(ctx) })
	}
	switch scenario {
	case "JSON1K":
		h.GET("/scenario", func(_ context.Context, rc *app.RequestContext) {
			rc.JSON(http.StatusOK, nativePayload1K)
		})
	case "JSON100K":
		h.GET("/scenario", func(_ context.Context, rc *app.RequestContext) {
			rc.JSON(http.StatusOK, nativePayload100K)
		})
	case "State":
		h.GET("/scenario", func(_ context.Context, rc *app.RequestContext) {
			rc.Set("key", "value")
			v, _ := rc.Get("key")
			rc.JSON(http.StatusOK, map[string]any{"value": v})
		})
	case "BindJSON":
		h.POST("/scenario", func(_ context.Context, rc *app.RequestContext) {
			var v struct {
				Name string `json:"name"`
			}
			if err := rc.BindJSON(&v); err != nil {
				rc.Status(http.StatusBadRequest)
				return
			}
			rc.JSON(http.StatusOK, map[string]any{"name": v.Name})
		})
	case "BindFull":
		// hertz binds body, query, path and header in one call, the native idiom.
		h.POST("/scenario/:id", func(_ context.Context, rc *app.RequestContext) {
			var in struct {
				Name   string `json:"name"`
				Age    int    `json:"age"`
				Active bool   `query:"active"`
				ID     string `path:"id"`
				Token  string `header:"X-Token"`
			}
			if err := rc.Bind(&in); err != nil {
				rc.Status(http.StatusBadRequest)
				return
			}
			rc.JSON(http.StatusOK, map[string]any{
				"name": in.Name, "age": in.Age, "active": in.Active, "id": in.ID, "token": in.Token,
			})
		})
	case "LargeBody":
		h.POST("/scenario", func(_ context.Context, rc *app.RequestContext) {
			rc.JSON(http.StatusOK, map[string]any{"length": len(rc.Request.Body())})
		})
	case "MultipartUpload":
		h.POST("/scenario", func(_ context.Context, rc *app.RequestContext) {
			f, err := rc.FormFile("file")
			if err != nil {
				rc.Status(http.StatusBadRequest)
				return
			}
			rc.JSON(http.StatusOK, map[string]any{
				"filename": f.Filename, "size": f.Size, "title": rc.PostForm("title"),
			})
		})
	case "StaticFile":
		// hertz's native static (app.FS) serves only from disk, so it cannot be
		// compared like-for-like with the scenario's in-memory FS; decline.
		return nil, false
	default:
		h.GET("/scenario", func(_ context.Context, rc *app.RequestContext) {
			rc.Status(http.StatusNoContent)
		})
	}
	return hertzRunner(tb, "hertz", h, req), true
}
