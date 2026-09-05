package ginx

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestQueryBindingRunsValidation covers A4: query binding must run struct
// validation so `binding:"required"` behaves like gin's built-in bindings.
func TestQueryBindingRunsValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type dto struct {
		Name string `query:"name" binding:"required"`
	}

	t.Run("missing required query fails", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/x", nil)
		var dst dto
		if err := (QueryBinding{}).Bind(req, &dst); err == nil {
			t.Fatal("expected validation error for missing required query field")
		}
	})

	t.Run("present required query passes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/x?name=sphere", nil)
		var dst dto
		if err := (QueryBinding{}).Bind(req, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dst.Name != "sphere" {
			t.Fatalf("name = %q, want %q", dst.Name, "sphere")
		}
	})
}
