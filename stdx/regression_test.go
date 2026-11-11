package stdx

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
)

type servedResponse struct {
	Status int
	Body   string
}

// serveTest dispatches one request through e's TestRequester.
func serveTest(t *testing.T, e httpx.Engine, method, target string) servedResponse {
	t.Helper()
	resp, err := e.(httpx.TestRequester).Do(httptest.NewRequest(method, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return servedResponse{Status: resp.StatusCode, Body: string(body)}
}

// The router backtracks out of a static branch that dead-ends, and the values
// recorded on the abandoned branch do not leak into the route that matches.
func TestRouterBacktrackingDropsAbandonedValues(t *testing.T) {
	for _, wild := range []bool{false, true} {
		t.Run(fmt.Sprint(wild), func(t *testing.T) {
			e := New()
			r := e.Group("")
			r.GET("/a/:lost/end", func(c httpx.Context) error { return c.Text(200, "unreachable") })
			pattern := "/:first/:second/ok"
			if wild {
				pattern = "/*rest"
			}
			r.GET(pattern, func(c httpx.Context) error { return c.JSON(200, c.Params()) })
			body := serveTest(t, e, "GET", "/a/value/ok").Body
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

// A 1xx informational status from a mounted handler is passed through and
// does not commit the response: the final status still follows.
func TestMountedInformationalStatus(t *testing.T) {
	e := New()
	httpx.MountStd(e.Group(""), "GET", "/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
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
	if resp.StatusCode != http.StatusCreated || string(b) != "created" {
		t.Errorf("status=%d body=%s; want 201 created", resp.StatusCode, b)
	}
}

// A handler still running after http.TimeoutHandler gave up must not touch the
// context handed to the next request: AdaptStdMiddleware isolates the
// continuation on a child context, so late state never leaks sideways.
func TestTimeoutContinuationDoesNotTouchPooledContext(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	e := New()
	r := e.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error { c.Set("request", c.Param("id")); return next(c) }
	})
	r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
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
	if resp := serveTest(t, e, "GET", "/first"); resp.Status != 503 || resp.Body != "timeout" {
		close(release)
		t.Fatalf("timeout response=%d %q", resp.Status, resp.Body)
	}
	resp := serveTest(t, e, "GET", "/second")
	close(release)
	if resp.Status != 204 || resp.Body != "" {
		t.Fatalf("second response=%d %q", resp.Status, resp.Body)
	}
	select {
	case got := <-finished:
		if got.id != "first" || got.state != "first" || !errors.Is(got.err, http.ErrHandlerTimeout) {
			t.Errorf("late handler result=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
	if resp := serveTest(t, e, "GET", "/third"); resp.Status != 204 || resp.Body != "" {
		t.Fatalf("third response=%d %q", resp.Status, resp.Body)
	}
}
