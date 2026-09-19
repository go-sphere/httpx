package stdx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// FromStd is this adapter's FromGin/FromHertz: net/http's request pair is the
// native context here, so an httpx.Context can be built from it without an
// Engine. The detached context has no route and no trusted-proxy policy.
func TestFromStd(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/from?a=1", nil)
	req.RemoteAddr = "203.0.113.9:4444"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	ctx := FromStd(rr, req)

	if ctx.Method() != http.MethodPost {
		t.Fatalf("Method = %q", ctx.Method())
	}
	if ctx.Query("a") != "1" {
		t.Fatalf("Query(a) = %q", ctx.Query("a"))
	}
	if got := ctx.FullPath(); got != "" {
		t.Fatalf("FullPath = %q, want empty on a detached context", got)
	}
	if got := ctx.Param("id"); got != "" {
		t.Fatalf("Param(id) = %q, want empty on a detached context", got)
	}
	// No Engine means no configured trusted proxies, so a forwarding header
	// must not be believed.
	if got := ctx.ClientIP(); got != "203.0.113.9" {
		t.Fatalf("ClientIP = %q, want the peer address", got)
	}
	if err := ctx.Next(); err != nil {
		t.Fatalf("Next on a detached context = %v, want nil", err)
	}
	if err := ctx.JSON(http.StatusTeapot, map[string]string{"from": "std"}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Body.String(); got != `{"from":"std"}` {
		t.Fatalf("body = %q", got)
	}
}

// The native escape hatch works on a detached context too: it is the same
// http.ResponseWriter/*http.Request pair that went in.
func TestFromStdNativeContext(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	native, ok := httpx.AsNativeContext[*Native](FromStd(rr, req))
	if !ok {
		t.Fatal("FromStd context does not expose a native context")
	}
	if native.Request() != req {
		t.Fatal("native request is not the one passed to FromStd")
	}
}

// FromStd is new API, so the escape hatch's degradation is a stated contract
// rather than something a caller discovers by panicking. Engine is the only
// method on *Native that has no answer without an Engine; every other one works
// on the pair that went in, so all of them are exercised here and the nil is
// asserted instead of left to be met at a call site.
func TestFromStdNativeDegradation(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	native, ok := httpx.AsNativeContext[*Native](FromStd(rr, req))
	if !ok {
		t.Fatal("FromStd context does not expose a native context")
	}

	// The documented nil. A caller must check this, so it must not become a
	// stand-in engine that answers for routes and proxies that do not exist.
	if engine := native.Engine(); engine != nil {
		t.Fatalf("Engine() = %v, want nil on a detached context", engine)
	}

	if native.ResponseWriter() == nil {
		t.Fatal("ResponseWriter() is nil")
	}
	if w, r := native.Unwrap(); w == nil || r != req {
		t.Fatalf("Unwrap() = (%v, %v), want the writer wrapper and the request", w, r)
	}
	if native.Written() {
		t.Fatal("Written() is true before anything was written")
	}

	swapped := httptest.NewRequest(http.MethodPut, "/swapped", nil)
	native.SetRequest(swapped)
	if native.Request() != swapped {
		t.Fatal("SetRequest did not take effect")
	}
	native.SetWriter(httptest.NewRecorder())
	if native.ResponseWriter() == nil {
		t.Fatal("ResponseWriter() is nil after SetWriter")
	}
	native.MarkWritten(http.StatusTeapot)
	if !native.Written() {
		t.Fatal("MarkWritten did not take effect")
	}
}

// DefaultErrorHandler on stdx is httpx-shaped because net/http has no error
// handler of its own — the naming rule ("this adapter's default handler in its
// native shape") lands on httpx.ErrorHandler here.
func TestDefaultErrorHandlerIsHTTPXShape(t *testing.T) {
	conf := NewConfig(WithErrorHandler(DefaultErrorHandler))
	if conf.errHandler == nil {
		t.Fatal("DefaultErrorHandler is not usable as httpx.ErrorHandler")
	}
}
