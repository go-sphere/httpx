package fiberx

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

type customFiberContext struct{ *fiber.DefaultCtx }

func (c *customFiberContext) Method(override ...string) string {
	return "custom:" + c.DefaultCtx.Method(override...)
}

func TestCustomContextPreserved(t *testing.T) {
	native := fiber.NewWithCustomCtx(func(app *fiber.App) fiber.CustomCtx {
		return &customFiberContext{DefaultCtx: fiber.NewDefaultCtx(app)}
	})
	engine := New(WithEngine(native))
	r := engine.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error {
			if c.Method() != "custom:GET" {
				t.Errorf("custom Method override lost: %q", c.Method())
			}
			if _, ok := httpx.AsNativeContext[*customFiberContext](c); !ok {
				t.Error("custom native context type lost")
			}
			c.Set("from-middleware", true)
			return next(c)
		}
	})
	r.GET("/custom", func(c httpx.Context) error {
		if c.Method() != "custom:GET" {
			t.Errorf("handler Method override lost: %q", c.Method())
		}
		if v, ok := c.Get("from-middleware"); !ok || v != true {
			t.Error("state not shared with handler")
		}
		return c.NoContent(http.StatusNoContent)
	})
	resp, err := native.Test(httptest.NewRequest(http.MethodGet, "/custom", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// The body is read as received: fiber's Body decodes by Content-Encoding (no
// other adapter decompresses) and writes a 415 into the response for an
// encoding it does not know, under the handler's feet.
func TestBodyReadsRawRequestBody(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"name":"zipped"}`))
	_ = zw.Close()

	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"unknown encoding", "foo", []byte(`{"name":"plain"}`)},
		{"gzip", "gzip", gz.Bytes()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := New()
			r := engine.Group("")
			var read, raw []byte
			var readErr, bindErr error
			r.POST("/reader", func(ctx httpx.Context) error {
				read, readErr = io.ReadAll(ctx.BodyReader())
				return ctx.Text(http.StatusOK, "handler")
			})
			r.POST("/raw", func(ctx httpx.Context) error {
				raw, _ = ctx.BodyRaw()
				return ctx.Text(http.StatusOK, "handler")
			})
			r.POST("/bind", func(ctx httpx.Context) error {
				var dst struct {
					Name string `json:"name"`
				}
				bindErr = ctx.BindJSON(&dst)
				return ctx.Text(http.StatusOK, "handler")
			})
			for _, path := range []string{"/reader", "/raw", "/bind"} {
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Content-Encoding", tc.encoding)
				resp, err := engine.(*Engine).Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK || string(body) != "handler" {
					t.Errorf("%s: status=%d body=%q, want the handler's 200 %q", path, resp.StatusCode, body, "handler")
				}
			}
			if readErr != nil || !bytes.Equal(read, tc.body) {
				t.Errorf("BodyReader = %q, %v; want the raw body %q", read, readErr, tc.body)
			}
			if !bytes.Equal(raw, tc.body) {
				t.Errorf("BodyRaw = %q, want the raw body %q", raw, tc.body)
			}
			// A gzip body is not JSON as received, so it fails to decode, as on
			// the net/http adapters; the plain one decodes.
			if wantErr := tc.encoding == "gzip"; (bindErr != nil) != wantErr {
				t.Errorf("BindJSON error = %v, want error: %v", bindErr, wantErr)
			}
		})
	}
}

// BindJSON decodes with the app's configured JSONDecoder, as fiber's own
// binder does.
func TestBindJSONUsesAppDecoder(t *testing.T) {
	var called bool
	app := fiber.New(fiber.Config{
		JSONDecoder: func(data []byte, v any) error {
			called = true
			return json.Unmarshal(data, v)
		},
	})
	engine := New(WithEngine(app))
	var name string
	engine.Group("").POST("/bind", func(ctx httpx.Context) error {
		var dst struct {
			Name string `json:"name"`
		}
		if err := ctx.BindJSON(&dst); err != nil {
			return err
		}
		name = dst.Name
		return ctx.NoContent(http.StatusNoContent)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/bind", strings.NewReader(`{"name":"n"}`)))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || !called || name != "n" {
		t.Fatalf("status=%d decoder called=%v name=%q", resp.StatusCode, called, name)
	}
}

func TestFiberxBodyRawSurvivesRequestCtxReuse(t *testing.T) {
	const (
		firstBody  = "body-of-the-first-request"
		secondBody = "body-of-the-second-reques" // same length, different bytes
	)
	app := fiber.New()
	engine := New(WithEngine(app))

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
		ctx.Request.Reset()
		ctx.Response.Reset()
		ctx.ResetUserValues()
		ctx.Request.Header.SetMethod(http.MethodPost)
		ctx.Request.SetRequestURI("http://example.com" + path)
		ctx.Request.SetBodyString(body)
		handler(ctx)
		if status := ctx.Response.StatusCode(); status != http.StatusOK {
			t.Fatalf("POST %s: status = %d, want 200", path, status)
		}
	}

	serve("/body/capture", firstBody)
	serve("/body", secondBody)

	if string(captured) != firstBody {
		t.Fatalf("body captured = %q, want %q", captured, firstBody)
	}
}

func TestBindMapsSurviveRequestCtxReuse(t *testing.T) {
	app := fiber.New()
	engine := New(WithEngine(app))

	var (
		qSingle, fSingle, hSingle map[string]string
		qMulti, fMulti, hMulti    map[string][]string
	)
	engine.Group("").POST("/capture", func(ctx httpx.Context) error {
		if err := ctx.BindQuery(&qSingle); err != nil {
			return err
		}
		if err := ctx.BindQuery(&qMulti); err != nil {
			return err
		}
		if err := ctx.BindForm(&fSingle); err != nil {
			return err
		}
		if err := ctx.BindForm(&fMulti); err != nil {
			return err
		}
		if err := ctx.BindHeader(&hSingle); err != nil {
			return err
		}
		if err := ctx.BindHeader(&hMulti); err != nil {
			return err
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	engine.Group("").POST("/other", func(ctx httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	handler := app.Handler()
	ctx := &fasthttp.RequestCtx{}
	send := func(uri, hdrVal, body string) {
		t.Helper()
		ctx.Request.Reset()
		ctx.Response.Reset()
		ctx.ResetUserValues()
		ctx.Request.Header.SetMethod(http.MethodPost)
		ctx.Request.SetRequestURI(uri)
		ctx.Request.Header.SetContentType("application/x-www-form-urlencoded")
		ctx.Request.Header.Set("X-Trace", hdrVal)
		ctx.Request.SetBodyString(body)
		handler(ctx)
		if ctx.Response.StatusCode() != http.StatusNoContent {
			t.Fatalf("POST %s status = %d", uri, ctx.Response.StatusCode())
		}
	}

	send("http://example.com/capture?k1=alpha&k2=bravo", "first-val", "f1=one&f2=two")
	send("http://example.com/other?k1=xxxxx&k2=yyyyy", "other-val", "f1=zzz&f2=www")

	if qSingle["k1"] != "alpha" || qSingle["k2"] != "bravo" {
		t.Fatalf("qSingle = %v, want k1=alpha k2=bravo", qSingle)
	}
	if !reflect.DeepEqual(qMulti["k1"], []string{"alpha"}) {
		t.Fatalf("qMulti = %v", qMulti)
	}
	if fSingle["f1"] != "one" || fSingle["f2"] != "two" {
		t.Fatalf("fSingle = %v, want f1=one f2=two", fSingle)
	}
	if !reflect.DeepEqual(fMulti["f2"], []string{"two"}) {
		t.Fatalf("fMulti = %v", fMulti)
	}
	if hSingle["X-Trace"] != "first-val" {
		t.Fatalf("hSingle = %v, want X-Trace=first-val", hSingle)
	}
	if !reflect.DeepEqual(hMulti["X-Trace"], []string{"first-val"}) {
		t.Fatalf("hMulti = %v", hMulti)
	}
}

func TestResponseHeaderValuesEmptyAndUnset(t *testing.T) {
	engine := New()
	var (
		emptyVal  []string
		clVal     []string
		trailerV  []string
		unsetVal  []string
		explicitC []string
	)
	engine.Group("").GET("/hdr", func(ctx httpx.Context) error {
		editor, ok := httpx.AsResponseHeaderEditor(ctx)
		if !ok {
			t.Fatal("missing ResponseHeaderEditor")
		}
		ctx.SetHeader("X-Empty", "")
		emptyVal = editor.ResponseHeaderValues("X-Empty")
		clVal = editor.ResponseHeaderValues("Content-Length")
		trailerV = editor.ResponseHeaderValues("Trailer")
		unsetVal = editor.ResponseHeaderValues("X-Unset")
		explicitC = editor.ResponseHeaderValues("Content-Type")
		return ctx.NoContent(http.StatusNoContent)
	})
	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "http://example.com/hdr", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !reflect.DeepEqual(emptyVal, []string{""}) {
		t.Fatalf("X-Empty = %#v, want []string{\"\"}", emptyVal)
	}
	if clVal != nil || trailerV != nil || unsetVal != nil || explicitC != nil {
		t.Fatalf("unset values = Content-Length:%#v Trailer:%#v X-Unset:%#v Content-Type:%#v, want nil",
			clVal, trailerV, unsetVal, explicitC)
	}
}
