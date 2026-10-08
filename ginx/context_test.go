package ginx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// gin consumes the reader before DataFromReader returns, so a copy that fails
// part-way is reported to the handler for a known and an unknown size alike.
func TestDataFromReaderReturnsCopyError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	failed := errors.New("reader failed")
	for _, size := range []int64{-1, 8} {
		engine := New(WithEngine(gin.New()))
		var got error
		engine.Group("").GET("/", func(c httpx.Context) error {
			got = c.DataFromReader(http.StatusOK, "application/octet-stream",
				io.MultiReader(strings.NewReader("abcd"), iotest.ErrReader(failed)), size)
			return nil
		})
		resp, err := engine.(httpx.TestRequester).Do(httptest.NewRequest(http.MethodGet, "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if !errors.Is(got, failed) {
			t.Errorf("size=%d: DataFromReader = %v, want the reader's error", size, got)
		}
	}
}
