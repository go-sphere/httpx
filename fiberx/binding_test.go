package fiberx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

// BindHeader hides the Host from fiber's binder by emptying it for the duration
// of the bind (fasthttp keeps Host in the header set, where the other four
// adapters have no Host header at all). The shared suite pins what the binder
// must not see; this pins the other half — that the request still knows its host
// afterwards, which only the native context can show.
func TestBindHeaderRestoresHost(t *testing.T) {
	engine := New()
	var (
		bound     string
		hostAfter string
		hostInMW  string
	)
	r := engine.Group("")
	// A layer above the handler must see the host unchanged too; Use composes
	// into the routes that follow it.
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			err := next(ctx)
			if fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx); ok {
				hostInMW = fc.Host()
			}
			return err
		}
	})
	r.GET("/bind/host", func(ctx httpx.Context) error {
		var dst struct {
			Host  string `header:"Host"`
			Trace string `header:"X-Trace-In"`
		}
		if err := ctx.BindHeader(&dst); err != nil {
			return err
		}
		bound = dst.Host
		if dst.Trace != "t-1" {
			t.Errorf("X-Trace-In bound as %q, want %q", dst.Trace, "t-1")
		}
		// app.Test serves on its own goroutine, so a failure is recorded here
		// and asserted after the request.
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			t.Error("native fiber context unavailable")
			return ctx.NoContent(http.StatusNoContent)
		}
		hostAfter = fc.Host()
		// A second bind must see the same header set as the first.
		var again struct {
			Host string `header:"Host"`
		}
		if err := ctx.BindHeader(&again); err != nil {
			return err
		}
		if again.Host != "" {
			t.Errorf("second BindHeader filled Host with %q", again.Host)
		}
		return ctx.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/bind/host", nil)
	req.Header.Set("X-Trace-In", "t-1")
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("engine does not implement httpx.TestRequester")
	}
	resp, err := requester.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if bound != "" {
		t.Fatalf("BindHeader filled header:\"Host\" with %q, want unset", bound)
	}
	if hostAfter != "example.com" {
		t.Fatalf("Host() = %q after BindHeader, want %q: the bind did not restore it", hostAfter, "example.com")
	}
	if hostInMW != "example.com" {
		t.Fatalf("Host() = %q in a layer above the handler, want %q", hostInMW, "example.com")
	}
}
