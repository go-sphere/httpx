package conformance

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

// Echo and fiber lack named wildcards: those adapters rewrite /*name and
// remember the name per engine, because the rewrite is lossy — every named
// wildcard in a scope collapses to one pattern. Cannot live in httpxtest: the
// shared suite builds one engine per case, which is what does not reproduce it.
func TestWildcardNamesAreScopedToOneEngine(t *testing.T) {
	// Both patterns normalize to the same thing on echox and fiberx.
	const (
		patternA = "/files/*filename"
		patternB = "/files/*path"
	)

	for _, suite := range append(httpxtestSuites(), fiberxOwnEngineSuite()) {
		t.Run(suite.Name, func(t *testing.T) {
			// This order makes the second engine the one a process-wide table would let win.
			engineA := newWildcardEngine(t, suite, "", patternA, "filename")
			engineB := newWildcardEngine(t, suite, "", patternB, "path")

			// A group shares its engine's table and must not be a third scope.
			engineC := newWildcardEngine(t, suite, "/api", patternB, "path")

			for _, tc := range []struct {
				name         string
				engine       httpx.Engine
				target       string
				wantPattern  string
				wantWildcard string
			}{
				{"engine A", engineA, "/files/a/b.txt", patternA, "a/b.txt"},
				{"engine B", engineB, "/files/c.txt", patternB, "c.txt"},
				{"engine C group", engineC, "/api/files/d.txt", "/api" + patternB, "d.txt"},
			} {
				got := serveWildcard(t, tc.engine, tc.target)
				if got.FullPath != tc.wantPattern {
					t.Errorf("%s: FullPath() = %q, want %q: another engine's registration answered for this one",
						tc.name, got.FullPath, tc.wantPattern)
				}
				if got.Param != tc.wantWildcard {
					t.Errorf("%s: Param() = %q, want %q", tc.name, got.Param, tc.wantWildcard)
				}
				if got.Params != tc.wantWildcard {
					t.Errorf("%s: Params()[name] = %q, want %q", tc.name, got.Params, tc.wantWildcard)
				}
				if got.Bound != tc.wantWildcard {
					t.Errorf("%s: BindURI uri:name = %q, want %q", tc.name, got.Bound, tc.wantWildcard)
				}
			}

			// Registering on A again after B must not disturb B: the tables are
			// separate in both directions, not merely ordered.
			engineA2 := newWildcardEngine(t, suite, "", patternA, "filename")
			if got := serveWildcard(t, engineB, "/files/c.txt"); got.FullPath != patternB {
				t.Errorf("engine B after a later registration elsewhere: FullPath() = %q, want %q",
					got.FullPath, patternB)
			}
			if got := serveWildcard(t, engineA2, "/files/e.txt"); got.FullPath != patternA {
				t.Errorf("engine A2: FullPath() = %q, want %q", got.FullPath, patternA)
			}
		})
	}
}

type wildcardReading struct {
	FullPath string `json:"fullPath"`
	Param    string `json:"param"`
	Params   string `json:"params"`
	Bound    string `json:"bound"`
}

// A tagged struct, not a map: gin's binder writes straight into a map it is
// handed, so a map destination is not portable. Generated code uses structs anyway.
var uriBinders = map[string]func(httpx.Context) (string, error){
	"filename": func(ctx httpx.Context) (string, error) {
		var dst struct {
			Value string `uri:"filename"`
		}
		err := ctx.BindURI(&dst)
		return dst.Value, err
	},
	"path": func(ctx httpx.Context) (string, error) {
		var dst struct {
			Value string `uri:"path"`
		}
		err := ctx.BindURI(&dst)
		return dst.Value, err
	},
}

func newWildcardEngine(t *testing.T, suite httpxtest.Suite, prefix, pattern, name string) httpx.Engine {
	t.Helper()
	bindURI, ok := uriBinders[name]
	if !ok {
		t.Fatalf("no uri binder for wildcard name %q", name)
	}
	engine := suite.NewEngine(t, httpxtest.Options{})
	engine.Group(prefix).GET(pattern, func(ctx httpx.Context) error {
		bound, err := bindURI(ctx)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, wildcardReading{
			FullPath: ctx.FullPath(),
			Param:    ctx.Param(name),
			Params:   ctx.Params()[name],
			Bound:    bound,
		})
	})
	return engine
}

func serveWildcard(t *testing.T, engine httpx.Engine, target string) wildcardReading {
	t.Helper()
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("engine does not support httpx.TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil))
	if err != nil {
		t.Fatalf("serve %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %q", target, resp.StatusCode, body)
	}
	var got wildcardReading
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("GET %s: parse %q: %v", target, body, err)
	}
	return got
}
