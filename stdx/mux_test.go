package stdx

import (
	"net/http"
	"strings"
	"testing"
)

// The route tree is this adapter's only hand-written routing, so its rules are
// pinned directly: what matches, what backtracks, what is a 405 instead of a
// 404, and which ServeMux conveniences this tree deliberately does not have.
func TestTreeMatch(t *testing.T) {
	patterns := []struct{ method, pattern string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/users"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users/new"},
		{http.MethodGet, "/users/:id"},
		{http.MethodGet, "/users/:id/posts/:post"},
		{http.MethodGet, "/a/:x/c"},
		{http.MethodGet, "/a/*rest"},
		{http.MethodGet, "/assets/*filepath"},
		{http.MethodHead, "/assets/*filepath"},
		{http.MethodGet, "/trailing/"},
	}
	root := &node{}
	for _, p := range patterns {
		root.add(p.method, p.pattern, &route{pattern: p.pattern})
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   string // matched pattern, "" when nothing matched
		params string // "name=value,..." in match order
		allow  string // expected Allow list when nothing matched
	}{
		{name: "Root", method: http.MethodGet, path: "/", want: "/"},
		{name: "Static", method: http.MethodGet, path: "/users", want: "/users"},
		{name: "StaticBeatsParam", method: http.MethodGet, path: "/users/new", want: "/users/new"},
		{name: "Param", method: http.MethodGet, path: "/users/42", want: "/users/:id", params: "id=42"},
		{name: "TwoParams", method: http.MethodGet, path: "/users/42/posts/7", want: "/users/:id/posts/:post", params: "id=42,post=7"},
		// The param branch matches /a/b but dies at the missing "c", so the
		// walk has to come back and try the wildcard.
		{name: "BacktrackToWildcard", method: http.MethodGet, path: "/a/b", want: "/a/*rest", params: "rest=b"},
		{name: "ParamBranchWins", method: http.MethodGet, path: "/a/b/c", want: "/a/:x/c", params: "x=b"},
		{name: "WildcardTakesRest", method: http.MethodGet, path: "/a/b/c/d", want: "/a/*rest", params: "rest=b/c/d"},
		{name: "WildcardFile", method: http.MethodGet, path: "/assets/css/app.css", want: "/assets/*filepath", params: "filepath=css/app.css"},
		// A directory request has to reach the static handler, which is what
		// turns it into 404 or index.html — not a router miss.
		{name: "WildcardDirectory", method: http.MethodGet, path: "/assets/", want: "/assets/*filepath", params: "filepath="},
		// A bare prefix is not the wildcard's: every framework leaves
		// /assets to whatever else matches it, or to 404.
		{name: "WildcardBarePrefixIsNotMatched", method: http.MethodGet, path: "/assets", want: ""},
		// Prefix-adjacent URLs must not leak into the static mount.
		{name: "PrefixBoundary", method: http.MethodGet, path: "/assetshello.txt", want: ""},
		// No trailing-slash redirect in either direction, unlike ServeMux.
		{name: "TrailingSlashIsItsOwnRoute", method: http.MethodGet, path: "/trailing/", want: "/trailing/"},
		{name: "TrailingSlashNotRedirected", method: http.MethodGet, path: "/trailing", want: ""},
		{name: "NoPathCleaning", method: http.MethodGet, path: "/users//42", want: ""},
		{name: "CaseSensitive", method: http.MethodGet, path: "/USERS", want: ""},
		{name: "MethodNotAllowed", method: http.MethodDelete, path: "/users", want: "", allow: "GET,POST"},
		{name: "MethodNotAllowedOnWildcard", method: http.MethodPost, path: "/assets/app.css", want: "", allow: "GET,HEAD"},
		{name: "Unmatched", method: http.MethodGet, path: "/nope", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf [8]string
			values := buf[:0]
			r, allow := root.match(tc.method, tc.path, &values)

			if tc.want == "" {
				if r != nil {
					t.Fatalf("matched %q, want no match", r.pattern)
				}
				if got := joinSorted(allow); got != tc.allow {
					t.Fatalf("allow = %q, want %q", got, tc.allow)
				}
				return
			}
			if r == nil {
				t.Fatalf("no match, want %q (allow=%v)", tc.want, allow)
			}
			if r.pattern != tc.want {
				t.Fatalf("matched %q, want %q", r.pattern, tc.want)
			}
			if got := joinParams(r.params, values); got != tc.params {
				t.Fatalf("params = %q, want %q", got, tc.params)
			}
		})
	}
}

// Capturing into the caller's buffer is what keeps matching allocation-free;
// a regression here shows up as allocations on every parameterized request.
func TestTreeMatchDoesNotAllocate(t *testing.T) {
	root := &node{}
	root.add(http.MethodGet, "/users/:id/posts/:post", &route{pattern: "/users/:id/posts/:post"})
	root.add(http.MethodGet, "/assets/*filepath", &route{pattern: "/assets/*filepath"})

	// The buffer lives outside the measured closure, as it does in production:
	// inside the request context, not on match's caller stack.
	var buf [8]string
	if n := testing.AllocsPerRun(100, func() {
		values := buf[:0]
		if r, _ := root.match(http.MethodGet, "/users/1/posts/2", &values); r == nil {
			t.Fatal("no match")
		}
		values = buf[:0]
		if r, _ := root.match(http.MethodGet, "/assets/a/b.css", &values); r == nil {
			t.Fatal("no match")
		}
	}); n != 0 {
		t.Fatalf("allocations per match round = %v, want 0", n)
	}
}

func TestTreeRejectsDuplicateRoute(t *testing.T) {
	root := &node{}
	root.add(http.MethodGet, "/dup", &route{pattern: "/dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("registering the same method and path twice did not panic")
		}
	}()
	root.add(http.MethodGet, "/dup", &route{pattern: "/dup"})
}

func TestTreeRejectsConflictingParamNames(t *testing.T) {
	root := &node{}
	root.add(http.MethodGet, "/users/:id", &route{pattern: "/users/:id"})
	defer func() {
		if recover() == nil {
			t.Fatal("two names for the same parameter position did not panic")
		}
	}()
	root.add(http.MethodGet, "/users/:name", &route{pattern: "/users/:name"})
}

func joinParams(names, values []string) string {
	parts := make([]string, 0, len(names))
	for i, name := range names {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, ",")
}

func joinSorted(list []string) string {
	sorted := append([]string(nil), list...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return strings.Join(sorted, ",")
}
