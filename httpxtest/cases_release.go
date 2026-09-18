package httpxtest

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() { register("ReleaseRegression", casesReleaseRegression) }
func casesReleaseRegression(t *testing.T, run runner) {
	t.Run("ConvertedCapabilities", func(t *testing.T) { releaseConvertedCapabilities(t, run) })
	t.Run("EmptyCommitMatrix", func(t *testing.T) { releaseEmptyCommitMatrix(t, run) })
	t.Run("HeaderCase", func(t *testing.T) { releaseHeaderCase(t, run) })
	t.Run("ValidationInterface", func(t *testing.T) { releaseValidationInterface(t, run) })
	t.Run("StatusAfterCommit", func(t *testing.T) { releaseStatusAfterCommit(t, run) })
	t.Run("ConvertedStdMiddleware", func(t *testing.T) { releaseConvertedStdMiddleware(t, run) })
	t.Run("StatusOnlyShortCircuit", func(t *testing.T) { releaseStatusOnlyShortCircuit(t, run) })
	t.Run("ErrorAfterEmptyCommitted", func(t *testing.T) { releaseErrorAfterEmptyCommitted(t, run) })
	t.Run("EngineInterceptorInherited", func(t *testing.T) { releaseEngineInterceptorInherited(t, run) })
	t.Run("SecondNextAfterStop", func(t *testing.T) { releaseSecondNextAfterStop(t, run) })
}

func serveReleaseEngine(t *testing.T, e httpx.Engine, method, target string) (int, string) {
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

func releaseHeaderCase(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	e.Group("").GET("/", func(c httpx.Context) error {
		var dst struct {
			Token string `header:"x-token" binding:"required"`
		}
		if err := c.BindHeader(&dst); err != nil {
			return err
		}
		return c.Text(200, dst.Token)
	})
	q, ok := httpx.AsTestRequester(e)
	if !ok {
		t.Fatal("engine does not support TestRequester")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Token", "present")
	resp, err := q.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || string(b) != "present" {
		t.Errorf("status=%d body=%s", resp.StatusCode, b)
	}
}

func releaseValidationInterface(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	e.Group("").GET("/", func(c httpx.Context) error {
		var child struct {
			Required string `binding:"required"`
		}
		dst := struct{ Child any }{Child: &child}
		if err := c.BindQuery(&dst); err != nil {
			return err
		}
		return c.Text(200, "validation skipped")
	})
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if status != 400 {
		t.Errorf("status=%d body=%s; want 400", status, body)
	}
}

func releaseStatusAfterCommit(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	observed := 0
	e.Group("").GET("/", func(c httpx.Context) error {
		if err := c.Text(201, "created"); err != nil {
			return err
		}
		c.Status(503)
		observed = c.StatusCode()
		return nil
	})
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if status != 201 || observed != 201 {
		t.Errorf("wire=%d observed=%d body=%s; want 201", status, observed, body)
	}
}

func releaseConvertedStdMiddleware(t *testing.T, run runner) {
	if run.suite.StdMiddleware == nil {
		t.Skip("no StdMiddleware hook declared")
	}
	s := run.suite
	e := s.NewEngine(t, Options{})
	r := e.Group("")
	httpx.UseInterceptor(r, httpx.AsInterceptor(s.StdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Trace", "ok"); next.ServeHTTP(w, r) })
	})))
	r.GET("/", func(c httpx.Context) error { return c.Text(200, "ok") })
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if status != 200 || body != "ok" {
		t.Errorf("status=%d body=%s", status, body)
	}
}

func releaseStatusOnlyShortCircuit(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	r := e.Group("")
	r.Use(func(c httpx.Context) error { c.Status(401); return nil })
	r.GET("/", func(c httpx.Context) error { return c.Text(200, "unreachable") })
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if status != 401 || body != "" {
		t.Errorf("status=%d body=%s; want 401 empty", status, body)
	}
}

func releaseErrorAfterEmptyCommitted(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	e.Group("").GET("/", func(c httpx.Context) error {
		if err := c.NoContent(202); err != nil {
			return err
		}
		return errors.New("late")
	})
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if status != 202 || body != "" {
		t.Errorf("status=%d body=%s; want 202 empty", status, body)
	}
}

func releaseEngineInterceptorInherited(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	ran := false
	if composed := httpx.UseInterceptor(e, func(next httpx.Handler) httpx.Handler { return func(c httpx.Context) error { return c.NoContent(401) } }); composed != s.Caps.ComposesInterceptors {
		t.Fatal("engine interceptor capability disagrees with Caps")
	}
	e.Group("/api").Group("/v1").GET("/secret", func(c httpx.Context) error { ran = true; return c.Text(200, "secret") })
	status, body := serveReleaseEngine(t, e, "GET", "/api/v1/secret")
	if ran || status != 401 {
		t.Errorf("handlerRan=%v status=%d body=%s; want false,401", ran, status, body)
	}
}

func releaseSecondNextAfterStop(t *testing.T, run runner) {
	s := run.suite
	e := s.NewEngine(t, Options{})
	r := e.Group("")
	ran := false
	r.Use(func(c httpx.Context) error {
		if err := c.Next(); err != nil {
			return err
		}
		return c.Next()
	})
	r.Use(func(c httpx.Context) error { return c.NoContent(401) })
	r.GET("/", func(c httpx.Context) error { ran = true; return nil })
	status, body := serveReleaseEngine(t, e, "GET", "/")
	if ran {
		t.Errorf("blocked handler ran on second Next: status=%d body=%s", status, body)
	}
}

func releaseConvertedCapabilities(t *testing.T, run runner) {
	var hasNative, hasStream, hasFlush bool
	got := run.serve(t, func(router httpx.Router) {
		router.Use(func(c httpx.Context) error {
			_, hasNative = c.(httpx.NativeContextProvider)
			_, hasStream = httpx.AsStreamer(c)
			_, hasFlush = httpx.AsFlusher(c)
			return c.Next()
		})
		httpx.UseInterceptor(router, httpx.AsInterceptor(func(c httpx.Context) error {
			if _, ok := httpx.AsFlusher(c); ok != hasFlush {
				t.Error("Flusher capability changed")
			}
			if _, ok := c.(httpx.NativeContextProvider); ok != hasNative {
				t.Error("native context capability changed")
			}
			if _, ok := httpx.AsStreamer(c); ok != hasStream {
				t.Error("Streamer capability changed")
			}
			if !hasStream {
				return c.Text(200, "converted")
			}
			return httpx.ServerSentEvents(c, func(w *httpx.SSEWriter) error { return w.SendData("converted") })
		}))
		router.GET("/", func(c httpx.Context) error { t.Error("short-circuited handler ran"); return nil })
	}, httptest.NewRequest("GET", "/", nil))
	want := "converted"
	if hasStream {
		want = "data: converted\n\n"
	}
	if got.Status != 200 || got.Body != want {
		t.Fatalf("unexpected converted stream: %+v", got)
	}
}

func releaseEmptyCommitMatrix(t *testing.T, run runner) {
	for _, code := range []int{200, 202, 204, 205} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			for _, commit := range []bool{false, true} {
				got := run.serve(t, func(router httpx.Router) {
					router.GET("/", func(c httpx.Context) error {
						if commit {
							if err := c.NoContent(code); err != nil {
								return err
							}
						} else {
							c.Status(code)
						}
						return errors.New("late failure")
					})
				}, httptest.NewRequest("GET", "/", nil))
				want := 500
				if commit {
					want = code
				}
				if got.Status != want || commit && got.Body != "" {
					t.Fatalf("commit=%v code=%d: %+v", commit, code, got)
				}
			}
		})
	}
}
