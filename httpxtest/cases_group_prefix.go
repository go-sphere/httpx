package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("GroupPrefix", casesGroupPrefix)
}

// A group prefix must not change which paths the routes under it answer. The
// four spellings — "", "/", "/api", "/api/" — reach the same scope; the two
// ending in a slash are the ones an adapter gets wrong, because joining prefix
// and route path by concatenation turns "/" + "/x" into "//x". The whole shape
// is asserted, not just reachability: the route answers at
// path.Join(prefix, routePath), BasePath() keeps a caller-written trailing
// slash and FullPath() joins BasePath() with the route path, so the two agree,
// and Handle mounts (Static, StaticFS, HandleStd) join the same way.
func casesGroupPrefix(t *testing.T, r runner) {
	// "/" and "/api/" are the shapes an adapter gets wrong; "" and "/api" keep a
	// fix from trading one shape for another.
	shapes := []string{"", "/", "/api", "/api/"}

	t.Run("EngineGroup", func(t *testing.T) {
		for _, prefix := range shapes {
			t.Run(nameOfPrefix(prefix), func(t *testing.T) {
				r.assertScopeReachable(t, joinPrefix("/", prefix), func(engine httpx.Engine) httpx.Router {
					return engine.Group(prefix)
				})
			})
		}
	})

	t.Run("RouterGroup", func(t *testing.T) {
		for _, prefix := range shapes {
			t.Run(nameOfPrefix(prefix), func(t *testing.T) {
				r.assertScopeReachable(t, joinPrefix("/", prefix), func(engine httpx.Engine) httpx.Router {
					return engine.Group("").Group(prefix)
				})
			})
		}
	})

	// Nesting is where the defect compounded: a trailing slash anywhere in the
	// chain of prefixes moved every route below it, so "/" + "/api" broke a group
	// whose own prefix was written the safe way.
	t.Run("Nested", func(t *testing.T) {
		for _, tc := range []struct{ outer, inner string }{
			{"/", "/"},
			{"/", "/api"},
			{"/api", "/"},
			{"/api/", "/v1/"},
			{"", "/"},
			{"/", ""},
			{"/api/", "/v1"},
		} {
			t.Run(nameOfPrefix(tc.outer)+"+"+nameOfPrefix(tc.inner), func(t *testing.T) {
				base := joinPrefix(joinPrefix("/", tc.outer), tc.inner)
				r.assertScopeReachable(t, base, func(engine httpx.Engine) httpx.Router {
					return engine.Group(tc.outer).Group(tc.inner)
				})
			})
		}
	})

	// A route path of "/" or "" is the other half of the same join: how an SPA's
	// root handler is registered, and where a prefix that already ends in a slash
	// meets it. The request goes to the *joined* path, not the base path: whether
	// the base path reaches it is a trailing-slash policy the adapters answer
	// differently on purpose — gin and hertz redirect 301, echox and stdx 404,
	// fiber matches — so asserting one would assert an ununified policy.
	t.Run("RootRoutePath", func(t *testing.T) {
		for _, prefix := range shapes {
			for _, routePath := range []string{"/", ""} {
				t.Run(nameOfPrefix(prefix)+"+"+nameOfPrefix(routePath), func(t *testing.T) {
					target := joinPrefix(joinPrefix("/", prefix), routePath)
					got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
						engine.Group(prefix).GET(routePath, func(ctx httpx.Context) error {
							return ctx.Text(http.StatusOK, "root")
						})
					}, httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil))

					if got.Status != http.StatusOK || got.Body != "root" {
						t.Fatalf("GET %s = %d %q, want 200 %q: Group(%q).GET(%q) registered a path this request cannot reach",
							target, got.Status, got.Body, "root", prefix, routePath)
					}
				})
			}
		}
	})

	// Static, StaticFS and HandleStd go through Handle, so they inherit the join
	// exactly as a handler route does.
	t.Run("Mounts", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-content"), 0o600); err != nil {
			t.Fatalf("write static file: %v", err)
		}

		for _, prefix := range shapes {
			t.Run(nameOfPrefix(prefix), func(t *testing.T) {
				base := joinPrefix("/", prefix)
				for _, tc := range []struct {
					name     string
					mount    func(httpx.Router)
					relative string
					want     string
				}{
					{
						name:     "Static",
						mount:    func(g httpx.Router) { g.Static("/assets", dir) },
						relative: "/assets/hello.txt",
						want:     "static-content",
					},
					{
						name:     "StaticFS",
						mount:    func(g httpx.Router) { g.StaticFS("/files", os.DirFS(dir)) },
						relative: "/files/hello.txt",
						want:     "static-content",
					},
					{
						name: "HandleStd",
						mount: func(g httpx.Router) {
							if !httpx.MountStd(g, http.MethodGet, "/mounted", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
								_, _ = w.Write([]byte("from-std"))
							})) {
								t.Skip("adapter does not implement httpx.StdHandlerMounter")
							}
						},
						relative: "/mounted",
						want:     "from-std",
					},
				} {
					t.Run(tc.name, func(t *testing.T) {
						target := path.Join(base, tc.relative)
						got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
							tc.mount(engine.Group(prefix))
						}, httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil))

						if got.Status != http.StatusOK || got.Body != tc.want {
							t.Fatalf("GET %s = %d %q, want 200 %q: the mount did not land under Group(%q)",
								target, got.Status, got.Body, tc.want, prefix)
						}
					})
				}
			})
		}
	})
}

// assertScopeReachable registers one route on the scope newScope builds, asks
// for it at the path the prefixes imply, and checks BasePath/FullPath agree.
func (r runner) assertScopeReachable(t *testing.T, wantBase string, newScope func(httpx.Engine) httpx.Router) {
	t.Helper()

	const relative = "/x"
	target := path.Join(wantBase, relative)
	wantFullPath := target

	var gotBase, gotFullPath string
	got := r.serveEngine(t, Options{}, func(engine httpx.Engine) {
		scope := newScope(engine)
		gotBase = scope.BasePath()
		scope.GET(relative, func(ctx httpx.Context) error {
			gotFullPath = ctx.FullPath()
			return ctx.Text(http.StatusOK, "reached")
		})
	}, httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil))

	if got.Status != http.StatusOK || got.Body != "reached" {
		t.Fatalf("GET %s = %d %q, want 200 %q: the route was registered somewhere this request cannot reach (BasePath()=%q)",
			target, got.Status, got.Body, "reached", gotBase)
	}
	if gotBase != wantBase {
		t.Fatalf("BasePath() = %q, want %q", gotBase, wantBase)
	}
	if gotFullPath != wantFullPath {
		t.Fatalf("FullPath() = %q, want %q: it disagrees with the path the route answers at", gotFullPath, wantFullPath)
	}
	// Stated separately because this is the relation callers rely on:
	// httpz.MatchOperation keys authorization off FullPath, URL building off
	// BasePath.
	if joined := path.Join(gotBase, relative); joined != gotFullPath {
		t.Fatalf("path.Join(BasePath()=%q, %q) = %q, but FullPath() = %q", gotBase, relative, joined, gotFullPath)
	}
}

// joinPrefix is the join every adapter's own joinPaths performs: path.Join,
// except a relative part ending in a slash keeps it, so BasePath() gives back
// what was written rather than a normalization of it.
func joinPrefix(base, relative string) string {
	if relative == "" {
		return base
	}
	joined := path.Join(base, relative)
	if strings.HasSuffix(relative, "/") && !strings.HasSuffix(joined, "/") {
		return joined + "/"
	}
	return joined
}

// nameOfPrefix keeps subtest names free of slashes, which would otherwise read
// as extra levels in -run patterns.
func nameOfPrefix(prefix string) string {
	switch prefix {
	case "":
		return "empty"
	case "/":
		return "slash"
	}
	name := strings.ReplaceAll(strings.Trim(prefix, "/"), "/", "-")
	if strings.HasSuffix(prefix, "/") {
		return name + "-slash"
	}
	return name
}
