package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckServeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckServeFile(file); err != nil {
		t.Fatalf("regular file: %v", err)
	}
	for name, path := range map[string]string{
		"directory": dir,
		"missing":   filepath.Join(dir, "missing.txt"),
	} {
		code, status, message := ParseError(CheckServeFile(path))
		if status != http.StatusNotFound || code != 0 || message != "" {
			t.Fatalf("%s: code=%d status=%d message=%q, want 404 with no user message", name, code, status, message)
		}
	}
}

func TestServeFileIgnoresRequestPath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte("page"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/site/index.html", "/a/../page.html"} {
		rec := httptest.NewRecorder()
		if err := ServeFile(rec, httptest.NewRequest(http.MethodGet, target, nil), path); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if rec.Code != http.StatusOK || rec.Body.String() != "page" {
			t.Fatalf("%s: status=%d body=%q, want 200 %q", target, rec.Code, rec.Body.String(), "page")
		}
	}
	rec := httptest.NewRecorder()
	if _, status, _ := ParseError(ServeFile(rec, httptest.NewRequest(http.MethodGet, "/", nil), t.TempDir())); status != http.StatusNotFound || rec.Body.Len() != 0 {
		t.Fatalf("directory: status=%d body=%q, want 404 and nothing written", status, rec.Body.String())
	}
}
