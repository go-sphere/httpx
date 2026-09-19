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

// BodyRaw must return a caller-owned slice that stays valid after the handler
// returns, so the fasthttp-based adapters copy out of a pooled request buffer.
// httpxtest cannot catch an adapter that aliases: its in-process requesters
// build a fresh native context per request, so reuse has to be driven here.

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
	// Second request on the same RequestCtx refills what the first BodyRaw read.
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
	// Second request on the same RequestContext refills what the first BodyRaw read.
	rc.ResetWithoutConn()
	serve("/body", secondBody)

	if string(captured) != firstBody {
		t.Fatalf("body captured in the first request = %q, want %q: hertzx.BodyRaw returned a view into hertz's pooled buffer",
			captured, firstBody)
	}
}
