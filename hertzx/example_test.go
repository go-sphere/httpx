package hertzx_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/hertzx"
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
	engine := hertzx.New()
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

// WithErrorHandler takes the framework-neutral httpx.ErrorHandler, so one
// renderer can be shared by every adapter.
func ExampleWithErrorHandler() {
	engine := hertzx.New(hertzx.WithErrorHandler(func(ctx httpx.Context, err error) {
		_, status, message := httpx.ClassifyError(err)
		_ = ctx.Text(int(status), "error: "+message)
	}))
	engine.Group("").GET("/admin", func(ctx httpx.Context) error {
		return httpx.NewForbiddenError("admins only")
	})

	serve(engine, http.MethodGet, "/admin")
	// Output:
	// 403 error: admins only
}
