package stdx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/stdx"
)

// Build an engine, register a group and a route, and serve a request
// in-process through httpx.TestRequester.
func Example() {
	engine := stdx.New()
	api := engine.Group("/api")
	api.GET("/users/:id", func(ctx httpx.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
	})

	tr, ok := httpx.AsTestRequester(engine)
	if !ok {
		fmt.Println("no in-process requester")
		return
	}
	resp, err := tr.Do(httptest.NewRequest(http.MethodGet, "/api/users/42", nil))
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
	// Output:
	// 200 {"id":"42"}
}

// The engine is an http.Handler, so it can be mounted in another server or
// driven with an httptest.ResponseRecorder. Unmatched paths are rendered by
// the error handler.
func ExampleEngine_ServeHTTP() {
	engine := stdx.New()
	engine.Group("").GET("/ping", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "pong")
	})
	handler := engine.(http.Handler)

	for _, target := range []string{"/ping", "/missing"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		fmt.Println(rec.Code, rec.Body.String())
	}
	// Output:
	// 200 pong
	// 404 {"success":false,"code":0,"message":"Not Found"}
}

// Engines are single-use: after Stop, Start returns httpx.ErrEngineClosed
// without opening a listener.
func ExampleEngine_Stop() {
	engine := stdx.New(stdx.WithAddr("127.0.0.1:0"))
	if err := engine.Stop(context.Background()); err != nil {
		fmt.Println("stop:", err)
		return
	}
	err := engine.Start()
	fmt.Println(errors.Is(err, httpx.ErrEngineClosed))
	// Output:
	// true
}

// WithErrorHandler installs one framework-neutral error renderer; the same
// function works with every adapter's WithErrorHandler.
func ExampleWithErrorHandler() {
	engine := stdx.New(stdx.WithErrorHandler(func(ctx httpx.Context, err error) {
		_, status, message := httpx.ClassifyError(err)
		_ = ctx.Text(int(status), "error: "+message)
	}))
	engine.Group("").GET("/admin", func(ctx httpx.Context) error {
		return httpx.NewForbiddenError("admins only")
	})

	rec := httptest.NewRecorder()
	engine.(http.Handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 403 error: admins only
}

// AdaptStdMiddleware mounts a plain net/http middleware; header changes and
// short-circuits reach the httpx chain.
func ExampleAdaptStdMiddleware() {
	poweredBy := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Powered-By", "net/http")
			next.ServeHTTP(w, r)
		})
	}
	engine := stdx.New()
	engine.Use(stdx.AdaptStdMiddleware(poweredBy))
	engine.Group("").GET("/", func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	engine.(http.Handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("X-Powered-By"), rec.Body.String())
	// Output:
	// 200 net/http ok
}

// FromStd lets an ordinary http.Handler use httpx helpers without an Engine.
func ExampleFromStd() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := stdx.FromStd(w, r)
		httpx.DefaultErrorHandler(ctx, httpx.NewBadRequestError("name is required"))
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 400 {"success":false,"code":0,"message":"name is required"}
}
