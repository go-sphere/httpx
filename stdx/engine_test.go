package stdx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

func TestServeHTTPDispatch(t *testing.T) {
	var tr trace
	engine, ok := New().(*Engine)
	if !ok {
		t.Fatal("New did not return *Engine")
	}
	engine.Use(tr.mw("engine"))
	r := engine.Group("")
	r.Use(tr.mw("group"))
	r.GET("/x", tr.leaf("leaf"))
	r.HEAD("/x", tr.leaf("head"))

	t.Run("MatchedRouteRunsWholeChain", func(t *testing.T) {
		tr.reset()
		rec := serve(engine, getReq("/x"))
		if rec.Code != http.StatusNoContent || tr.String() != "engine,group,leaf" {
			t.Fatalf("status = %d, chain = %q", rec.Code, tr.String())
		}
	})

	// Unmatched paths are outside every group: only the engine's own
	// middleware sees them, and the answer is the standard 404 body.
	t.Run("UnmatchedPathRunsOnlyEngineMiddleware", func(t *testing.T) {
		tr.reset()
		rec := serve(engine, getReq("/nope"))
		if rec.Code != http.StatusNotFound || tr.String() != "engine" {
			t.Fatalf("status = %d, chain = %q", rec.Code, tr.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want the JSON error body", ct)
		}
	})

	t.Run("MethodNotAllowedListsMethodsSorted", func(t *testing.T) {
		tr.reset()
		rec := serve(engine, httptest.NewRequest(http.MethodPost, "http://example.com/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Fatalf("Allow = %q, want %q", allow, "GET, HEAD")
		}
		if tr.String() != "engine" {
			t.Fatalf("chain = %q, want only the engine layer", tr.String())
		}
	})
}

func TestServeHTTPResponseCommit(t *testing.T) {
	t.Run("StatusOnlyHandlerCommitsThatStatus", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/s", func(ctx httpx.Context) error {
			ctx.Status(http.StatusAccepted)
			return nil
		})
		rec := serve(engine, getReq("/s"))
		if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("SilentHandlerIs200", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/s", func(ctx httpx.Context) error { return nil })
		if rec := serve(engine, getReq("/s")); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("ErrorIsRenderedThroughErrorHandler", func(t *testing.T) {
		engine, r := newTestEngine(t, WithErrorHandler(func(ctx httpx.Context, err error) {
			_ = ctx.Text(http.StatusTeapot, err.Error())
		}))
		r.GET("/e", func(ctx httpx.Context) error { return errors.New("boom") })
		rec := serve(engine, getReq("/e"))
		if rec.Code != http.StatusTeapot || rec.Body.String() != "boom" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	// An error handler that records a status without writing a body owes a
	// response exactly like a handler does; it must not fall out as 200.
	t.Run("StatusOnlyErrorHandlerCommitsThatStatus", func(t *testing.T) {
		engine, r := newTestEngine(t, WithErrorHandler(func(ctx httpx.Context, err error) {
			ctx.SetHeader("X-Error", err.Error())
			ctx.Status(http.StatusBadGateway)
		}))
		r.GET("/e", func(ctx httpx.Context) error { return errors.New("upstream") })
		rec := serve(engine, getReq("/e"))
		if rec.Code != http.StatusBadGateway || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Error") != "upstream" {
			t.Fatalf("X-Error = %q", rec.Header().Get("X-Error"))
		}
	})

	// An error handler that renders nothing — one that only logs — leaves the
	// status to the adapter, and the error's own status is what that has to be,
	// not the 200 a response starts at: caches and monitoring believe a status.
	// The body stays empty, since inventing one overwrites the handler's decision.
	t.Run("SilentErrorHandlerCommitsTheErrorStatus", func(t *testing.T) {
		engine, r := newTestEngine(t, WithErrorHandler(func(ctx httpx.Context, err error) {}))
		r.GET("/e", func(ctx httpx.Context) error { return httpx.NewForbiddenError("ignored") })
		if rec := serve(engine, getReq("/e")); rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q, want 403 with an empty body", rec.Code, rec.Body.String())
		}
		// An error carrying no status of its own classifies to 500.
		r.GET("/u", func(ctx httpx.Context) error { return errors.New("ignored") })
		if rec := serve(engine, getReq("/u")); rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("DefaultErrorHandlerUsesHTTPXStatus", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/e", func(ctx httpx.Context) error { return httpx.NewForbiddenError("no") })
		rec := serve(engine, getReq("/e"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"no"`) {
			t.Fatalf("body = %q, want the message rendered", rec.Body.String())
		}
	})

	// A committed response is never overwritten by an error body, but the error
	// is not lost: the outer layer's next(ctx) still returns it.
	t.Run("ErrorAfterCommitPropagatesWithoutRendering", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var seen error
		r.Use(func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				seen = next(ctx)
				return seen
			}
		})
		r.GET("/late", func(ctx httpx.Context) error {
			if err := ctx.Text(http.StatusOK, "ok"); err != nil {
				return err
			}
			return errors.New("after write")
		})
		rec := serve(engine, getReq("/late"))
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("status = %d, body = %q; the committed response must stand", rec.Code, rec.Body.String())
		}
		if seen == nil || seen.Error() != "after write" {
			t.Fatalf("outer layer saw %v, want the handler's error", seen)
		}
	})
}

// The context is pooled: every field a request sets has to be reset before
// the next one, or state leaks between unrelated requests.
func TestPooledContextIsCleanBetweenRequests(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/users/:id", func(ctx httpx.Context) error {
		ctx.Set("who", ctx.Param("id"))
		ctx.Status(http.StatusAccepted)
		return ctx.Text(http.StatusAccepted, ctx.Query("q"))
	})
	var got struct {
		key      any
		present  bool
		param    string
		params   map[string]string
		fullPath string
		query    string
		status   int
	}
	r.GET("/plain", func(ctx httpx.Context) error {
		got.key, got.present = ctx.Get("who")
		got.param = ctx.Param("id")
		got.params = ctx.Params()
		got.fullPath = ctx.FullPath()
		got.query = ctx.Query("q")
		got.status = ctx.StatusCode()
		return nil
	})

	for range 3 {
		serve(engine, getReq("/users/42?q=first"))
		serve(engine, getReq("/plain"))
		if got.present || got.key != nil {
			t.Fatalf("state leaked: Get = (%v, %v)", got.key, got.present)
		}
		if got.param != "" || got.params != nil || got.fullPath != "/plain" {
			t.Fatalf("route leaked: param=%q params=%v fullPath=%q", got.param, got.params, got.fullPath)
		}
		if got.query != "" {
			t.Fatalf("query leaked: %q", got.query)
		}
		if got.status != http.StatusOK {
			t.Fatalf("status leaked: %d", got.status)
		}
	}
}

func TestDoServesInProcess(t *testing.T) {
	engine, r := newTestEngine(t)
	r.POST("/echo", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		return ctx.Bytes(http.StatusCreated, raw, "text/plain")
	})
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("engine does not implement httpx.TestRequester")
	}
	resp, err := requester.Do(bodyReq(http.MethodPost, "/echo", "text/plain", "ping"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated || string(body) != "ping" {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, body)
	}
}

func TestLifecycle(t *testing.T) {
	t.Run("StartAfterStopIsRefused", func(t *testing.T) {
		engine, _ := newTestEngine(t, WithAddr("127.0.0.1:0"))
		if err := engine.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if err := engine.Start(); !errors.Is(err, httpx.ErrEngineClosed) {
			t.Fatalf("Start after Stop = %v, want ErrEngineClosed", err)
		}
	})

	t.Run("StartBindsThenStopReturnsNil", func(t *testing.T) {
		engine, _ := newTestEngine(t, WithAddr("127.0.0.1:0"))
		if engine.IsRunning() {
			t.Fatal("IsRunning before Start")
		}
		errCh := make(chan error, 1)
		go func() { errCh <- engine.Start() }()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && !engine.IsRunning() {
			time.Sleep(5 * time.Millisecond)
		}
		if !engine.IsRunning() {
			t.Fatal("engine did not report running")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := engine.Stop(ctx); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Start returned %v after graceful Stop", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return after Stop")
		}
		if engine.IsRunning() {
			t.Fatal("IsRunning after Stop")
		}
	})

	t.Run("WithServerInstallsEngineAsHandler", func(t *testing.T) {
		server := &http.Server{Addr: ":0"}
		engine, _ := newTestEngine(t, WithServer(server))
		if server.Handler != engine {
			t.Fatal("engine was not installed as the server's Handler")
		}
	})
}

func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		proxies []string
		remote  string
		xff     string
		want    string
	}{
		{name: "NoProxiesIgnoresForwardedFor", remote: "10.0.0.1:1234", xff: "1.2.3.4", want: "10.0.0.1"},
		{name: "RemoteWithoutPort", remote: "10.0.0.1", want: "10.0.0.1"},
		{name: "IPv6Remote", remote: "[::1]:80", want: "::1"},
		{name: "UntrustedPeerIgnoresHeader", proxies: []string{"10.0.0.0/8"}, remote: "192.168.1.1:1", xff: "1.2.3.4", want: "192.168.1.1"},
		{name: "TrustedPeerTakesLastUntrustedHop", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: "1.2.3.4, 10.0.0.2", want: "1.2.3.4"},
		{name: "WalksPastSeveralTrustedHops", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: "8.8.8.8, 1.2.3.4, 10.0.0.2, 10.0.0.3", want: "1.2.3.4"},
		{name: "AllHopsTrustedReturnsLeftmost", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: "10.0.0.9", want: "10.0.0.9"},
		// The shape a same-host reverse proxy produces when the client itself
		// is on the LAN: Caddy writes the client into X-Forwarded-For and
		// connects from 127.0.0.1. The client must win over the peer.
		{name: "TrustedClientInTrustedRangeReturnsClient", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, remote: "127.0.0.1:1", xff: "192.168.1.50", want: "192.168.1.50"},
		{name: "AllHopsTrustedMultiHopReturnsLeftmost", proxies: []string{"127.0.0.0/8", "192.168.0.0/16"}, remote: "127.0.0.1:1", xff: "192.168.1.50, 192.168.1.60", want: "192.168.1.50"},
		{name: "AllHopsTrustedBlankLeftEdgeFallsBackToPeer", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: ", 10.0.0.9", want: "10.0.0.1"},
		{name: "EmptyHeaderFallsBackToPeer", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", want: "10.0.0.1"},
		{name: "MalformedHopEndsTheChain", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: "1.2.3.4, garbage, 10.0.0.2", want: "10.0.0.1"},
		{name: "BlankEntriesAreSkipped", proxies: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: "1.2.3.4, , 10.0.0.2", want: "1.2.3.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts []Option
			if tc.proxies != nil {
				opts = append(opts, WithTrustedProxies(tc.proxies...))
			}
			engine, r := newTestEngine(t, opts...)
			var got string
			r.GET("/ip", func(ctx httpx.Context) error {
				got = ctx.ClientIP()
				return nil
			})
			req := getReq("/ip")
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			serve(engine, req)
			if got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("InvalidProxyPanicsAtConstruction", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("WithTrustedProxies accepted an invalid entry")
			}
		}()
		New(WithTrustedProxies("not-an-ip-or-cidr"))
	})
}
