package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// TestStaticFileHandlerPaths pins what a static mount answers for the paths the
// conformance suite does not reach: escapes out of the mounted tree, the bare
// prefix, and the redirects net/http performs inside the mount. They are the
// cases that decide whether serving through FileServer is safe, so they are
// asserted on the shared handler rather than once per adapter.
func TestStaticFileHandlerPaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.css"), "css")
	if err := os.Mkdir(filepath.Join(dir, "spa"), 0o750); err != nil {
		t.Fatalf("mkdir spa: %v", err)
	}
	writeFile(t, filepath.Join(dir, "spa", "index.html"), "<h1>spa</h1>")
	if err := os.Mkdir(filepath.Join(dir, "plain"), 0o750); err != nil {
		t.Fatalf("mkdir plain: %v", err)
	}
	writeFile(t, filepath.Join(dir, "plain", "listed.txt"), "listed")
	// Outside the mounted tree: nothing below may reach it.
	writeFile(t, filepath.Join(filepath.Dir(dir), "SECRET.txt"), "top-secret")

	handler := httpx.StaticFileHandler("/assets", os.DirFS(dir))

	cases := []struct {
		name     string
		path     string
		status   int
		body     string
		location string
	}{
		{name: "File", path: "/assets/main.css", status: http.StatusOK, body: "css"},
		{name: "DotSegmentFile", path: "/assets/./main.css", status: http.StatusOK, body: "css"},
		{name: "DoubleSlashFile", path: "/assets//main.css", status: http.StatusOK, body: "css"},
		{name: "DirectoryIndex", path: "/assets/spa/", status: http.StatusOK, body: "<h1>spa</h1>"},

		// net/http's own redirects inside a static mount, kept so a single-page
		// app mounted on a prefix behaves the way it does under any file server.
		{name: "DirectoryRedirectsToSlash", path: "/assets/spa", status: http.StatusMovedPermanently, location: "spa/"},
		{name: "IndexRedirectsToDirectory", path: "/assets/spa/index.html", status: http.StatusMovedPermanently, location: "./"},

		// A directory with no index.html is 404, never a listing.
		{name: "DirectoryWithoutIndex", path: "/assets/plain/", status: http.StatusNotFound},
		{name: "BarePrefix", path: "/assets", status: http.StatusNotFound},
		{name: "BarePrefixSlash", path: "/assets/", status: http.StatusNotFound},

		{name: "Missing", path: "/assets/nope.css", status: http.StatusNotFound},
		{name: "Traversal", path: "/assets/../SECRET.txt", status: http.StatusNotFound},
		{name: "EncodedTraversal", path: "/assets/..%2fSECRET.txt", status: http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.body != "" && rec.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.body)
			}
			if got := rec.Header().Get("Location"); got != tc.location {
				t.Fatalf("Location = %q, want %q", got, tc.location)
			}
			if body := rec.Body.String(); tc.status == http.StatusNotFound {
				// Neither the directory's contents nor anything above the mount
				// may show up in a refusal.
				for _, leak := range []string{"listed.txt", "top-secret", "main.css"} {
					if strings.Contains(body, leak) {
						t.Fatalf("404 body leaked %q: %q", leak, body)
					}
				}
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
