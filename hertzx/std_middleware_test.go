package hertzx

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

type readRecorder struct {
	r    io.Reader
	read bool
}

func (rr *readRecorder) Read(p []byte) (int, error) {
	rr.read = true
	return rr.r.Read(p)
}

type countingWriter struct {
	http.ResponseWriter
	written int
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	cw.written += len(p)
	return cw.ResponseWriter.Write(p)
}

// A DataFromReader body behind AdaptStdMiddleware stays a stream: nothing reads
// it while the chain runs, and the middleware's writer never sees it replayed.
func TestStdMiddlewarePassesBodyStreamThrough(t *testing.T) {
	body := &readRecorder{r: strings.NewReader("hello")}
	var readInChain bool
	var replayed int
	engine := New()
	router := engine.Group("")
	router.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			err := next(ctx)
			readInChain = body.read
			return err
		}
	})
	router.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := &countingWriter{ResponseWriter: w}
			next.ServeHTTP(cw, r)
			replayed = cw.written
		})
	}))
	router.GET("/stream", func(ctx httpx.Context) error {
		return ctx.DataFromReader(http.StatusOK, "text/plain", body, 5)
	})

	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "http://example.com/stream", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	if readInChain {
		t.Error("the body stream was read before the chain returned")
	}
	if replayed != 0 {
		t.Errorf("the middleware's writer received %d replayed bytes, want 0", replayed)
	}
	if string(got) != "hello" {
		t.Errorf("body = %q, want %q", got, "hello")
	}
}
