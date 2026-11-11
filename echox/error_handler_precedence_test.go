package echox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

// NewConfig preserves a caller's HTTPErrorHandler while New replaces it whenever
// WithErrorHandler is given, so the two can disagree. These pin the single
// precedence list both read, documented on NewConfig.

// markerHandler answers with a status nothing else in this file uses, so which
// handler ran is readable off the response alone.
func markerHandler(status int) echo.HTTPErrorHandler {
	return func(_ error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		_ = c.String(status, "marker")
	}
}

func unmatchedResponse(t *testing.T, engine httpx.Engine) *http.Response {
	t.Helper()
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("echox engine is not a TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "/nothing-here", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// 1. WithErrorHandler is the portable surface, so it wins over a handler the
// caller installed on their own engine: otherwise the framework-neutral handler
// would render routed errors while echo's own 404/405 went somewhere different.
func TestWithErrorHandlerOutranksTheEnginesOwn(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = markerHandler(http.StatusTeapot)

	engine := New(WithEngine(e), WithErrorHandler(func(ctx httpx.Context, _ error) {
		_ = ctx.Text(http.StatusPaymentRequired, "httpx")
	}))

	if resp := unmatchedResponse(t, engine); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want %d from the WithErrorHandler handler",
			resp.StatusCode, http.StatusPaymentRequired)
	}
}

// 2. Without WithErrorHandler, a handler the caller set on their own engine is
// left alone — that is what the isEchoDefaultErrorHandler guard is for.
func TestEnginesOwnErrorHandlerSurvivesWithoutWithErrorHandler(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = markerHandler(http.StatusTeapot)

	engine := New(WithEngine(e))

	if resp := unmatchedResponse(t, engine); resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d: the caller's own handler was replaced",
			resp.StatusCode, http.StatusTeapot)
	}
}

// 3. An engine still carrying echo's own default gets this adapter's, so
// New(WithEngine(echo.New())) answers the same as New().
func TestEchoDefaultErrorHandlerIsReplaced(t *testing.T) {
	e := echo.New()
	engine := New(WithEngine(e))

	resp := unmatchedResponse(t, engine)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want the shared httpx error body", got)
	}
}

// The documented override point: assigning the field after New wins outright, so
// a caller who wants their own handler back has a supported way to take it.
func TestErrorHandlerCanBeOverriddenAfterNew(t *testing.T) {
	e := echo.New()
	engine := New(WithEngine(e), WithErrorHandler(func(ctx httpx.Context, _ error) {
		_ = ctx.Text(http.StatusPaymentRequired, "httpx")
	}))
	e.HTTPErrorHandler = markerHandler(http.StatusTeapot)

	if resp := unmatchedResponse(t, engine); resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d: overriding after New did not win",
			resp.StatusCode, http.StatusTeapot)
	}
}

// A route's error is rendered inside the route in every configuration, so a
// native layer reading the response after next sees the error's status, as on
// ginx, rather than the 200 echo's HTTPErrorHandler would overwrite later.
func TestRouteErrorIsRenderedInsideUseNative(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"Default", nil},
		{"EnginesOwn", []Option{WithEngine(func() *echo.Echo {
			e := echo.New()
			e.HTTPErrorHandler = func(err error, c echo.Context) {
				DefaultErrorHandler(err, c)
			}
			return e
		}())}},
		{"WithErrorHandler", []Option{WithErrorHandler(func(ctx httpx.Context, err error) {
			status, body := httpx.RenderError(err)
			_ = ctx.JSON(status, body)
		})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New(tc.opts...)
			engine, ok := app.(*Engine)
			if !ok {
				t.Fatalf("New returned %T, want *Engine", app)
			}
			var seen int
			var seenErr error
			engine.UseNative(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					seenErr = next(c)
					seen = c.Response().Status
					return seenErr
				}
			})
			app.Group("").GET("/forbidden", func(httpx.Context) error {
				return httpx.NewForbiddenError("no")
			})
			requester, ok := httpx.AsTestRequester(app)
			if !ok {
				t.Fatal("echox engine is not a TestRequester")
			}
			resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "/forbidden", nil))
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
			if seen != http.StatusForbidden || seenErr != nil {
				t.Fatalf("UseNative saw status %d, err %v after next; want 403 and nil", seen, seenErr)
			}
		})
	}
}

// WithErrorHandler(nil) keeps the handler already configured, as on ginx and
// hertzx.
func TestWithErrorHandlerNilIsIgnored(t *testing.T) {
	engine := New(WithErrorHandler(func(ctx httpx.Context, _ error) {
		_ = ctx.Text(http.StatusPaymentRequired, "httpx")
	}), WithErrorHandler(nil))
	if resp := unmatchedResponse(t, engine); resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want %d: WithErrorHandler(nil) dropped the configured handler",
			resp.StatusCode, http.StatusPaymentRequired)
	}
}

// A route returning echo's own ErrNotFound is a route error, not an unmatched
// path: engine-scope middleware must not run a second time for it.
func TestRouteEchoNotFoundIsNotUnmatched(t *testing.T) {
	app := New()
	var runs int
	app.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			runs++
			return next(ctx)
		}
	})
	app.Group("").GET("/gone", func(httpx.Context) error {
		return echo.ErrNotFound
	})
	requester, ok := httpx.AsTestRequester(app)
	if !ok {
		t.Fatal("echox engine is not a TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "/gone", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || runs != 1 {
		t.Fatalf("status = %d, middleware runs = %d; want 404 and 1", resp.StatusCode, runs)
	}
}
