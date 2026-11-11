package httpxtest

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The recorded contracts ship with the package so an adapter in another module
// (or another repository) compares against the same files.
//
//go:embed golden
var goldenFS embed.FS

// Update mode is an environment variable rather than a flag: registering a flag
// here would add it to every binary that imports httpxtest.
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
	// body is: hertz labels every bodyless response text/plain and the other
	// three label none, and on a redirect gin labels its HTML body while fiber
	// has none to label.
	if c.BodyMode == "json" || c.BodyMode == "text" {
		c.ContentType = contentType
	} else {
		c.ContentType = "n/a"
	}
	// Keep large JSON contracts reviewable without dropping their coverage.
	if c.BodyMode == "json" && len(c.Body) > 16<<10 {
		c.BodyMode = "json-sha256"
		c.Body = fmt.Sprintf("%x", sha256.Sum256([]byte(c.Body)))
	}
	return c
}

func canonicalJSON(t *testing.T, body string) string {
	t.Helper()
	var v any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		t.Fatalf("invalid json body: %v; body=%q", err, body)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected exactly one JSON value, got trailing data: %q", body)
	}
	// Re-encoding sorts object keys, so formatting and key order stop being part
	// of the comparison while values stay exact.
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
	if c.BodyMode == "text" {
		fmt.Fprintf(&b, "body-bytes: %d\n", len(c.Body))
	}
	if c.BodyMode == "json" || c.BodyMode == "json-sha256" || c.BodyMode == "text" {
		b.WriteString("---\n")
		b.WriteString(c.Body)
		if !strings.HasSuffix(c.Body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

var goldenNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// goldenStem is the case path below the suite root, so the same file is used
// whichever adapter runs the case. A case's golden file is its stem plus
// ".txt", and the cases nested under it have stems beginning stem+"__".
func (r runner) goldenStem(t *testing.T) string {
	t.Helper()
	name := strings.TrimPrefix(t.Name(), r.root+"/")
	if name == t.Name() {
		t.Fatalf("case %q is not below the suite root %q", t.Name(), r.root)
	}
	return goldenNameUnsafe.ReplaceAllString(strings.ReplaceAll(name, "/", "__"), "_")
}

func (r runner) compareGolden(t *testing.T, resp response) {
	t.Helper()
	name := r.goldenStem(t) + ".txt"
	r.ledger.use(name)
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
// the suite runs from a checkout; update mode against a module in the build
// cache is a mistake worth reporting rather than ignoring.
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

// goldenLedger records, for one Run, the golden files the cases compared and
// the stems of the cases that skipped. It is guarded so a case may call
// t.Parallel.
type goldenLedger struct {
	mu      sync.Mutex
	used    map[string]bool
	skipped []string
}

func (l *goldenLedger) use(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.used[name] = true
}

func (l *goldenLedger) skip(stem string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.skipped = append(l.skipped, stem)
}

// orphans returns the golden files in names that no case compared and no
// skipped case covers, sorted.
func (l *goldenLedger) orphans(names []string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, name := range names {
		if l.used[name] || l.coveredBySkip(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (l *goldenLedger) coveredBySkip(name string) bool {
	stem := strings.TrimSuffix(name, ".txt")
	for _, skipped := range l.skipped {
		if stem == skipped || strings.HasPrefix(stem, skipped+"__") {
			return true
		}
	}
	return false
}

// checkOrphans fails t for every golden file in fsys that this run neither
// compared nor skipped past. README.md documents the directory and is no
// contract.
func (l *goldenLedger) checkOrphans(t *testing.T, fsys fs.FS) {
	t.Helper()
	entries, err := fs.ReadDir(fsys, "golden")
	if err != nil {
		t.Fatalf("read embedded golden directory: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "README.md" {
			continue
		}
		names = append(names, e.Name())
	}
	if orphans := l.orphans(names); len(orphans) > 0 {
		t.Fatalf("golden files no case produces; delete them from httpxtest/golden:\n  %s",
			strings.Join(orphans, "\n  "))
	}
}

// partialRun reports whether -run or -skip may have filtered out cases, in
// which case a golden file the run did not reach proves nothing. A -run
// pattern without a slash selects top-level tests only, so every case below
// the one calling Run still runs.
func partialRun() (string, bool) {
	if f := flag.Lookup("test.run"); f != nil && strings.Contains(f.Value.String(), "/") {
		return "-run selects individual cases; the orphan check needs them all", true
	}
	if f := flag.Lookup("test.skip"); f != nil && f.Value.String() != "" {
		return "-skip may exclude cases; the orphan check needs them all", true
	}
	return "", false
}
