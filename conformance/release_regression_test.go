package conformance

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/stdx"
)

func regressionServe(t *testing.T, e httpx.Engine, method, target string) (int, string) {
	t.Helper()
	q, ok := httpx.AsTestRequester(e)
	if !ok {
		t.Fatal("engine does not support TestRequester")
	}
	resp, err := q.Do(httptest.NewRequest(method, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

func TestReleaseRegressionRouterBacktracking(t *testing.T) {
	for _, wild := range []bool{false, true} {
		t.Run(fmt.Sprint(wild), func(t *testing.T) {
			e := stdx.New()
			r := e.Group("")
			r.GET("/a/:lost/end", func(c httpx.Context) error { return c.Text(200, "unreachable") })
			pattern := "/:first/:second/ok"
			if wild {
				pattern = "/*rest"
			}
			r.GET(pattern, func(c httpx.Context) error { return c.JSON(200, c.Params()) })
			_, body := regressionServe(t, e, "GET", "/a/value/ok")
			want := `{"first":"a","second":"value"}`
			if wild {
				want = `{"rest":"a/value/ok"}`
			}
			if body != want {
				t.Errorf("got %s; want %s", body, want)
			}
		})
	}
}

// Models middleware that buffers headers independently.
type regressionHeaderWriter struct {
	http.ResponseWriter
	header http.Header
}

func (w *regressionHeaderWriter) Header() http.Header { return w.header }
func (w *regressionHeaderWriter) WriteHeader(status int) {
	for k, v := range w.header {
		w.ResponseWriter.Header()[k] = v
	}
	w.ResponseWriter.WriteHeader(status)
}
func TestReleaseRegressionStdWriterHeaderCache(t *testing.T) {
	e := stdx.New()
	r := e.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { c.SetHeader("X-Before", "before"); return next(c) }
	})
	r.Use(stdx.AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(&regressionHeaderWriter{ResponseWriter: w, header: make(http.Header)}, req)
		})
	}))
	r.GET("/", func(c httpx.Context) error { c.SetHeader("X-Downstream", "value"); return c.NoContent(204) })
	q, ok := httpx.AsTestRequester(e)
	if !ok {
		t.Fatal("engine does not support TestRequester")
	}
	resp, err := q.Do(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("X-Downstream") != "value" {
		t.Fatal(resp.Header)
	}
}

func TestReleaseRegressionStdWriterSeparateHeaders(t *testing.T) {
	e := stdx.New()
	r := e.Group("")
	var observed string
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { c.SetHeader("X-Before", "before"); return next(c) }
	})
	r.Use(stdx.AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			wrapped := &regressionHeaderWriter{ResponseWriter: w, header: make(http.Header)}
			next.ServeHTTP(wrapped, req)
			observed = wrapped.Header().Get("X-Downstream")
		})
	}))
	r.GET("/", func(c httpx.Context) error { c.SetHeader("X-Downstream", "value"); return c.NoContent(204) })
	regressionServe(t, e, "GET", "/")
	if observed != "value" {
		t.Errorf("wrapped writer observed header=%q; want value", observed)
	}
}

func TestReleaseRegressionStdInformationalStatus(t *testing.T) {
	e := stdx.New()
	r := e.Group("")
	httpx.MountStd(r, "GET", "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(103)
		w.WriteHeader(201)
		_, _ = io.WriteString(w, "created")
	}))
	server := httptest.NewServer(e.(http.Handler))
	defer server.Close()
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("server returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 || string(b) != "created" {
		t.Errorf("status=%d body=%s; want 201 created", resp.StatusCode, b)
	}
}

// A handler still running after http.TimeoutHandler gave up must not touch the
// context handed to the next request: AdaptStdMiddleware isolates the
// continuation on a child context, so late state never leaks sideways.
func TestReleaseRegressionStdTimeoutPoolReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	e := stdx.New()
	r := e.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { c.Set("request", c.Param("id")); return next(c) }
	})
	r.Use(stdx.AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, 50*time.Millisecond, "timeout")
	}))
	release := make(chan struct{})
	type result struct {
		id    string
		state any
		err   error
	}
	finished := make(chan result, 1)
	r.GET("/:id", func(c httpx.Context) error {
		if c.Param("id") == "first" {
			<-release
			state, _ := c.Get("request")
			c.Set("late", true)
			got := result{id: c.Param("id"), state: state, err: c.Text(200, "late response")}
			finished <- got
			return got.err
		}
		if _, ok := c.Get("late"); ok {
			return c.Text(500, "state leaked")
		}
		return c.NoContent(204)
	})
	status, body := regressionServe(t, e, "GET", "/first")
	if status != 503 || body != "timeout" {
		close(release)
		t.Fatalf("timeout response=%d %q", status, body)
	}
	status, body = regressionServe(t, e, "GET", "/second")
	close(release)
	if status != 204 || body != "" {
		t.Fatalf("second response=%d %q", status, body)
	}
	select {
	case got := <-finished:
		if got.id != "first" || got.state != "first" || !errors.Is(got.err, http.ErrHandlerTimeout) {
			t.Errorf("late handler result=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
	status, body = regressionServe(t, e, "GET", "/third")
	if status != 204 || body != "" {
		t.Fatalf("third response=%d %q", status, body)
	}
}
