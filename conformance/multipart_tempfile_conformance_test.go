package conformance

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

type tempfileCtxKey struct{}

// An upload larger than the in-memory multipart budget spills to temporary
// files, which must be gone once the request is over — including after a layer
// replaced the request's context with SetContext, which on the net/http
// adapters swaps the *http.Request the form is parsed on. Needs a real
// listener: net/http removes the files only when it finishes a served request.
func TestMultipartTempFilesRemovedAfterSetContext(t *testing.T) {
	const fileSize = 33 << 20 // above the 32 MiB kept in memory
	var body bytes.Buffer
	body.Grow(fileSize + 1024)
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte{'x'}, 64<<10)
	for remaining := fileSize; remaining > 0; {
		n := min(remaining, len(chunk))
		if _, err := part.Write(chunk[:n]); err != nil {
			t.Fatal(err)
		}
		remaining -= n
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			engine, addr := a.serve(t, serveConfig{maxBodySize: 64 << 20})
			var spilled atomic.Bool
			engine.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.SetContext(context.WithValue(ctx.Context(), tempfileCtxKey{}, "trace"))
					return next(ctx)
				}
			})
			engine.Group("").POST("/upload", func(ctx httpx.Context) error {
				fh, err := ctx.FormFile("file")
				if err != nil {
					return err
				}
				if entries, err := os.ReadDir(tmp); err == nil && len(entries) > 0 {
					spilled.Store(true)
				}
				return ctx.Text(http.StatusOK, fh.Filename)
			})
			startServing(t, engine, addr)

			req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/upload", bytes.NewReader(body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", mw.FormDataContentType())
			resp, err := oneShotClient(10 * time.Second).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if !spilled.Load() {
				t.Fatal("expected multipart upload >32 MiB to spill temporary file into TMPDIR during handling")
			}

			// The server removes the files after the response is out; give it a
			// moment before calling them leaked.
			deadline := time.Now().Add(3 * time.Second)
			for {
				entries, err := os.ReadDir(tmp)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) == 0 {
					return
				}
				if time.Now().After(deadline) {
					names := make([]string, len(entries))
					for i, e := range entries {
						names[i] = e.Name()
					}
					t.Fatalf("temporary upload files left behind: %v", names)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}
