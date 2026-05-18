package hertzx

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func newStaticServer(t *testing.T, files fs.FS) *server.Hertz {
	t.Helper()
	h := server.Default(
		server.WithHostPorts("127.0.0.1:0"),
		server.WithDisablePrintRoute(true),
	)
	engine := New(WithEngine(h))
	engine.Group("").StaticFS("/assets", files)
	return h
}

type staticResp struct {
	status      int
	contentType string
	body        []byte
}

func doStatic(t *testing.T, h *server.Hertz, method, target string) staticResp {
	t.Helper()
	hctx := h.NewContext()
	hctx.Request.Header.SetMethod(method)
	hctx.Request.SetRequestURI("http://example.com" + target)
	h.ServeHTTP(context.Background(), hctx)
	return staticResp{
		status:      hctx.Response.StatusCode(),
		contentType: string(hctx.Response.Header.ContentType()),
		body:        append([]byte(nil), hctx.Response.Body()...),
	}
}

func TestStaticHandler(t *testing.T) {
	files := fstest.MapFS{
		"hello.txt":          {Data: []byte("hello world")},
		"nested/deep.txt":    {Data: []byte("deep")},
		"my..file.txt":       {Data: []byte("dotted")},
		"a..b.tar.gz":        {Data: []byte("archive")},
		"unknown.weirdxyz":   {Data: []byte("blob")},
		"sub/index.html":     {Data: []byte("<h1>hi</h1>")},
	}
	h := newStaticServer(t, files)

	t.Run("normal file", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/hello.txt")
		if r.status != http.StatusOK {
			t.Fatalf("status = %d, want 200", r.status)
		}
		if !bytes.Equal(r.body, []byte("hello world")) {
			t.Fatalf("body = %q, want %q", r.body, "hello world")
		}
		if r.contentType == "" {
			t.Fatalf("content-type empty")
		}
	})

	t.Run("nested file", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/nested/deep.txt")
		if r.status != http.StatusOK || string(r.body) != "deep" {
			t.Fatalf("got status=%d body=%q", r.status, r.body)
		}
	})

	t.Run("dotted filename is allowed", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/my..file.txt")
		if r.status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (regression: .. in filename must not 404)", r.status)
		}
		if string(r.body) != "dotted" {
			t.Fatalf("body = %q, want %q", r.body, "dotted")
		}
	})

	t.Run("multi-dot archive name is allowed", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/a..b.tar.gz")
		if r.status != http.StatusOK || string(r.body) != "archive" {
			t.Fatalf("got status=%d body=%q", r.status, r.body)
		}
	})

	t.Run("missing file is 404", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/nope.txt")
		if r.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", r.status)
		}
	})

	t.Run("directory is 404", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/nested")
		if r.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for directory", r.status)
		}
	})

	t.Run("empty filepath is 404", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/")
		if r.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for empty filepath", r.status)
		}
	})

	t.Run("nested traversal stays in root", func(t *testing.T) {
		// Hertz's router normalizes URLs before matching, but path.Clean inside the
		// handler is defense-in-depth for any `..` that survives in the wildcard tail.
		r := doStatic(t, h, http.MethodGet, "/assets/nested/../hello.txt")
		if r.status != http.StatusOK || string(r.body) != "hello world" {
			t.Fatalf("got status=%d body=%q", r.status, r.body)
		}
	})

	t.Run("unknown extension falls back to octet-stream", func(t *testing.T) {
		r := doStatic(t, h, http.MethodGet, "/assets/unknown.weirdxyz")
		if r.status != http.StatusOK {
			t.Fatalf("status = %d, want 200", r.status)
		}
		if r.contentType != "application/octet-stream" {
			t.Fatalf("content-type = %q, want application/octet-stream", r.contentType)
		}
	})

	t.Run("HEAD returns headers without body", func(t *testing.T) {
		r := doStatic(t, h, http.MethodHead, "/assets/hello.txt")
		if r.status != http.StatusOK {
			t.Fatalf("status = %d, want 200", r.status)
		}
		if len(r.body) != 0 {
			t.Fatalf("HEAD body = %q, want empty", r.body)
		}
		if r.contentType == "" {
			t.Fatalf("HEAD content-type empty")
		}
	})

	t.Run("HEAD on missing file is 404", func(t *testing.T) {
		r := doStatic(t, h, http.MethodHead, "/assets/nope.txt")
		if r.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", r.status)
		}
	})
}
