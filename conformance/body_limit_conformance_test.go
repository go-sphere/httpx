package conformance

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
)

// fiber and hertz enforce their own 4 MiB body limit in the server, before any
// handler, so WithMaxBodySize above it must raise that limit. The in-process
// requester of hertzx bypasses its server, hence a real listener.
func TestMaxBodySizeRaisesFrameworkLimit(t *testing.T) {
	const limit = 5 << 20
	for _, framework := range []string{"fiberx", "hertzx"} {
		t.Run(framework, func(t *testing.T) {
			addr := reserveAddrTB(t)
			var engine httpx.Engine
			switch framework {
			case "fiberx":
				engine = fiberx.New(
					fiberx.WithListen(addr, fiber.ListenConfig{DisableStartupMessage: true}),
					fiberx.WithMaxBodySize(limit),
				)
			case "hertzx":
				hlog.SetSilentMode(true)
				hlog.SetOutput(io.Discard)
				engine = hertzx.New(hertzx.WithAddr(addr), hertzx.WithMaxBodySize(limit))
			}
			engine.Group("").POST("/upload", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, fmt.Sprint(len(raw)))
			})
			stop := startEngineAndWait(t, engine, addr)
			defer stop()

			post := func(size int) (int, string, error) {
				t.Helper()
				resp, err := http.Post("http://"+addr+"/upload", "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte("a"), size)))
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
