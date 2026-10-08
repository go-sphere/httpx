package fiberx

import (
	"io"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func TestRouteShapePrecedence(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"/users/new", "/users/:id", true},
		{"/users/:id", "/users/new", false},
		{"/users/:id", "/users/*rest", true},
		{"/users/new", "/users/*rest", true},
		{"/users", "/users/*rest", true},
		{"/users/new", "/posts/:id", false},
		{"/users/:id/posts", "/users/:id/:tab", true},
		{"/a/:x", "/b/:y", false},
		{"/users/new/edit", "/users/:id/*rest", true},
		{"/a/b/:y", "/a/:x/c", true},
	} {
		if got := beats(parseRouteShape(tc.a), parseRouteShape(tc.b)); got != tc.want {
			t.Errorf("beats(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// Precedence holds for a caller driving the app's own handler, not only for
// Start and Do, because the order lives in fiber's route stack.
func TestRouteOrderReachesAppHandler(t *testing.T) {
	app := fiber.New(fiber.Config{CaseSensitive: true, StrictRouting: true})
	router := New(WithEngine(app)).Group("/users")
	router.GET("/:id", func(ctx httpx.Context) error { return ctx.Text(200, "param") })
	router.GET("/new", func(ctx httpx.Context) error { return ctx.Text(200, "static") })

	resp, err := app.Test(httptest.NewRequest("GET", "/users/new", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "static" {
		t.Fatalf("body = %q, want %q", body, "static")
	}
}

// Moving a route across a native middleware would change whether it wraps the
// route, so that registration fails loudly instead.
func TestRouteOrderRefusesToCrossNativeMiddleware(t *testing.T) {
	engine := New().(*Engine)
	router := engine.Group("")
	router.GET("/users/:id", func(ctx httpx.Context) error { return nil })
	engine.UseNative(func(c fiber.Ctx) error { return c.Next() })

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "/users/new") {
			t.Fatalf("recover() = %v, want a panic naming the route", r)
		}
	}()
	router.GET("/users/new", func(ctx httpx.Context) error { return nil })
}

// A native middleware whose prefix cannot match the moved route does not wrap
// it before or after the move, so registration goes ahead.
func TestRouteOrderCrossesDisjointNativeMiddleware(t *testing.T) {
	engine := New().(*Engine)
	root := engine.Group("")
	root.GET("/users/:id", func(ctx httpx.Context) error { return ctx.Text(200, "param") })
	engine.Group("/admin").(*Router).UseNative(func(c fiber.Ctx) error { return c.Next() })
	root.GET("/users/new", func(ctx httpx.Context) error { return ctx.Text(200, "static") })

	resp, err := engine.engine.Test(httptest.NewRequest("GET", "/users/new", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "static" {
		t.Fatalf("body = %q, want %q", body, "static")
	}
}

// Every registration order of overlapping routes answers each path with the
// same route.
func TestRouteOrderIndependentOfRegistration(t *testing.T) {
	routes := []string{"/a/:x/c", "/a/b/d", "/a/b/:y", "/a/b/*r", "/a/:x/:z"}
	want := map[string]string{
		"/a/b/c":   "/a/b/:y",
		"/a/b/d":   "/a/b/d",
		"/a/q/c":   "/a/:x/c",
		"/a/b/x/y": "/a/b/*r",
		"/a/q/z":   "/a/:x/:z",
	}
	var permute func(order []string, rest []string)
	permute = func(order []string, rest []string) {
		if len(rest) == 0 {
			engine := New().(*Engine)
			router := engine.Group("")
			for _, path := range order {
				router.GET(path, func(ctx httpx.Context) error { return ctx.Text(200, path) })
			}
			for path, route := range want {
				resp, err := engine.engine.Test(httptest.NewRequest("GET", path, nil))
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				if string(body) != route {
					t.Errorf("order %v: %s answered by %q, want %q", order, path, body, route)
				}
			}
			return
		}
		for i := range rest {
			next := append(slices.Clone(rest[:i]), rest[i+1:]...)
			permute(append(slices.Clone(order), rest[i]), next)
		}
	}
	permute(nil, routes)
}

func TestRouteOrderMayMatch(t *testing.T) {
	for _, tc := range []struct {
		path, route string
		want        bool
	}{
		{"/", "/users/new", true},
		{"/admin", "/users/new", false},
		{"/users", "/users/new", true},
		{"/USERS", "/users/new", false},
		{"/users/new/edit", "/users/new", false},
		{"/users/:id", "/users/new", true},
		{"/posts/*", "/users/new", false},
		{"/users/x", "/users/*", true},
	} {
		if got := mayMatch(tc.path, parseRouteShape(tc.route), true); got != tc.want {
			t.Errorf("mayMatch(%q, %q) = %v, want %v", tc.path, tc.route, got, tc.want)
		}
	}
}
