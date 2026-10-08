package ginx_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/ginx"
)

// serve dispatches one request in-process through httpx.TestRequester.
func serve(engine httpx.Engine, method, target string) {
	tr, ok := httpx.AsTestRequester(engine)
	if !ok {
		fmt.Println("no in-process requester")
		return
	}
	resp, err := tr.Do(httptest.NewRequest(method, target, nil))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(resp.StatusCode, string(body))
}

// Build an engine, register a group and a route, and serve requests
// in-process. Unmatched paths are rendered by the error handler.
func Example() {
	gin.SetMode(gin.ReleaseMode) // keep gin's debug log out of the output

	engine := ginx.New()
	api := engine.Group("/api")
	api.GET("/users/:id", func(ctx httpx.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
	})

	serve(engine, http.MethodGet, "/api/users/42")
	serve(engine, http.MethodGet, "/missing")
	// Output:
	// 200 {"id":"42"}
	// 404 {"success":false,"code":0,"message":"Not Found"}
}

// UseNative mounts gin middleware; it always runs outside the httpx layers
// registered on the same scope.
func ExampleRouter_UseNative() {
	gin.SetMode(gin.ReleaseMode)

	engine := ginx.New()
	api := engine.Group("/api")
	api.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			fmt.Println("httpx layer")
			return next(ctx)
		}
	})
	api.(*ginx.Router).UseNative(func(c *gin.Context) {
		fmt.Println("gin middleware")
		c.Next()
	})
	api.GET("/ping", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "pong")
	})

	serve(engine, http.MethodGet, "/api/ping")
	// Output:
	// gin middleware
	// httpx layer
	// 200 pong
}
