package httpxtest

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// The recorded contracts ship with the package so an adapter in another module
// (or another repository) compares against the same files.
//
//go:embed golden
var goldenFS embed.FS

// Update mode is an environment variable rather than a flag: this is a normal
// package, and registering a flag here would add it to every binary that
// imports httpxtest.
const updateGoldenEnv = "HTTPX_UPDATE_GOLDEN"

// responseContract is the part of a response that is contractual across
// adapters: status, the main part of Content-Type, the Location, X-Trace and
// Set-Cookie headers, and the body — JSON semantically, a redirect body not at
// all.
type responseContract struct {
	Status      int
	ContentType string
	Location    []string
	XTrace      []string
	Cookies     []*http.Cookie
	BodyMode    string
	Body        string
}

func contractOf(t *testing.T, resp response) responseContract {
	t.Helper()
	c := responseContract{
		Status:   resp.Status,
		Location: resp.Headers.Values("Location"),
		XTrace:   resp.Headers.Values("X-Trace"),
	}
	for _, raw := range resp.Headers.Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatalf("invalid Set-Cookie %q: %v", raw, err)
		}
		c.Cookies = append(c.Cookies, cookie)
	}
	sort.Slice(c.Cookies, func(i, j int) bool { return c.Cookies[i].Name < c.Cookies[j].Name })

	contentType := strings.TrimSpace(strings.Split(resp.Headers.Get("Content-Type"), ";")[0])
	isRedirect := resp.Status >= http.StatusMultipleChoices && resp.Status < http.StatusBadRequest && len(c.Location) > 0
	switch {
	case strings.Contains(strings.ToLower(contentType), "application/json"):
		c.BodyMode, c.Body = "json", canonicalJSON(t, resp.Body)
	case isRedirect:
		// Redirect bodies are framework decoration: gin writes an HTML link,
		// fiber writes nothing.
		c.BodyMode = "ignored"
	case resp.Body == "":
		c.BodyMode = "none"
	default:
		c.BodyMode, c.Body = "text", resp.Body
	}

	// Content-Type describes a body, so it is contractual exactly where the
	// body is. Hertz labels every bodyless response text/plain (native hertz
	// does the same) and the other three label none of them; on a redirect gin
	// labels its HTML body and fiber has none to label.
	if c.BodyMode == "json" || c.BodyMode == "text" {
		c.ContentType = contentType
	} else {
		c.ContentType = "n/a"
	}
	return c
}

func canonicalJSON(t *testing.T, body string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("invalid json body: %v; body=%q", err, body)
	}
	// Re-encoding sorts object keys, so formatting and key order stop being
	// part of the comparison while values stay exact.
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("cannot re-encode json body: %v", err)
	}
	return string(out)
}

func (c responseContract) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "status: %d\n", c.Status)
	contentType := c.ContentType
	if contentType == "" {
		contentType = "-"
	}
	fmt.Fprintf(&b, "content-type: %s\n", contentType)
	for _, v := range c.Location {
		fmt.Fprintf(&b, "location: %s\n", v)
	}
	for _, v := range c.XTrace {
		fmt.Fprintf(&b, "x-trace: %s\n", v)
	}
	for _, cookie := range c.Cookies {
		expires := "-"
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "cookie: name=%s value=%s path=%s domain=%s maxage=%d secure=%t httponly=%t samesite=%d expires=%s\n",
			cookie.Name, cookie.Value, cookie.Path, cookie.Domain, cookie.MaxAge,
			cookie.Secure, cookie.HttpOnly, cookie.SameSite, expires)
	}
	fmt.Fprintf(&b, "body: %s\n", c.BodyMode)
	if c.BodyMode == "json" || c.BodyMode == "text" {
		b.WriteString("---\n")
		b.WriteString(c.Body)
		if !strings.HasSuffix(c.Body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

var goldenNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// goldenName is the case path below the suite root, so the same file is used
// whichever adapter runs the case.
func (r runner) goldenName(t *testing.T) string {
	t.Helper()
	name := strings.TrimPrefix(t.Name(), r.root+"/")
	if name == t.Name() {
		t.Fatalf("case %q is not below the suite root %q", t.Name(), r.root)
	}
	return goldenNameUnsafe.ReplaceAllString(strings.ReplaceAll(name, "/", "__"), "_") + ".txt"
}

func (r runner) compareGolden(t *testing.T, resp response) {
	t.Helper()
	name := r.goldenName(t)
	got := contractOf(t, resp).render()

	if os.Getenv(updateGoldenEnv) == "1" {
		writeGolden(t, name, got)
		return
	}
	want, err := goldenFS.ReadFile(filepath.ToSlash(filepath.Join("golden", name)))
	if err != nil {
		t.Fatalf("%s: no golden contract for this case: %v\nrecord it from a source checkout with %s=1:\n%s",
			r.suite.Name, err, updateGoldenEnv, got)
	}
	if string(want) != got {
		t.Fatalf("%s: response contract changed\n%s", r.suite.Name, diffContracts("golden", string(want), r.suite.Name, got))
	}
}

// writeGolden writes into the package source directory, which only exists when
// the suite runs from a checkout. Running update mode against a module in the
// build cache is a mistake worth reporting rather than ignoring.
func writeGolden(t *testing.T, name, content string) {
	t.Helper()
	dir, ok := goldenSourceDir()
	if !ok {
		t.Fatalf("%s=1 needs a writable source checkout of httpxtest; none found at %s", updateGoldenEnv, dir)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}

func goldenSourceDir() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	dir := filepath.Join(filepath.Dir(file), "golden")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return dir, false
	}
	probe := filepath.Join(dir, ".writable")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		return dir, false
	}
	_ = os.Remove(probe)
	return dir, true
}

func diffContracts(wantName, want, gotName, got string) string {
	wantLines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	var b strings.Builder
	for i := 0; i < max(len(wantLines), len(gotLines)); i++ {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w == g {
			fmt.Fprintf(&b, "  %s\n", w)
			continue
		}
		if w != "" {
			fmt.Fprintf(&b, "- %s: %s\n", wantName, w)
		}
		if g != "" {
			fmt.Fprintf(&b, "+ %s: %s\n", gotName, g)
		}
	}
	return b.String()
}
