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

// failingWriter is a ResponseWriter whose connection is gone: every body write
// fails.
type failingWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w failingWriter) Write([]byte) (int, error)       { return 0, w.err }
func (w failingWriter) WriteString(string) (int, error) { return 0, w.err }

// Text and Bytes report a failed write to the handler instead of leaving it in
// gin's c.Errors.
func TestTextAndBytesReturnWriteError(t *testing.T) {
	failed := errors.New("client gone")
	for name, write := range map[string]func(httpx.Context) error{
		"Text":  func(c httpx.Context) error { return c.Text(http.StatusOK, "text") },
		"Bytes": func(c httpx.Context) error { return c.Bytes(http.StatusOK, []byte("png"), "image/png") },
	} {
		t.Run(name, func(t *testing.T) {
			gc, _ := gin.CreateTestContext(failingWriter{ResponseRecorder: httptest.NewRecorder(), err: failed})
			if err := write(FromGin(gc)); !errors.Is(err, failed) {
				t.Fatalf("%s = %v, want the write error", name, err)
			}
			if len(gc.Errors) != 0 {
				t.Fatalf("gin recorded %v; the error belongs to the caller", gc.Errors)
			}
		})
	}
}
