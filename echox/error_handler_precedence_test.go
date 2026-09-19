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
