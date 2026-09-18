package conformance

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/go-sphere/httpx/stdx"
	"github.com/gofiber/fiber/v3"
	"github.com/labstack/echo/v4"
	"github.com/valyala/fasthttp"
)

// Each adapter declares its capabilities and an engine factory, and runs the
// shared suite from httpxtest against the golden contracts recorded there.
//
// These suites belong in the adapter modules — that is the point of putting the
// cases in an importable package, and it is what a third-party adapter does.
// They live here until the root module is released with httpxtest in it: with
// GOWORK=off (which `make check` uses for its dependency check) ginx and the
// others resolve github.com/go-sphere/httpx at their declared version, which
// does not contain the package yet. Adding a replace to an adapter go.mod is
// not an option — it would ship in ginx/vX.Y.Z. After the next root tag, move
// each entry below into <adapter>/conformance_test.go unchanged and bump that
// module's httpx requirement.
func httpxtestSuites() []httpxtest.Suite {
	return []httpxtest.Suite{
		{
			Name: "ginx",
			Caps: httpxtest.Caps{
				NamedWildcard:              true,
				Flusher:                    true,
				RendersErrorAtFailingLayer: true,
				ComposesInterceptors:       true,
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
				gin.SetMode(gin.ReleaseMode)
				engineOpts := []ginx.Option{ginx.WithEngine(gin.New())}
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, ginx.WithErrorHandler(opts.ErrorHandler))
				}
				return ginx.New(engineOpts...)
			},
			StdMiddleware: ginx.AdaptStdMiddleware,
			Dispatch:      ginDispatch,
			NativeMiddleware: func(mark func(string)) httpx.Middleware {
				return ginx.AdaptGinMiddleware(func(c *gin.Context) {
					mark("native-pre")
					c.Next()
					mark("native-post")
				})
			},
		},
		{
			Name: "fiberx",
			Caps: httpxtest.Caps{
				RendersErrorAtFailingLayer: true,
				ComposesInterceptors:       true,
				// fasthttp offers no forced connection close; see the field doc.
				ForcedStopCutsConnections: false,
			},
			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
				var engineOpts []fiberx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, fiberx.WithErrorHandler(opts.ErrorHandler))
				}
				return fiberx.New(engineOpts...)
			},
			StdMiddleware: fiberx.AdaptStdMiddleware,
			Dispatch:      fiberDispatch,
			NativeMiddleware: func(mark func(string)) httpx.Middleware {
				return fiberx.AdaptFiberMiddleware(func(c fiber.Ctx) error {
					mark("native-pre")
					err := c.Next()
					mark("native-post")
					return err
				})
			},
		},
		{
			Name: "echox",
			Caps: httpxtest.Caps{
				Flusher:                    true,
				ComposesInterceptors:       true,
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
				engineOpts := []echox.Option{echox.WithEngine(echo.New())}
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, echox.WithErrorHandler(opts.ErrorHandler))
				}
				return echox.New(engineOpts...)
			},
			StdMiddleware: echox.AdaptStdMiddleware,
			Dispatch:      echoDispatch,
			NativeMiddleware: func(mark func(string)) httpx.Middleware {
				return echox.AdaptEchoMiddleware(func(next echo.HandlerFunc) echo.HandlerFunc {
					return func(c echo.Context) error {
						mark("native-pre")
						err := next(c)
						mark("native-post")
						return err
					}
				})
			},
		},
		{
			Name: "hertzx",
			Caps: httpxtest.Caps{
				NamedWildcard:              true,
				Flusher:                    true,
				RendersErrorAtFailingLayer: true,
				ComposesInterceptors:       true,
				InProcessUnknownLengthBody: true,
				// hertz's Engine.Close never touches an active connection; see
				// the field doc.
				ForcedStopCutsConnections: false,
			},
			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
				hlog.SetSilentMode(true)
				hlog.SetOutput(io.Discard)
				var engineOpts []hertzx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, hertzx.WithErrorHandler(opts.ErrorHandler))
				}
				return hertzx.New(engineOpts...)
			},
			StdMiddleware: hertzx.AdaptStdMiddleware,
			Dispatch:      hertzDispatch,
			NativeMiddleware: func(mark func(string)) httpx.Middleware {
				return hertzx.AdaptHertzMiddleware(func(ctx context.Context, rc *app.RequestContext) {
					mark("native-pre")
					rc.Next(ctx)
					mark("native-post")
				})
			},
		},
		{
			Name: "stdx",
			Caps: httpxtest.Caps{
				NamedWildcard:              true,
				Flusher:                    true,
				ComposesInterceptors:       true,
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
				var engineOpts []stdx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, stdx.WithErrorHandler(opts.ErrorHandler))
				}
				return stdx.New(engineOpts...)
			},
			StdMiddleware: stdx.AdaptStdMiddleware,
			Dispatch:      stdDispatch,
			NativeMiddleware: func(mark func(string)) httpx.Middleware {
				return stdx.AdaptStdMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mark("native-pre")
						next.ServeHTTP(w, r)
						mark("native-post")
					})
				})
			},
		},
	}
}

func TestHTTPXTestSuite(t *testing.T) {
	for _, suite := range append(httpxtestSuites(), fiberxOwnEngineSuite()) {
		t.Run(suite.Name, func(t *testing.T) {
			httpxtest.Run(t, suite)
		})
	}
}

// fiberxOwnEngineSuite runs the whole suite against a fiber.App the caller
// built, which is the one configuration the adapter cannot fix up at
// construction time: fiber.Config is immutable after fiber.New, and its
// defaults differ from what the httpx contract requires (UnescapePath in
// particular). Only the tests use it — the benchmark table measures adapters,
// not configurations.
func fiberxOwnEngineSuite() httpxtest.Suite {
	suite := httpxtestSuites()[1]
	if suite.Name != "fiberx" {
		panic("fiberxOwnEngineSuite: suite order changed")
	}
	suite.Name = "fiberx-own-engine"
	suite.NewEngine = func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
		engineOpts := []fiberx.Option{fiberx.WithEngine(fiber.New())}
		if opts.ErrorHandler != nil {
			engineOpts = append(engineOpts, fiberx.WithErrorHandler(opts.ErrorHandler))
		}
		return fiberx.New(engineOpts...)
	}
	// Dispatch drives the adapter-built engine; leaving it set would measure
	// something this suite does not construct.
	suite.Dispatch = nil
	return suite
}

// BenchmarkHTTPXTestSuite measures the shared scenario table for every
// adapter. It goes through the in-process requester, so the numbers compare
// scenarios within one adapter; BenchmarkAdapter is the native-versus-httpx
// comparison.
func BenchmarkHTTPXTestSuite(b *testing.B) {
	for _, suite := range httpxtestSuites() {
		b.Run("adapter="+suite.Name, func(b *testing.B) {
			httpxtest.RunBenchmarks(b, suite)
		})
	}
}

// The Dispatch hooks below are what make httpxtest.RunBenchmarks measure the
// adapter instead of the test harness: each drives its framework's own
// dispatcher with reusable buffers, the way that framework is served in
// production.

// Each adapter's Dispatch is its framework runner applied to an engine with
// the httpx routes registered. The runner is split out so the native side of
// BenchmarkNativeVsHTTPX resets the request exactly the same way — otherwise
// the pair would measure the harness, not the abstraction.

func ginDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	register(ginx.New(ginx.WithEngine(ge)).Group(""))
	return ginRunner(tb, "ginx", ge, req)
}

// stdDispatch needs no bridging at all: the engine is the http.Handler.
func stdDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	engine := stdx.New()
	register(engine.Group(""))
	handler, ok := engine.(http.Handler)
	if !ok {
		tb.Fatal("stdx: engine is not an http.Handler")
	}
	return netHTTPRunner(tb, "stdx", handler, req)
}

func echoDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	e := echo.New()
	register(echox.New(echox.WithEngine(e)).Group(""))
	return netHTTPRunner(tb, "echox", e, req)
}

func fiberDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	f := fiber.New()
	register(fiberx.New(fiberx.WithEngine(f)).Group(""))
	return fiberRunner(tb, "fiberx", f, req)
}

func hertzDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	h := newBenchHertz()
	register(hertzx.New(hertzx.WithEngine(h)).Group(""))
	return hertzRunner(tb, "hertzx", h, req)
}

func ginRunner(tb testing.TB, name string, ge *gin.Engine, req *http.Request) func() {
	return netHTTPRunner(tb, name, ge, req)
}

// netHTTPRunner reuses one request and a writer that drops bodies: the only
// per-iteration work is clearing the response header.
func netHTTPRunner(tb testing.TB, name string, h http.Handler, req *http.Request) func() {
	w := &benchmarkResponseWriter{header: make(http.Header), status: http.StatusOK}
	run := func() {
		clear(w.header)
		h.ServeHTTP(w, req)
	}
	run()
	if w.status >= http.StatusBadRequest {
		tb.Fatalf("%s: scenario returned %d, want a success status", name, w.status)
	}
	return run
}

// fiberRunner materializes the fasthttp request once; per iteration it resets
// the response, the route values and the cached multipart form (without the
// last one every iteration after the first would reuse fiber's parse).
func fiberRunner(tb testing.TB, name string, f *fiber.App, req *http.Request) func() {
	handler := f.Handler()
	ctx := &fasthttp.RequestCtx{}
	fillFastHTTPRequest(tb, &ctx.Request, req)

	run := func() {
		ctx.Response.Reset()
		ctx.ResetUserValues()
		ctx.Request.RemoveMultipartFormFiles()
		handler(ctx)
	}
	run()
	if status := ctx.Response.StatusCode(); status >= http.StatusBadRequest {
		tb.Fatalf("%s: scenario returned %d, want a success status", name, status)
	}
	return run
}

// hertzRunner has to refill the request every iteration: hertz has no "reset
// the response only" API, and ResetWithoutConn clears the request too. That
// cost is inside every hertz measurement, native and httpx alike.
func hertzRunner(tb testing.TB, name string, h *server.Hertz, req *http.Request) func() {
	rc := h.NewContext()
	ctx := context.Background()
	body := readRequestBody(tb, req)

	run := func() {
		rc.ResetWithoutConn()
		fillHertzRequest(&rc.Request, req, body)
		h.ServeHTTP(ctx, rc)
	}
	run()
	if status := rc.Response.StatusCode(); status >= http.StatusBadRequest {
		tb.Fatalf("%s: scenario returned %d, want a success status", name, status)
	}
	return run
}

func newBenchHertz() *server.Hertz {
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	return server.New()
}

func fillFastHTTPRequest(tb testing.TB, dst *fasthttp.Request, req *http.Request) {
	tb.Helper()
	dst.Header.SetMethod(req.Method)
	dst.SetRequestURI(req.URL.RequestURI())
	dst.Header.SetHost(req.Host)
	for key, values := range req.Header {
		for _, v := range values {
			dst.Header.Add(key, v)
		}
	}
	if body := readRequestBody(tb, req); len(body) > 0 {
		dst.SetBody(body)
	}
}

func fillHertzRequest(dst *protocol.Request, req *http.Request, body []byte) {
	dst.Header.SetMethod(req.Method)
	dst.SetRequestURI(req.URL.RequestURI())
	dst.Header.SetHost(req.Host)
	for key, values := range req.Header {
		for _, v := range values {
			dst.Header.Add(key, v)
		}
	}
	if len(body) > 0 {
		dst.SetBody(body)
	}
}

func readRequestBody(tb testing.TB, req *http.Request) []byte {
	tb.Helper()
	if req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		tb.Fatalf("read request body: %v", err)
	}
	return body
}
