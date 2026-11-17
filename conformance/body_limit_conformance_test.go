package conformance

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
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

// A client declaring Content-Length > limit that stalls instead of sending the
// body must get 413 + Connection: close immediately rather than hanging the server.
func TestLimitRequestBodySlowlorisReturns413WithoutWaitingConformance(t *testing.T) {
	const limit = 1024
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{maxBodySize: limit})
			engine.Group("").POST("/slowloris", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, fmt.Sprint(len(raw)))
			})
			startServing(t, engine, addr)

			contentLen := 4096
			if a.Name == "fiberx" || a.Name == "hertzx" {
				// fiberx and hertzx buffer bodies before handlers run at their 4 MiB default ceiling.
				contentLen = 5 << 20
			}

			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				t.Fatalf("dial %s: %v", addr, err)
			}
			defer func() { _ = conn.Close() }()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatalf("set deadline: %v", err)
			}
			reqHead := fmt.Sprintf("POST /slowloris HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\n\r\npartial", addr, contentLen)
			if _, err := io.WriteString(conn, reqHead); err != nil {
				t.Fatalf("write headers: %v", err)
			}

			resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
			if err != nil {
				t.Fatalf("read 413 response without closing request body: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413", resp.StatusCode)
			}
			if !resp.Close && !strings.EqualFold(resp.Header.Get("Connection"), "close") {
				t.Fatalf("Connection = %q (Close=%v), want close", resp.Header.Get("Connection"), resp.Close)
			}
			if _, err := io.ReadAll(resp.Body); err != nil {
				t.Fatalf("reading 413 response body / EOF: %v", err)
			}
		})
	}
}
