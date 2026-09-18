package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// WithErrorHandler now takes httpx.ErrorHandler on every adapter; the gin
// shape moved to WithNativeErrorHandler. These tests pin both, including the
// Abort bookkeeping the httpx-typed wrapper owns: without it gin would run the
// rest of the chain over the error body.

func TestWithErrorHandlerIsHTTPXShaped(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	var gotCtx httpx.Context
	engine := New(WithEngine(ge), WithErrorHandler(func(ctx httpx.Context, err error) {
		gotCtx = ctx
		_ = ctx.JSON(http.StatusTeapot, map[string]string{"error": err.Error()})
	}))

	engine.Group("").GET("/boom", func(ctx httpx.Context) error {
		return httpx.NewInternalServerError("boom")
	})

	rr := httptest.NewRecorder()
	ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
	if gotCtx == nil {
		t.Fatal("handler did not receive an httpx.Context")
	}
	if _, ok := httpx.AsNativeContext[*gin.Context](gotCtx); !ok {
		t.Fatal("the httpx.Context handed to the error handler is not gin-backed")
	}
}

// The wrapper aborts the gin context so no later handler overwrites the error
// body it just wrote.
func TestWithErrorHandlerAborts(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	engine := New(WithEngine(ge), WithErrorHandler(func(ctx httpx.Context, err error) {
		_ = ctx.JSON(http.StatusTeapot, map[string]string{"error": "handled"})
	}))

	var later bool
	r := engine.Group("")
	r.Use(func(ctx httpx.Context) error {
		return httpx.NewInternalServerError("boom")
	})
	r.GET("/x", func(ctx httpx.Context) error {
		later = true
		return ctx.Text(http.StatusOK, "should not run")
	})

	rr := httptest.NewRecorder()
	ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if later {
		t.Fatal("the handler ran after the error handler rendered")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
}

func TestWithNativeErrorHandler(t *testing.T) {
	// DefaultErrorHandler is gin's native shape, so it goes through the native
	// option — that assignability is what the naming rule promises.
	_ = WithNativeErrorHandler(DefaultErrorHandler)

	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	var gotNative *gin.Context
	engine := New(WithEngine(ge), WithNativeErrorHandler(func(c *gin.Context, err error) {
		gotNative = c
		c.JSON(http.StatusTeapot, gin.H{"error": err.Error()})
		c.Abort()
	}))
	engine.Group("").GET("/boom", func(ctx httpx.Context) error {
		return httpx.NewInternalServerError("boom")
	})

	rr := httptest.NewRecorder()
	ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if gotNative == nil {
		t.Fatal("native handler did not receive a *gin.Context")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
}
