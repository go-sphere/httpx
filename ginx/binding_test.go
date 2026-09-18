package ginx

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// queryBinding decodes with gin's own field mapping and validates nothing: a
// `binding` tag is inert (httpx.Binder), and unlike gin's built-in bindings
// this one never reaches gin's package-level validator at all.
func TestQueryBindingDecodesWithoutValidating(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type dto struct {
		Name string   `query:"name" binding:"required"`
		Tags []string `query:"tags"`
	}

	t.Run("absent required query field binds to its zero value", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/x", nil)
		var dst dto
		if err := (queryBinding{}).Bind(req, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dst.Name != "" {
			t.Fatalf("name = %q, want empty", dst.Name)
		}
	})

	t.Run("present query fields decode", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/x?name=sphere&tags=a&tags=b", nil)
		var dst dto
		if err := (queryBinding{}).Bind(req, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dst.Name != "sphere" || len(dst.Tags) != 2 || dst.Tags[0] != "a" || dst.Tags[1] != "b" {
			t.Fatalf("dst = %+v", dst)
		}
	})

	t.Run("a decode failure is still an error", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/x?n=not-a-number", nil)
		var dst struct {
			N int `query:"n"`
		}
		if err := (queryBinding{}).Bind(req, &dst); err == nil {
			t.Fatal("expected a decode error for a non-numeric int field")
		}
	})
}
