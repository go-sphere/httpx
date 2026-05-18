package conformance

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func assertMatchesGin(t *testing.T, results map[string]responseSnapshot) {
	t.Helper()
	base, ok := results["ginx"]
	if !ok {
		t.Fatalf("missing ginx baseline")
	}
	for _, name := range []string{"fiberx", "echox", "hertzx"} {
		got := results[name]
		assertResponseLikeGin(t, name, base, got)
	}
}

func assertResponseLikeGin(t *testing.T, framework string, want responseSnapshot, got responseSnapshot) {
	t.Helper()

	if want.Status != got.Status {
		t.Fatalf("%s status mismatch: want %d, got %d", framework, want.Status, got.Status)
	}

	if isJSON(want.Headers.Get("Content-Type")) {
		assertJSONBodyEqual(t, framework, want.Body, got.Body)
	} else if (want.Status < 300 || want.Status >= 400 || want.Headers.Get("Location") == "") && want.Body != got.Body {
		t.Fatalf("%s body mismatch: want %q, got %q", framework, want.Body, got.Body)
	}

	if want.Status < 300 || want.Status >= 400 {
		compareContentType(t, framework, want.Headers.Get("Content-Type"), got.Headers.Get("Content-Type"))
	}
	compareHeaderIfPresent(t, framework, "Location", want.Headers, got.Headers)
	compareHeaderIfPresent(t, framework, "X-Trace", want.Headers, got.Headers)
	compareSetCookie(t, framework, want.Headers.Values("Set-Cookie"), got.Headers.Values("Set-Cookie"))
}

func compareHeaderIfPresent(t *testing.T, framework, key string, want, got http.Header) {
	t.Helper()
	w := want.Values(key)
	if len(w) == 0 {
		return
	}
	g := got.Values(key)
	if !reflect.DeepEqual(w, g) {
		t.Fatalf("%s header %s mismatch: want %v, got %v", framework, key, w, g)
	}
}

func compareContentType(t *testing.T, framework, want, got string) {
	t.Helper()
	if want == "" {
		return
	}
	wantMain := strings.TrimSpace(strings.Split(want, ";")[0])
	gotMain := strings.TrimSpace(strings.Split(got, ";")[0])
	if wantMain != gotMain {
		t.Fatalf("%s content-type mismatch: want %q, got %q", framework, wantMain, gotMain)
	}
}

func compareSetCookie(t *testing.T, framework string, want, got []string) {
	t.Helper()
	if len(want) == 0 {
		return
	}
	if len(got) < len(want) {
		t.Fatalf("%s set-cookie mismatch: want at least %d, got %d", framework, len(want), len(got))
	}
	gotCookies := parseSetCookies(t, framework, got)
	for _, raw := range want {
		w, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatalf("invalid gin set-cookie %q: %v", raw, err)
		}
		var g *http.Cookie
		for _, c := range gotCookies {
			if c.Name == w.Name {
				g = c
				break
			}
		}
		if g == nil {
			t.Fatalf("%s missing cookie %q in %v", framework, w.Name, got)
		}
		assertCookieEqual(t, framework, w, g)
	}
}

func parseSetCookies(t *testing.T, framework string, raw []string) []*http.Cookie {
	t.Helper()
	out := make([]*http.Cookie, 0, len(raw))
	for _, v := range raw {
		c, err := http.ParseSetCookie(v)
		if err != nil {
			t.Fatalf("%s invalid set-cookie %q: %v", framework, v, err)
		}
		out = append(out, c)
	}
	return out
}

func assertCookieEqual(t *testing.T, framework string, want, got *http.Cookie) {
	t.Helper()
	if want.Value != got.Value {
		t.Fatalf("%s cookie %q value mismatch: want %q, got %q", framework, want.Name, want.Value, got.Value)
	}
	if want.Path != got.Path {
		t.Fatalf("%s cookie %q path mismatch: want %q, got %q", framework, want.Name, want.Path, got.Path)
	}
	if want.Domain != got.Domain {
		t.Fatalf("%s cookie %q domain mismatch: want %q, got %q", framework, want.Name, want.Domain, got.Domain)
	}
	if want.MaxAge != got.MaxAge {
		t.Fatalf("%s cookie %q max-age mismatch: want %d, got %d", framework, want.Name, want.MaxAge, got.MaxAge)
	}
	if want.Secure != got.Secure {
		t.Fatalf("%s cookie %q secure mismatch: want %v, got %v", framework, want.Name, want.Secure, got.Secure)
	}
	if want.HttpOnly != got.HttpOnly {
		t.Fatalf("%s cookie %q httponly mismatch: want %v, got %v", framework, want.Name, want.HttpOnly, got.HttpOnly)
	}
	if want.SameSite != got.SameSite {
		t.Fatalf("%s cookie %q samesite mismatch: want %v, got %v", framework, want.Name, want.SameSite, got.SameSite)
	}
}

func isJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

func assertJSONBodyEqual(t *testing.T, framework, want, got string) {
	t.Helper()
	var wantObj any
	if err := json.Unmarshal([]byte(want), &wantObj); err != nil {
		t.Fatalf("invalid gin json body: %v; body=%q", err, want)
	}
	var gotObj any
	if err := json.Unmarshal([]byte(got), &gotObj); err != nil {
		t.Fatalf("%s invalid json body: %v; body=%q", framework, err, got)
	}
	if !reflect.DeepEqual(wantObj, gotObj) {
		t.Fatalf("%s json body mismatch: want %s, got %s", framework, want, got)
	}
}
