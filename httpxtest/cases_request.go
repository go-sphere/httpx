package httpxtest

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Request", casesRequest)
}

// Request-side cases: bodies, the five binders, forms and uploads. Each
// framework has its own body reader and binder, so these belong in the shared
// suite rather than in a four-framework test module.
func casesRequest(t *testing.T, r runner) {
	t.Run("BodyRaw", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/body/raw", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(raw))
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/body/raw", strings.NewReader(`{"name":"alice"}`)))
	})

	t.Run("BodyReader", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/body/reader", func(ctx httpx.Context) error {
				body, err := io.ReadAll(ctx.BodyReader())
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(body))
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/body/reader", strings.NewReader("reader-body")))
	})

	// BodyRaw must not consume the body for the binder that runs after it.
	t.Run("BodyRawThenBindJSON", func(t *testing.T) {
		type payload struct {
			Name string `json:"name"`
		}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/body/raw-then-bind", strings.NewReader(`{"name":"carol"}`))
		req.Header.Set("Content-Type", "application/json")
		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/body/raw-then-bind", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				var p payload
				if err := ctx.BindJSON(&p); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{"raw": string(raw), "name": p.Name})
			})
		}, req)
	})

	// The BodyAccess contract says the caller owns the returned slice, so it must
	// stay valid after the request. No in-process requester reuses a framework
	// buffer, so the case that actually forces reuse needs native types and lives
	// in conformance/body_ownership_conformance_test.go.
	t.Run("BodyRawOutlivesRequest", func(t *testing.T) {
		const first = "body-of-the-first-request"
		const second = "body-of-the-second-reques" // same length, different bytes
		var captured []byte

		engine := r.suite.NewEngine(t, Options{})
		engine.Group("").POST("/body/outlives", func(ctx httpx.Context) error {
			raw, err := ctx.BodyRaw()
			if err != nil {
				return err
			}
			if captured == nil {
				captured = raw
			}
			return ctx.Text(http.StatusOK, strconv.Itoa(len(raw)))
		})
		requester, ok := httpx.AsTestRequester(engine)
		if !ok {
			t.Fatalf("%s: engine does not support httpx.TestRequester", r.suite.Name)
		}
		for _, body := range []string{first, second} {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/body/outlives", strings.NewReader(body))
			resp, err := requester.Do(req)
			if err != nil {
				t.Fatalf("%s: serve %q: %v", r.suite.Name, body, err)
			}
			_ = resp.Body.Close()
		}
		if string(captured) != first {
			t.Fatalf("%s: body captured in the first request = %q, want %q: BodyRaw returned a view into a buffer the framework reused",
				r.suite.Name, captured, first)
		}
	})

	t.Run("BindJSONQueryURIHeader", func(t *testing.T) {
		type payload struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		type query struct {
			Active bool `query:"active"`
		}
		type uri struct {
			ID string `uri:"id"`
		}
		type header struct {
			Token string `header:"X-Token"`
		}

		req := httptest.NewRequest(http.MethodPost, "http://example.com/bind/7?active=true", strings.NewReader(`{"name":"tom","age":11}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Token", "token-1")

		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/bind/:id", func(ctx httpx.Context) error {
				var p payload
				var q query
				var u uri
				var h header
				if err := ctx.BindJSON(&p); err != nil {
					return err
				}
				if err := ctx.BindQuery(&q); err != nil {
					return err
				}
				if err := ctx.BindURI(&u); err != nil {
					return err
				}
				if err := ctx.BindHeader(&h); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"name":   p.Name,
					"age":    p.Age,
					"active": q.Active,
					"id":     u.ID,
					"token":  h.Token,
				})
			})
		}, req)
	})

	t.Run("BindFormAndFormValue", func(t *testing.T) {
		type form struct {
			Name string `form:"name"`
			Age  int    `form:"age"`
		}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/form/8", strings.NewReader("name=bob&age=12"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/form/:id", func(ctx httpx.Context) error {
				var f form
				if err := ctx.BindForm(&f); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"name":      f.Name,
					"age":       f.Age,
					"formValue": ctx.FormValue("name"),
					"id":        ctx.Param("id"),
				})
			})
		}, req)
	})

	t.Run("MultipartAndFormFile", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("title", "sample"); err != nil {
			t.Fatalf("write field: %v", err)
		}
		part, err := writer.CreateFormFile("file", "a.txt")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write([]byte("hello")); err != nil {
			t.Fatalf("write file part: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close writer: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/upload", func(ctx httpx.Context) error {
				f, err := ctx.FormFile("file")
				if err != nil {
					return err
				}
				mf, err := ctx.MultipartForm()
				if err != nil {
					return err
				}
				count := 0
				if mf != nil {
					count = len(mf.Value["title"])
				}
				opened, err := f.Open()
				if err != nil {
					return err
				}
				defer func() { _ = opened.Close() }()
				content, err := io.ReadAll(opened)
				if err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"filename": f.Filename,
					"size":     f.Size,
					"content":  string(content),
					"title":    ctx.FormValue("title"),
					"count":    count,
				})
			})
		}, req)
	})

	t.Run("Cookies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/request/cookies", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
		req.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})

		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/request/cookies", func(ctx httpx.Context) error {
				session, err := ctx.Cookie("session")
				if err != nil {
					return err
				}
				_, missingErr := ctx.Cookie("nope")
				return ctx.JSON(http.StatusOK, map[string]any{
					"session":      session,
					"cookies":      ctx.Cookies(),
					"missingIsErr": missingErr != nil,
				})
			})
		}, req)
	})

	// The whole request surface in one response, so a change to any accessor is a
	// contract diff.
	t.Run("RequestInfo", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/request/info/42?a=1&b=2", nil)
		req.Header.Set("X-Custom", "custom-value")

		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/request/info/:id", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"method":   ctx.Method(),
					"path":     ctx.Path(),
					"fullPath": ctx.FullPath(),
					"param":    ctx.Param("id"),
					"params":   ctx.Params(),
					"query":    ctx.Query("a"),
					"queries":  ctx.Queries(),
					"rawQuery": ctx.RawQuery(),
					"header":   ctx.Header("X-Custom"),
				})
			})
		}, req)
	})
}
