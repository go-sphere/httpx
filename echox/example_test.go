package echox_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
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
	fmt.Println(resp.StatusCode, strings.TrimSpace(string(body))) // echo ends JSON with a newline
}

// Build an engine, register a group and a route, and serve requests
// in-process. Unmatched paths are rendered by the error handler.
func Example() {
	engine := echox.New()
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

// Echo has no named wildcards, but the adapter keeps the registered name and
// pattern visible to Param and FullPath.
func ExampleRouter_Handle_namedWildcard() {
	engine := echox.New()
	engine.Group("/files").GET("/*path", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, ctx.FullPath()+" "+ctx.Param("path"))
	})

	serve(engine, http.MethodGet, "/files/docs/readme.txt")
	// Output:
	// 200 /files/*path docs/readme.txt
}
