package conformance

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

// A Context retained by an inner handler must stay usable from outer middleware
// after next returns, including with a native layer around the chain where
// the adapter has one.
func TestContextWrappersSurviveUnwind(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine := a.NewEngine(t, httpxtest.Options{})
			router := engine.Group("")
			var inner, leaf httpx.Context
			var order []string
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					defer func() { inner, leaf = nil, nil }()
					order = append(order, "outer-before")
					err := next(c)
					if inner == nil || leaf == nil {
						t.Error("downstream did not execute")
						return err
					}
					for _, retained := range []httpx.Context{inner, leaf} {
						if retained.Param("id") != "42" {
							t.Error("retained context lost route parameters")
						}
						if retained.StatusCode() != 204 {
							t.Error("retained context lost response status")
						}
						a, aok := httpx.AsNativeContext[any](c)
						b, bok := httpx.AsNativeContext[any](retained)
						if !aok || !bok || a != b {
							t.Error("native context identity changed")
						}
						retained.Set("after", "still-valid")
						if v, ok := c.Get("after"); !ok || v != "still-valid" {
							t.Error("retained context lost shared state")
						}
					}
					order = append(order, "outer-after")
					return err
				}
			})
			expected := []string{"outer-before", "inner-before", "leaf", "inner-after", "outer-after"}
			if a.NativeMiddleware != nil {
				// UseNative runs outside the httpx layers of its scope.
				a.NativeMiddleware(router, func(mark string) { order = append(order, mark) })
				expected = append(append([]string{"native-pre"}, expected...), "native-post")
			}
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					inner = c
					order = append(order, "inner-before")
					err := next(c)
					order = append(order, "inner-after")
					return err
				}
			})
			router.GET("/unwind/:id", func(c httpx.Context) error {
				leaf = c
				order = append(order, "leaf")
				return c.NoContent(204)
			})
			for range 3 {
				order = nil
				if got := mustServe(t, engine, http.MethodGet, "/unwind/42"); got.Status != 204 {
					t.Fatalf("unexpected response: %+v", got)
				}
				if !reflect.DeepEqual(order, expected) {
					t.Fatalf("order = %v, want %v", order, expected)
				}
			}
		})
	}
}

// A value-backed wrapper copied into an interface must keep request access
// shared, and SetContext must update the native request.
func TestWrapperSetContextAfterNext(t *testing.T) {
	type contextKey struct{}
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine := a.NewEngine(t, httpxtest.Options{})
			router := engine.Group("")
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					c.SetContext(context.WithValue(c.Context(), contextKey{}, "outer"))
					err := next(c)
					if c.Context().Value(contextKey{}) != "handler" {
						t.Error("SetContext below was not visible to the layer above")
					}
					return err
				}
			})
			router.GET("/context", func(c httpx.Context) error {
				if c.Context().Value(contextKey{}) != "outer" {
					t.Error("handler did not receive middleware context")
				}
				c.SetContext(context.WithValue(c.Context(), contextKey{}, "handler"))
				return c.NoContent(204)
			})
			if got := mustServe(t, engine, http.MethodGet, "/context"); got.Status != 204 {
				t.Fatalf("status = %d", got.Status)
			}
		})
	}
}
