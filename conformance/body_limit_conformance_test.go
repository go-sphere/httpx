package conformance

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// WithMaxBodySize is the limit a client sees, whatever the server underneath
// enforces on its own: fiber and hertz refuse bodies over 4 MiB before any
// handler runs, so a larger WithMaxBodySize must raise that limit. The
// in-process requester of hertzx bypasses its server, hence a real listener.
func TestMaxBodySizeRaisesFrameworkLimit(t *testing.T) {
	const limit = 5 << 20
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{maxBodySize: limit})
			engine.Group("").POST("/upload", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, fmt.Sprint(len(raw)))
			})
			startServing(t, engine, addr)

			post := func(size int) (int, string, error) {
				t.Helper()
				resp, err := oneShotClient(10*time.Second).Post("http://"+addr+"/upload", "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte("a"), size)))
				if err != nil {
					return 0, "", err
				}
				defer func() { _ = resp.Body.Close() }()
				body, _ := io.ReadAll(resp.Body)
				return resp.StatusCode, strings.TrimSpace(string(body)), nil
			}
			under := limit - 1<<10
			if status, body, err := post(under); err != nil || status != http.StatusOK || body != fmt.Sprint(under) {
				t.Fatalf("under the limit: status=%d body=%q err=%v, want 200 %d", status, body, err, under)
			}
			// The server may answer and close before the client has finished
			// sending, which the client reports as a write error instead.
			if status, _, err := post(limit + 1<<10); err == nil && status != http.StatusRequestEntityTooLarge {
				t.Fatalf("over the limit: status=%d, want 413", status)
			}
		})
	}
}
