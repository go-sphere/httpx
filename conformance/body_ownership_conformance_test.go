package conformance

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// BodyRaw returns a slice the caller owns (httpx.BodyAccess): it must stay
// valid after the handler returns. The adapters over fasthttp-based frameworks
// have to copy for that, because the framework serves the body out of a pooled
// buffer that the next request on the same connection refills.
//
// This cannot be asserted from httpxtest: every in-process requester builds a
// fresh native context per request, so nothing is ever reused and an aliasing
// adapter passes. Forcing the reuse needs the framework's own types, which is
// why these two live here.

const (
	firstBody  = "body-of-the-first-request"
	secondBody = "body-of-the-second-reques" // same length, different bytes
)

func TestFiberxBodyRawSurvivesRequestCtxReuse(t *testing.T) {
	app := fiber.New()
	engine := fiberx.New(fiberx.WithEngine(app))

	var captured []byte
	engine.Group("").POST("/body/capture", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		captured = raw
		return ctx.Text(http.StatusOK, string(raw))
	})
	engine.Group("").POST("/body", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		return ctx.Text(http.StatusOK, string(raw))
	})

	handler := app.Handler()
	ctx := &fasthttp.RequestCtx{}
	serve := func(path, body string) {
		t.Helper()
		ctx.Request.Header.SetMethod(http.MethodPost)
		ctx.Request.SetRequestURI("http://example.com" + path)
		ctx.Request.SetBodyString(body)
		handler(ctx)
		if status := ctx.Response.StatusCode(); status != http.StatusOK {
			t.Fatalf("POST %s: status = %d, want 200", path, status)
		}
	}

	serve("/body/capture", firstBody)
	// Same RequestCtx, second request: fasthttp refills the very buffer the
	// first BodyRaw read from, which is what a keep-alive connection does.
	ctx.Response.Reset()
	ctx.ResetUserValues()
	serve("/body", secondBody)

	if string(captured) != firstBody {
		t.Fatalf("body captured in the first request = %q, want %q: fiberx.BodyRaw returned a view into fasthttp's pooled buffer",
			captured, firstBody)
	}
}

func TestHertzxBodyRawSurvivesRequestContextReuse(t *testing.T) {
	native := server.New()
	engine := hertzx.New(hertzx.WithEngine(native))

	var captured []byte
	engine.Group("").POST("/body/capture", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		captured = raw
		return ctx.Text(http.StatusOK, string(raw))
	})
	engine.Group("").POST("/body", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		return ctx.Text(http.StatusOK, string(raw))
	})

	rc := native.NewContext()
	ctx := context.Background()
	serve := func(path, body string) {
		t.Helper()
		rc.Request.Header.SetMethod(http.MethodPost)
		rc.Request.SetRequestURI("http://example.com" + path)
		rc.Request.SetBodyString(body)
		native.ServeHTTP(ctx, rc)
		if status := rc.Response.StatusCode(); status != http.StatusOK {
			t.Fatalf("POST %s: status = %d, want 200", path, status)
		}
	}

	serve("/body/capture", firstBody)
	// Same RequestContext, second request: hertz refills the very buffer the
	// first BodyRaw read from.
	rc.ResetWithoutConn()
	serve("/body", secondBody)

	if string(captured) != firstBody {
		t.Fatalf("body captured in the first request = %q, want %q: hertzx.BodyRaw returned a view into hertz's pooled buffer",
			captured, firstBody)
	}
}
