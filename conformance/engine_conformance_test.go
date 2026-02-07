package conformance

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
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

func TestEngineConformance(t *testing.T) {
	for _, name := range []string{"ginx", "fiberx", "echox", "hertzx"} {
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

func runAcrossFrameworks(t *testing.T, register func(httpx.Router), request func() *http.Request) map[string]responseSnapshot {
	t.Helper()

	results := make(map[string]responseSnapshot, 4)
	for _, name := range []string{"ginx", "fiberx", "echox", "hertzx"} {
		h := newHarness(t, name)
		register(h.Router)
		results[name] = h.Do(t, request())
	}
	return results
}

func newHarness(t *testing.T, name string) frameworkHarness {
	t.Helper()

	switch name {
	case "ginx":
		g := gin.New()
		g.Use(gin.Recovery())
		engine := ginx.New(ginx.WithEngine(g), ginx.WithServerAddr(":0"))
		return frameworkHarness{
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
	case "fiberx":
		app := fiber.New(fiber.Config{
			ErrorHandler: func(ctx fiber.Ctx, err error) error {
				return ctx.Status(500).JSON(fiber.Map{"error": err.Error()})
			},
		})
		engine := fiberx.New(fiberx.WithEngine(app), fiberx.WithListen(":0"))
		return frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
				t.Helper()
				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("fiberx test request failed: %v", err)
				}
				return snapshotFromHTTPResponse(t, resp)
			},
		}
	case "echox":
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			_ = c.JSON(500, echo.Map{"error": err.Error()})
		}
		engine := echox.New(echox.WithEngine(e), echox.WithServerAddr(":0"))
		return frameworkHarness{
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
	case "hertzx":
		h := server.Default(server.WithHostPorts("127.0.0.1:0"))
		engine := hertzx.New(hertzx.WithEngine(h))
		return frameworkHarness{
			Name:   name,
			Engine: engine,
			Router: engine.Group(""),
			Do: func(t *testing.T, req *http.Request) responseSnapshot {
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
			},
		}
	default:
		t.Fatalf("unknown framework: %s", name)
		return frameworkHarness{}
	}
}

func snapshotFromHTTPResponse(t *testing.T, resp *http.Response) responseSnapshot {
	t.Helper()
	defer resp.Body.Close()
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
