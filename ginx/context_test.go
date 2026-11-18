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

func TestGinContextEdgeFixes(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	engine := New(WithEngine(gin.New()))
	var queryBefore, queryInHandler string
	var hints []int
	engine.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error {
			// Populate Gin's queryCache before AdaptStdMiddleware runs.
			queryBefore = c.Query("name")
			return next(c)
		}
	})
	engine.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/q" {
				r2 := r.Clone(r.Context())
				u := *r2.URL
				u.RawQuery = "name=rewritten"
				r2.URL = &u
				next.ServeHTTP(w, r2)
				return
			}
			if r.URL.Path == "/103" {
				next.ServeHTTP(&testHintWriter{ResponseWriter: w, hints: &hints}, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}))
	r := engine.Group("")
	r.GET("/q", func(c httpx.Context) error {
		queryInHandler = c.Query("name")
		return c.NoContent(http.StatusNoContent)
	})
	r.GET("/103", func(c httpx.Context) error {
		if err := c.NoContent(http.StatusEarlyHints); err != nil {
			return err
		}
		if c.Committed() {
			return errors.New("103 committed")
		}
		return c.Text(http.StatusOK, "done")
	})

	req := httptest.NewRequest(http.MethodGet, "/q?name=orig", nil)
	resp, err := engine.(httpx.TestRequester).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if queryBefore != "orig" || queryInHandler != "rewritten" {
		t.Fatalf("Query before=%q after rewrite=%q, want orig / rewritten", queryBefore, queryInHandler)
	}

	req103 := httptest.NewRequest(http.MethodGet, "/103", nil)
	resp103, err := engine.(httpx.TestRequester).Do(req103)
	if err != nil {
		t.Fatal(err)
	}
	body103, _ := io.ReadAll(resp103.Body)
	_ = resp103.Body.Close()
	if resp103.StatusCode != http.StatusOK || string(body103) != "done" || len(hints) != 1 || hints[0] != http.StatusEarlyHints {
		t.Fatalf("/103 status=%d body=%q hints=%v, want 200 done [103]", resp103.StatusCode, string(body103), hints)
	}
}

type testHintWriter struct {
	http.ResponseWriter
	hints *[]int
}

func (h *testHintWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		*h.hints = append(*h.hints, code)
		return
	}
	h.ResponseWriter.WriteHeader(code)
}
