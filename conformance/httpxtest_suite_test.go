// Package conformance holds the cross-adapter tests that cannot live in
// httpxtest or in one adapter's module, where each adapter runs the shared
// cases itself (its conformance_test.go): tests that need a real listener
// (engine lifecycle, trusted proxies, streaming, shutdown, body limits),
// native-context reuse, the concurrency and FD-leak stress tests and the
// cross-framework benchmark tables. httpxtestSuites, extended by adapters with
// real-listener construction, is its one list of adapters. It contains only
// tests and is not meant to be imported.
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

func httpxtestSuites() []httpxtest.Suite {
	return []httpxtest.Suite{
		{
			Name: "ginx",
			Caps: httpxtest.Caps{
				NamedWildcard:              true,
				Flusher:                    true,
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(_ testing.TB, opts httpxtest.Options) httpx.Engine {
				gin.SetMode(gin.ReleaseMode)
				engineOpts := []ginx.Option{ginx.WithEngine(gin.New())}
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, ginx.WithErrorHandler(opts.ErrorHandler))
				}
				if opts.MaxBodySize > 0 {
					engineOpts = append(engineOpts, ginx.WithMaxBodySize(opts.MaxBodySize))
				}
				return ginx.New(engineOpts...)
			},
			StdMiddleware: ginx.AdaptStdMiddleware,
			Dispatch:      ginDispatch,
			NativeMiddleware: func(scope any, mark func(string)) {
				useNative[gin.HandlerFunc](scope, func(c *gin.Context) {
					mark("native-pre")
					c.Next()
					mark("native-post")
				})
			},
		},
		{
			Name: "fiberx",
			Caps: httpxtest.Caps{
				ForcedStopCutsConnections: false,
			},
			NewEngine: func(_ testing.TB, opts httpxtest.Options) httpx.Engine {
				var engineOpts []fiberx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, fiberx.WithErrorHandler(opts.ErrorHandler))
				}
				if opts.MaxBodySize > 0 {
					engineOpts = append(engineOpts, fiberx.WithMaxBodySize(opts.MaxBodySize))
				}
				return fiberx.New(engineOpts...)
			},
			StdMiddleware: fiberx.AdaptStdMiddleware,
			Dispatch:      fiberDispatch,
			NativeMiddleware: func(scope any, mark func(string)) {
				useNative[fiber.Handler](scope, func(c fiber.Ctx) error {
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
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(_ testing.TB, opts httpxtest.Options) httpx.Engine {
				engineOpts := []echox.Option{echox.WithEngine(echo.New())}
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, echox.WithErrorHandler(opts.ErrorHandler))
				}
				if opts.MaxBodySize > 0 {
					engineOpts = append(engineOpts, echox.WithMaxBodySize(opts.MaxBodySize))
				}
				return echox.New(engineOpts...)
			},
			StdMiddleware: echox.AdaptStdMiddleware,
			Dispatch:      echoDispatch,
			NativeMiddleware: func(scope any, mark func(string)) {
				useNative[echo.MiddlewareFunc](scope, func(next echo.HandlerFunc) echo.HandlerFunc {
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
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  false,
			},
			NewEngine: func(_ testing.TB, opts httpxtest.Options) httpx.Engine {
				hlog.SetSilentMode(true)
				hlog.SetOutput(io.Discard)
				var engineOpts []hertzx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, hertzx.WithErrorHandler(opts.ErrorHandler))
				}
				if opts.MaxBodySize > 0 {
					engineOpts = append(engineOpts, hertzx.WithMaxBodySize(opts.MaxBodySize))
				}
				return hertzx.New(engineOpts...)
			},
			StdMiddleware: hertzx.AdaptStdMiddleware,
			Dispatch:      hertzDispatch,
			NativeMiddleware: func(scope any, mark func(string)) {
				useNative[app.HandlerFunc](scope, func(ctx context.Context, rc *app.RequestContext) {
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
				InProcessUnknownLengthBody: true,
				ForcedStopCutsConnections:  true,
			},
			NewEngine: func(_ testing.TB, opts httpxtest.Options) httpx.Engine {
				var engineOpts []stdx.Option
				if opts.ErrorHandler != nil {
					engineOpts = append(engineOpts, stdx.WithErrorHandler(opts.ErrorHandler))
				}
				if opts.MaxBodySize > 0 {
					engineOpts = append(engineOpts, stdx.WithMaxBodySize(opts.MaxBodySize))
				}
				return stdx.New(engineOpts...)
			},
			StdMiddleware: stdx.AdaptStdMiddleware,
			Dispatch:      stdDispatch,
		},
	}
}

// useNative is a type assertion because each adapter's UseNative takes its own
// framework's middleware type.
func useNative[T any](scope any, middleware T) {
	user, ok := scope.(interface{ UseNative(...T) })
	if !ok {
		panic("conformance: scope does not have UseNative for this framework")
	}
	user.UseNative(middleware)
}

func BenchmarkHTTPXTestSuite(b *testing.B) {
	for _, suite := range httpxtestSuites() {
		b.Run("adapter="+suite.Name, func(b *testing.B) {
			httpxtest.RunBenchmarks(b, suite)
		})
	}
}

// Dispatch drives each framework's own dispatcher with reusable buffers, so
// RunBenchmarks measures the adapter, not the harness. The runner split keeps
// the native side of BenchmarkNativeVsHTTPX resetting the request identically.

func ginDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	tb.Helper()
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	register(ginx.New(ginx.WithEngine(ge)).Group(""))
	return netHTTPRunner(tb, "ginx", ge, req)
}

func stdDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	tb.Helper()
	engine := stdx.New()
	register(engine.Group(""))
	handler, ok := engine.(http.Handler)
	if !ok {
		tb.Fatal("stdx: engine is not an http.Handler")
	}
	return netHTTPRunner(tb, "stdx", handler, req)
}

func echoDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	tb.Helper()
	e := echo.New()
	register(echox.New(echox.WithEngine(e)).Group(""))
	return netHTTPRunner(tb, "echox", e, req)
}

// fiberDispatch needs the fiber.App to drive, which an adapter-built engine
// does not expose, so it builds the app with the configuration
// fiberx.NewConfig documents for one and passes it in.
func fiberDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	tb.Helper()
	f := fiber.New(fiber.Config{
		ErrorHandler:              fiberx.DefaultErrorHandler,
		UnescapePath:              true,
		CaseSensitive:             true,
		StrictRouting:             true,
		DisableHeadAutoRegister:   true,
		DisableDefaultContentType: true,
	})
	register(fiberx.New(fiberx.WithEngine(f)).Group(""))
	return fiberRunner(tb, "fiberx", f, req)
}

func hertzDispatch(tb testing.TB, register func(httpx.Router), req *http.Request) func() {
	tb.Helper()
	h := newBenchHertz()
	register(hertzx.New(hertzx.WithEngine(h)).Group(""))
	return hertzRunner(tb, "hertzx", h, req)
}

func netHTTPRunner(tb testing.TB, name string, h http.Handler, req *http.Request) func() {
	tb.Helper()
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

// fiberRunner resets the cached multipart form too; without that, every
// iteration after the first would reuse fiber's parse.
func fiberRunner(tb testing.TB, name string, f *fiber.App, req *http.Request) func() {
	tb.Helper()
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

// hertzRunner refills the request every iteration: hertz has no "reset the
// response only" API, and ResetWithoutConn clears the request too.
func hertzRunner(tb testing.TB, name string, h *server.Hertz, req *http.Request) func() {
	tb.Helper()
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
