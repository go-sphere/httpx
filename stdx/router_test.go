package stdx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-sphere/httpx"
)

func TestBasePathAndGroups(t *testing.T) {
	engine, root := newTestEngine(t)
	if root.BasePath() != "/" {
		t.Fatalf("root BasePath = %q", root.BasePath())
	}
	api := root.Group("/api")
	v1 := api.Group("v1/")
	if api.BasePath() != "/api" || v1.BasePath() != "/api/v1/" {
		t.Fatalf("BasePath = %q, %q", api.BasePath(), v1.BasePath())
	}
	var fullPath string
	v1.GET("users/:id", func(ctx httpx.Context) error {
		fullPath = ctx.FullPath()
		return nil
	})
	rec := serve(engine, getReq("/api/v1/users/1"))
	if rec.Code != http.StatusOK || fullPath != "/api/v1/users/:id" {
		t.Fatalf("status = %d, FullPath = %q", rec.Code, fullPath)
	}
}

func TestJoinPaths(t *testing.T) {
	for _, tc := range []struct{ base, rel, want string }{
		{"/", "", "/"},
		{"/", "/x", "/x"},
		{"/api", "", "/api"},
		{"/api", "users", "/api/users"},
		{"/api/", "/users/", "/api/users/"},
		{"/api", "/", "/api/"},
		{"/a/b", "../c", "/a/c"},
	} {
		if got := joinPaths(tc.base, tc.rel); got != tc.want {
			t.Errorf("joinPaths(%q, %q) = %q, want %q", tc.base, tc.rel, got, tc.want)
		}
	}
}

// Use is snapshotted into each route at registration: the same rule every
// framework-backed adapter has, pinned here because this router implements it
// by hand.
func TestUseSnapshotsAtRegistration(t *testing.T) {
	var tr trace
	engine, r := newTestEngine(t)
	r.GET("/early", tr.leaf("early"))
	r.Use(tr.mw("late-mw"))
	r.GET("/late", tr.leaf("late"))

	serve(engine, getReq("/early"))
	serve(engine, getReq("/late"))
	if tr.String() != "early,late-mw,late" {
		t.Fatalf("chain = %q", tr.String())
	}
}

func TestGroupInheritsParentMiddlewareAtCreation(t *testing.T) {
	var tr trace
	engine, r := newTestEngine(t)
	r.Use(tr.mw("parent-before"))
	child := r.Group("/c", tr.mw("child-arg"))
	r.Use(tr.mw("parent-after"))
	child.Use(tr.mw("child-use"))
	child.GET("/x", tr.leaf("leaf"))

	serve(engine, getReq("/c/x"))
	if tr.String() != "parent-before,child-arg,child-use,leaf" {
		t.Fatalf("chain = %q", tr.String())
	}
}

// Interceptors are composed into the leaf: they always run inside every Use
// layer of the scope and its parents, whatever the registration order, and
// among themselves parent-first in registration order.
func TestInterceptorOrdering(t *testing.T) {
	var tr trace
	engine, r := newTestEngine(t)
	r.UseInterceptor(tr.interceptor("i-root"))
	r.Use(tr.mw("m-root"))
	child := r.Group("/c", tr.mw("m-child-arg"))
	httpx.UseInterceptor(child, tr.interceptor("i-child"))
	child.Use(tr.mw("m-child"))
	child.GET("/x", tr.leaf("leaf"))
	// Registered after the route: must not affect it.
	httpx.UseInterceptor(child, tr.interceptor("i-late"))
	child.GET("/y", tr.leaf("leaf-y"))

	serve(engine, getReq("/c/x"))
	if tr.String() != "m-root,m-child-arg,m-child,i-root,i-child,leaf" {
		t.Fatalf("chain = %q", tr.String())
	}
	tr.reset()
	serve(engine, getReq("/c/y"))
	if tr.String() != "m-root,m-child-arg,m-child,i-root,i-child,i-late,leaf-y" {
		t.Fatalf("chain = %q", tr.String())
	}
}

func TestEngineInterceptorWrapsEveryRoute(t *testing.T) {
	var tr trace
	engine, ok := New().(*Engine)
	if !ok {
		t.Fatal("New did not return *Engine")
	}
	engine.UseInterceptor(tr.interceptor("engine-i"))
	engine.UseInterceptor() // a no-op call must not disturb the chain
	r := engine.Group("")
	r.GET("/x", tr.leaf("leaf"))
	serve(engine, getReq("/x"))
	if tr.String() != "engine-i,leaf" {
		t.Fatalf("chain = %q", tr.String())
	}
	// Unmatched paths are outside any interceptor.
	tr.reset()
	serve(engine, getReq("/nope"))
	if tr.String() != "" {
		t.Fatalf("interceptor ran for an unmatched path: %q", tr.String())
	}
}

func TestInterceptorErrorIsRenderedAtTheRoute(t *testing.T) {
	engine, r := newTestEngine(t)
	r.UseInterceptor(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return httpx.NewUnauthorizedError("nope") }
	})
	r.GET("/x", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "never") })
	rec := serve(engine, getReq("/x"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestMethods(t *testing.T) {
	engine, r := newTestEngine(t)
	ok := func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, ctx.Method()) }
	r.Any("/any", ok)
	r.Handle("get", "/lower", ok)
	r.Handle("PROPFIND", "/dav", ok)
	r.PUT("/verbs", ok)
	r.PATCH("/verbs", ok)
	r.DELETE("/verbs", ok)
	r.OPTIONS("/verbs", ok)

	t.Run("AnyCoversTheStandardSet", func(t *testing.T) {
		for _, method := range anyMethods {
			req := httptest.NewRequest(method, "http://example.com/any", nil)
			// CONNECT carries an authority, not a path, so httptest leaves
			// URL.Path empty; the router only ever sees the path.
			req.URL.Path = "/any"
			rec := serve(engine, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s /any = %d", method, rec.Code)
			}
		}
		if rec := serve(engine, httptest.NewRequest("PROPFIND", "http://example.com/any", nil)); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("PROPFIND /any = %d, want 405", rec.Code)
		}
	})

	t.Run("LowercaseRegistrationDispatchesUppercase", func(t *testing.T) {
		if rec := serve(engine, getReq("/lower")); rec.Code != http.StatusOK {
			t.Fatalf("GET /lower = %d", rec.Code)
		}
	})

	// A method outside the fixed array goes through the rare map, and still
	// shows up in Allow.
	t.Run("RareMethod", func(t *testing.T) {
		if rec := serve(engine, httptest.NewRequest("PROPFIND", "http://example.com/dav", nil)); rec.Code != http.StatusOK {
			t.Fatalf("PROPFIND /dav = %d", rec.Code)
		}
		rec := serve(engine, getReq("/dav"))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "PROPFIND" {
			t.Fatalf("GET /dav = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
		}
	})

	t.Run("VerbHelpers", func(t *testing.T) {
		for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			rec := serve(engine, httptest.NewRequest(method, "http://example.com/verbs", nil))
			if rec.Code != http.StatusOK || rec.Body.String() != method {
				t.Fatalf("%s /verbs = %d %q", method, rec.Code, rec.Body.String())
			}
		}
		rec := serve(engine, getReq("/verbs"))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "DELETE, OPTIONS, PATCH, PUT" {
			t.Fatalf("GET /verbs = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
		}
	})
}

func TestRegistrationPanics(t *testing.T) {
	mustPanic := func(t *testing.T, name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s did not panic", name)
			}
		}()
		fn()
	}
	_, r := newTestEngine(t)
	noop := func(ctx httpx.Context) error { return nil }
	mustPanic(t, "wildcard not last", func() { r.GET("/a/*rest/b", noop) })
	mustPanic(t, "two wildcards", func() { r.GET("/b/*x/*y", noop) })
	r.GET("/dup", noop)
	mustPanic(t, "duplicate route", func() { r.GET("/dup", noop) })
	r.GET("/p/:id", noop)
	mustPanic(t, "conflicting parameter name", func() { r.GET("/p/:name", noop) })
}

func TestHandleStdAndStatic(t *testing.T) {
	var tr trace
	assets := fstest.MapFS{
		"a.txt":            &fstest.MapFile{Data: []byte("alpha")},
		"dir/index.html":   &fstest.MapFile{Data: []byte("<h1>index</h1>")},
		"nodex/other.txt":  &fstest.MapFile{Data: []byte("x")},
		"sub/deep/b.txt":   &fstest.MapFile{Data: []byte("beta")},
		"unrelated/x.json": &fstest.MapFile{Data: []byte("{}")},
	}
	engine, r := newTestEngine(t)
	r.UseInterceptor(tr.interceptor("i"))
	r.HandleStd(http.MethodGet, "/std/:id", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("std " + req.URL.Path))
	}))
	if !httpx.MountStd(r, http.MethodPost, "/mounted", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("mounted"))
	})) {
		t.Fatal("MountStd reported no StdHandlerMounter")
	}
	r.StaticFS("/assets", assets)

	t.Run("StdHandlerRunsOnTheRealWriterInsideInterceptors", func(t *testing.T) {
		tr.reset()
		rec := serve(engine, getReq("/std/7"))
		if rec.Code != http.StatusAccepted || rec.Body.String() != "std /std/7" || tr.String() != "i" {
			t.Fatalf("status = %d, body = %q, chain = %q", rec.Code, rec.Body.String(), tr.String())
		}
		// A std handler that writes counts as a committed response.
		rec = serve(engine, httptest.NewRequest(http.MethodPost, "http://example.com/mounted", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "mounted" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("StaticServesFilesDirectoriesAndMisses", func(t *testing.T) {
		for _, tc := range []struct {
			method string
			path   string
			status int
			body   string
		}{
			{http.MethodGet, "/assets/a.txt", http.StatusOK, "alpha"},
			{http.MethodGet, "/assets/sub/deep/b.txt", http.StatusOK, "beta"},
			{http.MethodHead, "/assets/a.txt", http.StatusOK, ""},
			{http.MethodGet, "/assets/dir/", http.StatusOK, "<h1>index</h1>"},
			{http.MethodGet, "/assets/nodex/", http.StatusNotFound, ""},
			{http.MethodGet, "/assets/", http.StatusNotFound, ""},
			{http.MethodGet, "/assets", http.StatusNotFound, ""},
			{http.MethodGet, "/assets/missing.txt", http.StatusNotFound, ""},
			{http.MethodPost, "/assets/a.txt", http.StatusMethodNotAllowed, ""},
		} {
			tr.reset()
			rec := serve(engine, httptest.NewRequest(tc.method, "http://example.com"+tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("%s %s = %d, want %d (body %q)", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
			}
			if tc.body != "" && rec.Body.String() != tc.body {
				t.Fatalf("%s %s body = %q, want %q", tc.method, tc.path, rec.Body.String(), tc.body)
			}
			if tc.status == http.StatusOK && tr.String() != "i" {
				t.Fatalf("%s %s ran outside the interceptor chain: %q", tc.method, tc.path, tr.String())
			}
		}
	})

	t.Run("StaticRouteIsAWildcard", func(t *testing.T) {
		var fullPath, param string
		engine2, r2 := newTestEngine(t)
		r2.Use(func(ctx httpx.Context) error {
			fullPath, param = ctx.FullPath(), ctx.Param(httpx.WildcardParamName(ctx.FullPath()))
			return ctx.Next()
		})
		r2.StaticFS("/assets", assets)
		serve(engine2, getReq("/assets/sub/deep/b.txt"))
		if fullPath != httpx.StaticRoutePattern("/assets") || param != "sub/deep/b.txt" {
			t.Fatalf("FullPath = %q, param = %q", fullPath, param)
		}
	})

	t.Run("StaticFromDisk", func(t *testing.T) {
		dir := t.TempDir()
		if err := writeFile(dir, "on-disk.txt", "disk"); err != nil {
			t.Fatal(err)
		}
		engine3, r3 := newTestEngine(t)
		r3.Static("/files", dir)
		rec := serve(engine3, getReq("/files/on-disk.txt"))
		if rec.Code != http.StatusOK || rec.Body.String() != "disk" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})
}

func TestRouterFeatures(t *testing.T) {
	_, r := newTestEngine(t)
	if !r.SupportsRouterFeature(httpx.RouterFeatureNamedWildcard) {
		t.Fatal("named wildcards are matched natively and must be reported")
	}
	if r.SupportsRouterFeature(httpx.RouterFeature("something-else")) {
		t.Fatal("an unknown feature was reported as supported")
	}
	if fixed, param := httpx.FixWildcardPathIfNeed(r, "/files/*filepath"); fixed != "/files/*filepath" || param != "filepath" {
		t.Fatalf("FixWildcardPathIfNeed = (%q, %q); a natively matched pattern must be left alone", fixed, param)
	}
}
