package httpxtest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Regression", casesRegression)
}

// Cases that exist because an adapter once got them wrong: each one pins a
// behavior that differed between frameworks and had to be normalized.
func casesRegression(t *testing.T, r runner) {
	// A generated route like /files/*filepath must register and resolve
	// everywhere, and the value must not carry a leading slash.
	t.Run("NamedWildcardValue", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Handle("GET", "/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"param":  ctx.Param("filepath"),
					"params": ctx.Params(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/files/a/b.txt", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			Param  string            `json:"param"`
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.Param != "a/b.txt" {
			t.Fatalf("Param(filepath) = %q, want %q", payload.Param, "a/b.txt")
		}
		if payload.Params["filepath"] != "a/b.txt" {
			t.Fatalf("Params()[filepath] = %q, want %q (params=%v)", payload.Params["filepath"], "a/b.txt", payload.Params)
		}
	})

	// FullPath is the pattern the caller registered. echo and fiber have no
	// named wildcards, so FixWildcardPathIfNeed rewrites "/files/*filepath" to
	// "/files/*" before handing it to them — an httpx-internal normalization
	// that used to leak out through this method. It is not cosmetic: downstream
	// gates authorization and rate limiting on FullPath, so a pattern that does
	// not match what was registered silently misses its policy.
	t.Run("FullPathIsTheRegisteredPattern", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			api := router.Group("/api")
			api.Handle("GET", "/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"fullPath": ctx.FullPath(),
					"param":    ctx.Param("filepath"),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/files/a/b.txt", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			FullPath string `json:"fullPath"`
			Param    string `json:"param"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.FullPath != "/api/files/*filepath" {
			t.Fatalf("FullPath() = %q, want %q", payload.FullPath, "/api/files/*filepath")
		}
		// The wildcard name still has to resolve: reporting the original
		// pattern must not come at the cost of the lookup that made it work.
		if payload.Param != "a/b.txt" {
			t.Fatalf("Param(filepath) = %q, want %q", payload.Param, "a/b.txt")
		}
	})

	// A route parameter arrives decoded everywhere. The escapes that matter are
	// the ones net/url keeps raw in URL.RawPath ("%2F", whose "/" is not escaped
	// on the way back), because that is where the frameworks stop agreeing:
	// fiber matches the raw path unless UnescapePath is on, and echo matches
	// RawPath whenever the standard library set it.
	t.Run("EncodedWildcardParamIsDecoded", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Handle("GET", "/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"param":  ctx.Param("filepath"),
					"params": ctx.Params(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/files/a/b%2Fc.txt", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			Param  string            `json:"param"`
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.Param != "a/b/c.txt" {
			t.Fatalf("Param(filepath) = %q, want %q", payload.Param, "a/b/c.txt")
		}
		if payload.Params["filepath"] != "a/b/c.txt" {
			t.Fatalf("Params()[filepath] = %q, want %q (params=%v)", payload.Params["filepath"], "a/b/c.txt", payload.Params)
		}
	})

	// The same value read through the binder. A named segment rather than a
	// wildcard, because gin matches on the decoded path and would not route
	// "%2F" to a single-segment parameter at all.
	t.Run("EncodedParamBindsDecoded", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Handle("GET", "/decode/:seg", func(ctx httpx.Context) error {
				var dst struct {
					Seg string `uri:"seg"`
				}
				if err := ctx.BindURI(&dst); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"param": ctx.Param("seg"),
					"bound": dst.Seg,
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/decode/a%2Cb", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			Param string `json:"param"`
			Bound string `json:"bound"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.Param != "a,b" {
			t.Fatalf("Param(seg) = %q, want %q", payload.Param, "a,b")
		}
		if payload.Bound != "a,b" {
			t.Fatalf("BindURI seg = %q, want %q: the binder and Param disagree", payload.Bound, "a,b")
		}
	})

	// BindURI and Param are two readings of the same route parameter, so a
	// field tagged uri:"x" must bind exactly what Param("x") returns — wildcards
	// included. That is the shape protoc-gen-sphere emits for "/v1/files/*path",
	// where the generated handler's only access to the path is ctx.BindURI, and
	// it is where three adapters disagreed in three different ways and silently:
	// ginx bound gin's raw wildcard value with the leading "/" that Param
	// strips, while echox and fiberx looked the uri tag up against the
	// framework's own parameter set, where FixWildcardPathIfNeed had left only
	// "*", found nothing, and bound "".
	//
	// The assertion is the invariant rather than a per-adapter literal, so an
	// adapter cannot satisfy it by being self-consistently wrong; want pins the
	// one value all five must agree on, so they cannot satisfy it by all
	// returning "".
	t.Run("BindURIMatchesParam", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			pattern string
			target  string
			want    string
		}{
			{"Wildcard", "/bind/files/*path", "/bind/files/a/b/c.txt", "a/b/c.txt"},
			{"WildcardSingleSegment", "/bind/files/*path", "/bind/files/c.txt", "c.txt"},
			// Encoded separator: net/url leaves "%2F" in RawPath, which is where
			// echo and fiber stop matching the decoded path.
			{"WildcardEncodedSlash", "/bind/files/*path", "/bind/files/a/b%2Fc.txt", "a/b/c.txt"},
			{"WildcardEncodedChar", "/bind/files/*path", "/bind/files/a%2Cb/c.txt", "a,b/c.txt"},
			// The non-wildcard form already agreed; it is here so a fix for the
			// wildcard cannot regress it.
			{"NamedSegment", "/bind/items/:path", "/bind/items/42", "42"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serve(t, func(router httpx.Router) {
					router.Handle("GET", tc.pattern, func(ctx httpx.Context) error {
						var in struct {
							Path string `uri:"path"`
						}
						if err := ctx.BindURI(&in); err != nil {
							return err
						}
						return ctx.JSON(http.StatusOK, map[string]any{
							"param":  ctx.Param("path"),
							"bound":  in.Path,
							"params": ctx.Params(),
						})
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))

				if got.Status != http.StatusOK {
					t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
				}
				var payload struct {
					Param  string            `json:"param"`
					Bound  string            `json:"bound"`
					Params map[string]string `json:"params"`
				}
				if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
					t.Fatalf("parse body: %v; body=%q", err, got.Body)
				}
				if payload.Bound != payload.Param {
					t.Fatalf("BindURI path = %q, Param(path) = %q: the binder and Param disagree", payload.Bound, payload.Param)
				}
				if payload.Params["path"] != payload.Param {
					t.Fatalf("Params()[path] = %q, Param(path) = %q: the map and Param disagree (params=%v)",
						payload.Params["path"], payload.Param, payload.Params)
				}
				if payload.Param != tc.want {
					t.Fatalf("Param(path) = %q, want %q", payload.Param, tc.want)
				}
			})
		}
	})

	// A root-level catch-all must also match the bare "/", which downstream
	// mounts (a std http.ServeMux serving a whole site) rely on.
	t.Run("RootCatchAllMatchesRoot", func(t *testing.T) {
		for _, reqPath := range []string{"/", "/a/b.txt"} {
			got := r.serve(t, func(router httpx.Router) {
				router.Handle("GET", "/*filepath", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "hit:"+ctx.Path())
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com"+reqPath, nil))

			if got.Status != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200; body=%q", reqPath, got.Status, got.Body)
			}
			if got.Body != "hit:"+reqPath {
				t.Fatalf("GET %s body = %q, want %q", reqPath, got.Body, "hit:"+reqPath)
			}
		}
	})

	// Headers() is the request's headers; Host travels separately and must not
	// appear among them.
	t.Run("HeadersExcludeHost", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/headers/nohost", nil)
		req.Header.Set("X-Trace-In", "t1")
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/headers/nohost", func(ctx httpx.Context) error {
				_, hasHost := ctx.Headers()["Host"]
				return ctx.JSON(http.StatusOK, map[string]any{
					"hasHost": hasHost,
					"trace":   ctx.Header("X-Trace-In"),
				})
			})
		}, req)
	})

	// Host travels outside the header map on net/http, so it is not a header
	// the request carries: Header("Host") is unset, exactly as Headers() has no
	// "Host" entry. fasthttp keeps it in the header set, which is where the two
	// fasthttp-backed adapters used to answer differently from the other three
	// — and differently from each other's own Headers().
	//
	// All three readings of the header are asserted together on purpose. This
	// case used to check Header and Headers only, and fiberx went a release with
	// BindHeader still filling a header:"Host" field: one adapter contradicting
	// itself about the same header, which is worse than the divergence the case
	// was written for. A binder is a reading of the header set like any other.
	t.Run("HeaderExcludesHost", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/headers/host", func(ctx httpx.Context) error {
				var bound struct {
					Host  string `header:"Host"`
					Trace string `header:"X-Trace-In"`
				}
				if err := ctx.BindHeader(&bound); err != nil {
					return err
				}
				_, inHeaders := ctx.Headers()["Host"]
				return ctx.JSON(http.StatusOK, map[string]any{
					"header":    ctx.Header("Host"),
					"lowercase": ctx.Header("host"),
					"inHeaders": inHeaders,
					"bound":     bound.Host,
					// An ordinary header still binds: removing Host must not
					// cost the rest of the header set.
					"boundTrace": bound.Trace,
					// And reading a header after the bind still works, which an
					// adapter that hides Host by editing the request has to
					// leave intact.
					"afterBind": ctx.Header("X-Trace-In"),
				})
			})
		}, func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "http://example.com/headers/host", nil)
			req.Header.Set("X-Trace-In", "t-1")
			return req
		}())

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			Header     string `json:"header"`
			Lowercase  string `json:"lowercase"`
			InHeaders  bool   `json:"inHeaders"`
			Bound      string `json:"bound"`
			BoundTrace string `json:"boundTrace"`
			AfterBind  string `json:"afterBind"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.Header != "" || payload.Lowercase != "" {
			t.Fatalf("Header(\"Host\") = %q, Header(\"host\") = %q, want both unset",
				payload.Header, payload.Lowercase)
		}
		if payload.InHeaders {
			t.Fatal("Headers() carries a Host entry")
		}
		if payload.Bound != "" {
			t.Fatalf("BindHeader filled header:\"Host\" with %q, want unset: it must read the same header set as Header and Headers",
				payload.Bound)
		}
		if payload.BoundTrace != "t-1" {
			t.Fatalf("BindHeader filled header:\"X-Trace-In\" with %q, want %q", payload.BoundTrace, "t-1")
		}
		if payload.AfterBind != "t-1" {
			t.Fatalf("Header(\"X-Trace-In\") = %q after BindHeader, want %q", payload.AfterBind, "t-1")
		}
	})

	// A cookie with Expires, a space in the value and every flag set must
	// serialize identically on every adapter.
	t.Run("SetCookieFullFidelity", func(t *testing.T) {
		expires := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.UTC)
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/cookie/full", func(ctx httpx.Context) error {
				ctx.SetCookie(&http.Cookie{
					Name:     "session",
					Value:    "a b",
					Path:     "/app",
					Expires:  expires,
					MaxAge:   600,
					Secure:   true,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				return ctx.JSON(http.StatusOK, map[string]any{"ok": true})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/cookie/full", nil))
	})

	// An unregistered route is 404, not a 500 from an unclassified framework
	// error.
	t.Run("NotFoundStatus", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/known", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))

		if got.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%q", got.Status, got.Body)
		}
	})

	// A handler that writes a response and then fails must not have the
	// response corrupted by a second error body.
	t.Run("ErrorAfterCommittedResponse", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/error", func(ctx httpx.Context) error {
				if err := ctx.JSON(http.StatusOK, map[string]any{"ok": true}); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/error", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("response is not a single JSON document: %v; body=%q", err, got.Body)
		}
		if payload["ok"] != true {
			t.Fatalf("body = %q, want the committed body", got.Body)
		}
	})

	// The same, one layer out: a middleware that fails after the handler
	// committed must not overwrite the response either.
	t.Run("MiddlewareErrorAfterCommittedResponse", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				if err := ctx.Next(); err != nil {
					return err
				}
				return errors.New("late middleware error")
			})
			router.GET("/committed/middleware", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/middleware", nil))

		if got.Status != http.StatusOK || got.Body != "ok" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "ok")
		}
	})

	// Not rendering an error over a committed response must not mean losing
	// it: an outer layer still has to see the failure, or a handler that fails
	// after writing its response becomes invisible to logging and metrics.
	// Every adapter keeps it on its own error path (gin/hertz append to the
	// native error list, echo returns it to echo, fiber parks it on the
	// context because returning it would let fiber render over the body).
	t.Run("OuterLayerSeesErrorAfterCommittedResponse", func(t *testing.T) {
		var seen error
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				seen = ctx.Next()
				// Swallow it: the point of the case is that it arrived, and
				// returning it would only re-enter the same path.
				return nil
			})
			router.GET("/committed/observed", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "ok"); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/observed", nil))

		if got.Status != http.StatusOK || got.Body != "ok" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "ok")
		}
		if seen == nil {
			t.Fatal("the middleware's Next returned nil: an error raised after the response was committed was dropped")
		}
		if !strings.Contains(seen.Error(), "late failure") {
			t.Fatalf("Next returned %v, want it to carry \"late failure\"", seen)
		}
	})

	// An invalid redirect code is a rendered error, not a panic (gin) and not
	// a silent rewrite to 302 (hertz).
	t.Run("InvalidRedirectCode", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/redirect/bad", func(ctx httpx.Context) error {
				return ctx.Redirect(999, "http://example.com/elsewhere")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/redirect/bad", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%q", got.Status, got.Body)
		}
		if loc := got.Headers.Get("Location"); loc != "" {
			t.Fatalf("Location = %q, want it unset for an invalid code", loc)
		}
	})

	// An unmarshalable value is an error through the error handler, not a
	// panic.
	t.Run("JSONMarshalError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/json/badvalue", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{"ch": make(chan int)})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/json/badvalue", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%q", got.Status, got.Body)
		}
	})

	// A URL that merely shares a string prefix with the static mount must not
	// serve files from it.
	t.Run("StaticPrefixBoundary", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-content"), 0o600); err != nil {
			t.Fatalf("write static file: %v", err)
		}
		register := func(router httpx.Router) { router.Static("/assets", dir) }

		ok := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/assets/hello.txt", nil))
		if ok.Status != http.StatusOK || ok.Body != "static-content" {
			t.Fatalf("sanity GET failed: status=%d body=%q", ok.Status, ok.Body)
		}
		leak := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/assetshello.txt", nil))
		if leak.Status == http.StatusOK && strings.Contains(leak.Body, "static-content") {
			t.Fatalf("prefix-adjacent URL leaked static content: %d %q", leak.Status, leak.Body)
		}
	})

	// Binder targets that are not a plain struct: a multi-level pointer and a
	// slice of pointers both have to decode the same way everywhere. There is
	// no validation left to assert here (Bind* decodes only — see
	// httpx.Binder), so the case pins the decoded values instead; the null
	// element stays because it is the one input that reaches ginx's recover,
	// gin's validator still being the thing that panics on it.
	t.Run("PointerAndSliceTargets", func(t *testing.T) {
		type item struct {
			Name string `json:"name"`
		}
		register := func(router httpx.Router) {
			router.POST("/bind/ptr", func(ctx httpx.Context) error {
				var dst *item
				if err := ctx.BindJSON(&dst); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{"name": dst.Name})
			})
			router.POST("/bind/slice", func(ctx httpx.Context) error {
				var dst []*item
				if err := ctx.BindJSON(&dst); err != nil {
					return err
				}
				names := make([]string, 0, len(dst))
				for _, it := range dst {
					if it == nil {
						names = append(names, "<nil>")
						continue
					}
					names = append(names, it.Name)
				}
				return ctx.JSON(http.StatusOK, map[string]any{"count": len(dst), "names": names})
			})
		}
		jsonReq := func(path, body string) *http.Request {
			req := httptest.NewRequest(http.MethodPost, "http://example.com"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			return req
		}

		for _, tc := range []struct {
			name, path, body, want string
		}{
			{"pointer target", "/bind/ptr", `{"name":"ok"}`, `{"name":"ok"}`},
			{"pointer target, absent field", "/bind/ptr", `{}`, `{"name":""}`},
			{"slice target", "/bind/slice", `[{"name":"a"},{"name":"b"}]`, `{"count":2,"names":["a","b"]}`},
			{"slice with an empty element", "/bind/slice", `[{"name":"a"},{}]`, `{"count":2,"names":["a",""]}`},
			{"slice with a null element", "/bind/slice", `[{"name":"a"},null]`, `{"count":2,"names":["a","<nil>"]}`},
		} {
			got := r.serve(t, register, jsonReq(tc.path, tc.body))
			if got.Status != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200; body=%q", tc.name, got.Status, got.Body)
			}
			var gotBody, wantBody any
			if err := json.Unmarshal([]byte(got.Body), &gotBody); err != nil {
				t.Fatalf("%s: parse body: %v; body=%q", tc.name, err, got.Body)
			}
			if err := json.Unmarshal([]byte(tc.want), &wantBody); err != nil {
				t.Fatalf("%s: parse want: %v", tc.name, err)
			}
			if !reflect.DeepEqual(gotBody, wantBody) {
				t.Fatalf("%s: body = %q, want %q", tc.name, got.Body, tc.want)
			}
		}
	})
}

// teapotErrorHandler is a custom handler that renders without aborting, which
// is how the cases check that the adapter — not the handler — is what stops
// the chain.
func teapotErrorHandler(ctx httpx.Context, err error) {
	_ = ctx.JSON(http.StatusTeapot, map[string]any{"error": err.Error()})
}

func init() {
	register("CommittedResponse", casesCommittedResponse)
}

// A response the handler already decided must never be replaced by an error
// body. The cases that wrote a body were already covered; these are the ones
// that decide a response *without* writing bytes, where an adapter that
// detects commitment by "has a body" gets it wrong.
func casesCommittedResponse(t *testing.T, r runner) {
	t.Run("NoContentThenError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/nocontent", func(ctx httpx.Context) error {
				if err := ctx.NoContent(http.StatusNoContent); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/nocontent", nil))

		if got.Status != http.StatusNoContent {
			t.Fatalf("status = %d, want %d: the error was rendered over a committed bodyless response; body=%q",
				got.Status, http.StatusNoContent, got.Body)
		}
		if got.Body != "" {
			t.Fatalf("body = %q, want it empty", got.Body)
		}
	})

	// The other direction: a bare Status(code) records a code without
	// producing a response, so a later error must still be rendered. Treating
	// it as committed would turn a failure into a silent 2xx with no body.
	t.Run("StatusAloneDoesNotSwallowError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/status", func(ctx httpx.Context) error {
				ctx.Status(http.StatusAccepted)
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/status", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d: a bare Status must not hide the error; body=%q",
				got.Status, http.StatusInternalServerError, got.Body)
		}
	})

	t.Run("RedirectThenError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/redirect", func(ctx httpx.Context) error {
				if err := ctx.Redirect(http.StatusFound, "/to"); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/redirect", nil))

		if got.Status != http.StatusFound {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusFound, got.Body)
		}
		if loc := got.Headers.Get("Location"); loc != "/to" {
			t.Fatalf("Location = %q, want %q", loc, "/to")
		}
	})

	// ServerSentEvents commits a 200 before the callback runs, so an error
	// from the callback cannot change the status any more. The error is still
	// recorded on the framework's error path where the adapter has one; what
	// must not happen is an error body replacing the stream.
	t.Run("StreamErrorBeforeFirstEvent", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/stream", func(ctx httpx.Context) error {
				return httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
					return errors.New("failed before the first event")
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200: the stream had already committed it; body=%q", got.Status, got.Body)
		}
		if strings.Contains(got.Body, "failed before the first event") {
			t.Fatalf("the error body replaced the stream: %q", got.Body)
		}
	})

	// A stream decides the response even on adapters whose callback runs after
	// the handler returns (fiber), so a failure raised *after* Stream must not
	// be rendered over it. Detecting this by "has a body" fails: the body is
	// still empty when the handler returns.
	t.Run("StreamThenError", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.GET("/committed/stream-then-error", func(ctx httpx.Context) error {
				streamer, ok := httpx.AsStreamer(ctx)
				if !ok {
					return errors.New("adapter does not implement httpx.Streamer")
				}
				if err := streamer.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
					_, err := w.Write([]byte("streamed-body"))
					return err
				}); err != nil {
					return err
				}
				return errors.New("failure after the stream was handed over")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream-then-error", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200: the error was rendered over a stream; body=%q", got.Status, got.Body)
		}
		if got.Body != "streamed-body" {
			t.Fatalf("body = %q, want %q: the stream was replaced", got.Body, "streamed-body")
		}
	})

	// The same failure one layer out: a middleware that fails after the
	// handler handed over the stream.
	t.Run("MiddlewareErrorAfterStream", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				if err := ctx.Next(); err != nil {
					return err
				}
				return errors.New("late middleware failure")
			})
			router.GET("/committed/stream-then-middleware", func(ctx httpx.Context) error {
				streamer, ok := httpx.AsStreamer(ctx)
				if !ok {
					return errors.New("adapter does not implement httpx.Streamer")
				}
				return streamer.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
					_, err := w.Write([]byte("streamed-body"))
					return err
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream-then-middleware", nil))

		if got.Status != http.StatusOK || got.Body != "streamed-body" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "streamed-body")
		}
	})
}
