package conformance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
	"github.com/labstack/echo/v4"
)

type responseSnapshot struct {
	Status  int
	Body    string
	Headers http.Header
}

type frameworkHarness struct {
	Name   string
	Engine httpx.Engine
	Router httpx.Router
	Do     func(*testing.T, *http.Request) responseSnapshot
}

type harnessMode int

const (
	harnessModeInProcess harnessMode = iota
	harnessModeStartOnly
	harnessModeNetwork
)

type harnessErrorMode int

const (
	harnessErrorDefault harnessErrorMode = iota
	harnessErrorTeapot
)

type harnessOptions struct {
	mode            harnessMode
	errorMode       harnessErrorMode
	silenceHertzLog bool
}

type harnessBundle struct {
	harness frameworkHarness
	baseURL string
	client  *http.Client
}

var conformanceFrameworks = []string{"ginx", "fiberx", "echox", "hertzx"}

func TestEngineConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			if h.Engine.IsRunning() {
				t.Fatalf("%s engine should not be running", name)
			}
			_ = h.Engine.Stop(context.Background())
			if h.Engine.IsRunning() {
				t.Fatalf("%s engine should remain stopped", name)
			}
		})
	}
}

func TestEngineStartConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			engine := newStartEngine(t, name)

			startErrCh := make(chan error, 1)
			go func() {
				startErrCh <- engine.Start()
			}()

			deadline := time.Now().Add(800 * time.Millisecond)
			for time.Now().Before(deadline) {
				if engine.IsRunning() {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = engine.Stop(stopCtx)

			select {
			case err := <-startErrCh:
				if !isExpectedStartExit(err) {
					t.Fatalf("%s start returned unexpected error: %v", name, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s start did not exit after stop", name)
			}
		})
	}
}

func isExpectedStartExit(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, http.ErrServerClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "server closed") || strings.Contains(msg, "closed")
}

func newStartEngine(t *testing.T, name string) httpx.Engine {
	t.Helper()
	b := newFrameworkHarnessTB(t, name, harnessOptions{mode: harnessModeStartOnly, errorMode: harnessErrorDefault, silenceHertzLog: true})
	return b.harness.Engine
}

func runAcrossFrameworks(t *testing.T, register func(httpx.Router), request func() *http.Request) map[string]responseSnapshot {
	t.Helper()

	results := make(map[string]responseSnapshot, 4)
	for _, name := range conformanceFrameworks {
		t.Logf("case=%s framework=%s", t.Name(), name)
		h := newHarness(t, name)
		register(h.Router)
		results[name] = h.Do(t, request())
	}
	return results
}

func newHarness(t *testing.T, name string) frameworkHarness {
	t.Helper()
	return newHarnessTB(t, name)
}

func newHarnessTB(tb testing.TB, name string) frameworkHarness {
	tb.Helper()
	b := newFrameworkHarnessTB(tb, name, harnessOptions{mode: harnessModeInProcess, errorMode: harnessErrorDefault})
	return b.harness
}

func newFrameworkHarnessTB(tb testing.TB, name string, opts harnessOptions) harnessBundle {
	tb.Helper()

	if opts.silenceHertzLog {
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
	}

	switch name {
	case "ginx":
		gin.SetMode(gin.ReleaseMode)
		g := gin.New()
		g.Use(gin.Recovery())
		addr := ginLikeAddrForMode(tb, opts.mode)

		var engine httpx.Engine
		if opts.errorMode == harnessErrorTeapot {
			engine = ginx.New(
				ginx.WithEngine(g),
				ginx.WithServerAddr(addr),
				ginx.WithErrorHandler(func(ctx *gin.Context, err error) {
					ctx.JSON(http.StatusTeapot, gin.H{"error": err.Error()})
				}),
			)
		} else {
			engine = ginx.New(ginx.WithEngine(g), ginx.WithServerAddr(addr))
		}

		h := frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
				t.Helper()
				rr := httptest.NewRecorder()
				g.ServeHTTP(rr, req)
				return snapshotFromHTTPResponse(t, rr.Result())
			},
		}
		if opts.mode == harnessModeNetwork {
			return harnessBundle{harness: h, baseURL: "http://" + addr, client: &http.Client{Timeout: 2 * time.Second}}
		}
		return harnessBundle{harness: h}
	case "fiberx":
		f := fiber.New(fiber.Config{
			ErrorHandler: func(ctx fiber.Ctx, err error) error {
				if opts.errorMode == harnessErrorTeapot {
					return ctx.Status(http.StatusTeapot).JSON(fiber.Map{"error": err.Error()})
				}
				return ctx.Status(500).JSON(fiber.Map{"error": err.Error()})
			},
		})

		var engine httpx.Engine
		baseURL := ""
		client := (*http.Client)(nil)
		switch opts.mode {
		case harnessModeNetwork:
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				tb.Fatalf("listen failed: %v", err)
				return harnessBundle{}
			}
			engine = fiberx.New(fiberx.WithEngine(f), fiberx.WithListener(ln, fiber.ListenConfig{DisableStartupMessage: true}))
			baseURL = "http://" + ln.Addr().String()
			client = &http.Client{Timeout: 2 * time.Second}
		case harnessModeStartOnly:
			engine = fiberx.New(fiberx.WithEngine(f), fiberx.WithListen("127.0.0.1:0", fiber.ListenConfig{DisableStartupMessage: true}))
		default:
			engine = fiberx.New(fiberx.WithEngine(f), fiberx.WithListen(":0"))
		}

		h := frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
				t.Helper()
				resp, err := f.Test(req)
				if err != nil {
					t.Fatalf("fiberx test request failed: %v", err)
				}
				return snapshotFromHTTPResponse(t, resp)
			},
		}
		return harnessBundle{harness: h, baseURL: baseURL, client: client}
	case "echox":
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			status := http.StatusInternalServerError
			if opts.errorMode == harnessErrorTeapot {
				status = http.StatusTeapot
			}
			_ = c.JSON(status, echo.Map{"error": err.Error()})
		}

		addr := ginLikeAddrForMode(tb, opts.mode)
		engine := echox.New(echox.WithEngine(e), echox.WithServerAddr(addr))
		h := frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
				t.Helper()
				rr := httptest.NewRecorder()
				e.ServeHTTP(rr, req)
				return snapshotFromHTTPResponse(t, rr.Result())
			},
		}
		if opts.mode == harnessModeNetwork {
			return harnessBundle{harness: h, baseURL: "http://" + addr, client: &http.Client{Timeout: 2 * time.Second}}
		}
		return harnessBundle{harness: h}
	case "hertzx":
		addr := hertzAddrForMode(tb, opts.mode)
		h := server.Default(
			server.WithHostPorts(addr),
			server.WithDisablePrintRoute(true),
		)

		var engine httpx.Engine
		if opts.errorMode == harnessErrorTeapot {
			engine = hertzx.New(
				hertzx.WithEngine(h),
				hertzx.WithErrorHandler(func(ctx context.Context, rc *app.RequestContext, err error) {
					rc.JSON(http.StatusTeapot, map[string]string{"error": err.Error()})
				}),
			)
		} else {
			engine = hertzx.New(hertzx.WithEngine(h))
		}

		fh := frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
				t.Helper()
				return doHertzRequest(t, h, req)
			},
		}
		if opts.mode == harnessModeNetwork {
			return harnessBundle{harness: fh, baseURL: "http://" + addr, client: &http.Client{Timeout: 2 * time.Second}}
		}
		return harnessBundle{harness: fh}
	default:
		tb.Fatalf("unknown framework: %s", name)
		return harnessBundle{}
	}
}

func ginLikeAddrForMode(tb testing.TB, mode harnessMode) string {
	tb.Helper()
	switch mode {
	case harnessModeStartOnly:
		return "127.0.0.1:0"
	case harnessModeNetwork:
		return reserveAddrTB(tb)
	default:
		return ":0"
	}
}

func hertzAddrForMode(tb testing.TB, mode harnessMode) string {
	tb.Helper()
	switch mode {
	case harnessModeInProcess:
		return "127.0.0.1:0"
	case harnessModeStartOnly:
		return "127.0.0.1:0"
	case harnessModeNetwork:
		return reserveAddrTB(tb)
	default:
		return "127.0.0.1:0"
	}
}

func doHertzRequest(t *testing.T, h *server.Hertz, req *http.Request) responseSnapshot {
	t.Helper()

	urlStr := req.URL.String()
	if !req.URL.IsAbs() {
		urlStr = "http://example.com" + req.URL.RequestURI()
	}

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
	}

	hctx := h.NewContext()
	hctx.Request.Header.SetMethod(req.Method)
	hctx.Request.SetRequestURI(urlStr)
	if len(bodyBytes) > 0 {
		hctx.Request.SetBodyStream(bytes.NewReader(bodyBytes), len(bodyBytes))
	}
	for key, values := range req.Header {
		for _, value := range values {
			hctx.Request.Header.Add(key, value)
		}
	}

	h.ServeHTTP(context.Background(), hctx)

	hdr := make(http.Header)
	hctx.Response.Header.VisitAll(func(k, v []byte) {
		hdr.Add(textproto.CanonicalMIMEHeaderKey(string(k)), string(v))
	})
	for _, setCookie := range hctx.Response.Header.GetAll("Set-Cookie") {
		hdr.Add("Set-Cookie", setCookie)
	}

	return responseSnapshot{Status: hctx.Response.StatusCode(), Body: string(hctx.Response.Body()), Headers: hdr}
}

func snapshotFromHTTPResponse(t *testing.T, resp *http.Response) responseSnapshot {
	t.Helper()
	defer func() {
		_ = resp.Body.Close
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	h := make(http.Header, len(resp.Header))
	for key, values := range resp.Header {
		h[key] = append([]string(nil), values...)
	}
	return responseSnapshot{Status: resp.StatusCode, Body: string(body), Headers: h}
}
