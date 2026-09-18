package httpxtest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Contract", casesContract)
}

// Contract details that are easy to get subtly different: the absence of
// validation, method name case, registration-time failures, sniffed content
// types, state-store semantics, and plain net/http middleware.
func casesContract(t *testing.T, r runner) {
	// Bind* decodes and does not validate: a `binding` tag is inert, so a
	// missing "required" field binds to its zero value with no 400. Pinned
	// rather than left untested — the adapters used to run go-playground's
	// validator here, and the whole point of removing it is that the caller,
	// not the binder, decides what a valid request is.
	t.Run("BindIgnoresBindingTag", func(t *testing.T) {
		type dto struct {
			Name string `json:"name" query:"name" binding:"required"`
		}
		register := func(router httpx.Router) {
			router.POST("/validate/json", func(ctx httpx.Context) error {
				var d dto
				if err := ctx.BindJSON(&d); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]string{"name": d.Name})
			})
			router.GET("/validate/query", func(ctx httpx.Context) error {
				var d dto
				if err := ctx.BindQuery(&d); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]string{"name": d.Name})
			})
		}
		jsonReq := func(body string) *http.Request {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/validate/json", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			return req
		}

		for _, tc := range []struct {
			name     string
			request  *http.Request
			wantName string
		}{
			{"JSONMissingRequired", jsonReq(`{}`), ""},
			{"JSONPresent", jsonReq(`{"name":"sphere"}`), "sphere"},
			{"QueryMissingRequired", httptest.NewRequest(http.MethodGet, "http://example.com/validate/query", nil), ""},
			{"QueryPresent", httptest.NewRequest(http.MethodGet, "http://example.com/validate/query?name=sphere", nil), "sphere"},
		} {
			got := r.serve(t, register, tc.request)
			if got.Status != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200; body=%q", tc.name, got.Status, got.Body)
			}
			var payload struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s: parse body: %v; body=%q", tc.name, err, got.Body)
			}
			if payload.Name != tc.wantName {
				t.Fatalf("%s: name = %q, want %q", tc.name, payload.Name, tc.wantName)
			}
		}
	})

	// The sequence generated handlers use: one struct filled from four sources
	// in turn. Every source has to survive the next Bind* call, and none of
	// them may fail because a *later* source has not been read yet — which is
	// exactly what validating after each decode did, since the `uri` field is
	// still empty when BindJSON returns.
	t.Run("BindMultiSourceSequence", func(t *testing.T) {
		type dto struct {
			Name   string `json:"name"`
			Token  string `header:"X-Token"`
			Active string `query:"active"`
			ID     string `uri:"id" binding:"required"`
		}
		register := func(router httpx.Router) {
			router.POST("/multi/:id", func(ctx httpx.Context) error {
				var d dto
				if err := ctx.BindJSON(&d); err != nil {
					return err
				}
				if err := ctx.BindHeader(&d); err != nil {
					return err
				}
				if err := ctx.BindQuery(&d); err != nil {
					return err
				}
				if err := ctx.BindURI(&d); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]string{
					"name": d.Name, "token": d.Token, "active": d.Active, "id": d.ID,
				})
			})
		}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/multi/42?active=yes", strings.NewReader(`{"name":"sphere"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Token", "t-1")

		got := r.serve(t, register, req)
		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		for key, want := range map[string]string{"name": "sphere", "token": "t-1", "active": "yes", "id": "42"} {
			if payload[key] != want {
				t.Fatalf("%s = %q, want %q; body=%q", key, payload[key], want, got.Body)
			}
		}
		r.compareGolden(t, got)
	})

	// Lowercase method names must register and dispatch.
	t.Run("MethodNameCase", func(t *testing.T) {
		methods := []string{"get", "post", "put", "delete", "patch", "options"}
		register := func(router httpx.Router) {
			for _, method := range methods {
				router.Handle(method, "/case/"+method, func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "ok")
				})
			}
		}
		for _, method := range methods {
			req := httptest.NewRequest(strings.ToUpper(method), "http://example.com/case/"+method, nil)
			if got := r.serve(t, register, req); got.Status != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", method, got.Status)
			}
		}
	})

	// Unsupported wildcard shapes must fail registration identically instead
	// of silently registering a different route.
	t.Run("InvalidWildcardRegistrationPanics", func(t *testing.T) {
		for _, path := range []string{"/a/*x/b/*y", "/foo*bar", "/a/*x/tail"} {
			t.Run(path, func(t *testing.T) {
				engine := r.suite.NewEngine(t, Options{})
				router := engine.Group("")
				defer func() {
					if recover() == nil {
						t.Fatalf("registering %q did not panic", path)
					}
				}()
				router.Handle("GET", path, func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "ok")
				})
			})
		}
	})

	// The anonymous wildcard is one of those shapes, and the one that used to
	// split the adapters 2-vs-3: gin and hertz panicked with their own message,
	// while echo, fiber and stdx registered the route and then disagreed about
	// whether the parameter was keyed "*" or "". So this case asserts more than
	// "it panicked" — the panic value has to be httpx's error, which is what
	// makes the five failures identical rather than merely simultaneous.
	t.Run("AnonymousWildcardRegistrationPanics", func(t *testing.T) {
		for _, path := range []string{"/*", "/files/*", "/a/b/*"} {
			t.Run(path, func(t *testing.T) {
				engine := r.suite.NewEngine(t, Options{})
				router := engine.Group("")
				defer func() {
					v := recover()
					if v == nil {
						t.Fatalf("registering %q did not panic", path)
					}
					err, ok := v.(error)
					if !ok {
						t.Fatalf("registering %q panicked with %T(%v), want an error", path, v, v)
					}
					if !strings.Contains(err.Error(), "httpx: wildcard must be named") {
						t.Fatalf("registering %q panicked with %q, want httpx's own wildcard error", path, err)
					}
				}()
				router.Handle("GET", path, func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "ok")
				})
			})
		}
	})

	// An empty contentType must be sniffed the same way everywhere.
	t.Run("BytesEmptyContentType", func(t *testing.T) {
		register := func(router httpx.Router) {
			router.GET("/bytes/sniff", func(ctx httpx.Context) error {
				return ctx.Bytes(http.StatusOK, []byte("plain text payload"), "")
			})
		}
		req := httptest.NewRequest(http.MethodGet, "http://example.com/bytes/sniff", nil)
		got := r.serve(t, register, req)
		if ct := got.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content-type = %q, want text/plain (sniffed)", ct)
		}
		r.compareGolden(t, got)
	})

	// The full Content-Type, charset case included, is part of the contract.
	t.Run("TextContentTypeExact", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/text/exact", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "hi")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/text/exact", nil))

		if ct := got.Headers.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Fatalf("content-type = %q, want %q exactly", ct, "text/plain; charset=utf-8")
		}
	})

	// A stored nil must be reported as absent.
	t.Run("StateStoreNilValue", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/state/nil", func(ctx httpx.Context) error {
				ctx.Set("k", nil)
				_, ok := ctx.Get("k")
				ctx.Set("v", "value")
				v, vok := ctx.Get("v")
				return ctx.JSON(http.StatusOK, map[string]any{
					"nilOK":   ok,
					"value":   v,
					"valueOK": vok,
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/state/nil", nil))

		var payload struct {
			NilOK   bool   `json:"nilOK"`
			Value   string `json:"value"`
			ValueOK bool   `json:"valueOK"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.NilOK {
			t.Fatal("a stored nil was reported as present")
		}
		if !payload.ValueOK || payload.Value != "value" {
			t.Fatalf("stored value lost: %+v", payload)
		}
	})

	casesStdMiddleware(t, r)
}

// Plain net/http middleware must behave the same through every adapter:
// request mutation (including context values), response wrapping, and
// short-circuiting.
func casesStdMiddleware(t *testing.T, r runner) {
	if r.suite.StdMiddleware == nil {
		t.Run("StdMiddleware", func(t *testing.T) {
			t.Skipf("%s: no StdMiddleware hook declared", r.suite.Name)
		})
		return
	}

	t.Run("StdMiddlewareRequestContextAndHeader", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Std", "1")
				next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), stdCtxKey{}, "from-std")))
			})
		}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(r.suite.StdMiddleware(mw))
			router.GET("/std/ctx", func(ctx httpx.Context) error {
				v, _ := ctx.Context().Value(stdCtxKey{}).(string)
				return ctx.JSON(http.StatusOK, map[string]string{"value": v})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/std/ctx", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d; body=%q", got.Status, got.Body)
		}
		if got.Headers.Get("X-Std") != "1" {
			t.Fatal("the std middleware's header did not reach the response")
		}
		if !strings.Contains(got.Body, "from-std") {
			t.Fatalf("the std middleware's context value was lost: body=%q", got.Body)
		}
	})

	t.Run("StdMiddlewareResponseWrapping", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Std", "wrapped")
				next.ServeHTTP(upperWriter{w}, req)
			})
		}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(r.suite.StdMiddleware(mw))
			router.GET("/std/wrap", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "hello")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/std/wrap", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d; body=%q", got.Status, got.Body)
		}
		if got.Body != "HELLO" {
			t.Fatalf("wrapped body = %q, want %q", got.Body, "HELLO")
		}
		if got.Headers.Get("X-Std") != "wrapped" {
			t.Fatal("the std middleware's header did not reach the response")
		}
	})

	t.Run("StdMiddlewareShortCircuit", func(t *testing.T) {
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("blocked"))
			})
		}
		got := r.serve(t, func(router httpx.Router) {
			router.Use(r.suite.StdMiddleware(mw))
			router.GET("/std/block", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Handler-Ran", "1")
				return ctx.Text(http.StatusOK, "handler")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/std/block", nil))

		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%q", got.Status, got.Body)
		}
		if got.Body != "blocked" {
			t.Fatalf("body = %q, want %q", got.Body, "blocked")
		}
		if got.Headers.Get("X-Handler-Ran") != "" {
			t.Fatal("the handler ran after the std middleware short-circuited")
		}
	})
}

type stdCtxKey struct{}

// upperWriter upper-cases the body, exercising the writer-wrapping path of
// AdaptStdMiddleware.
type upperWriter struct {
	http.ResponseWriter
}

func (w upperWriter) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(bytes.ToUpper(p))
}
