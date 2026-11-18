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
// suite rather than in the conformance module.
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

	// A header tag names a header the way HTTP does, case-insensitively: a
	// lowercase tag binds the canonical header a client sent.
	t.Run("BindHeaderTagIsCaseInsensitive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/bind/header-case", nil)
		req.Header.Set("X-Token", "present")
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/bind/header-case", func(ctx httpx.Context) error {
				var dst struct {
					Token string `header:"x-token"`
				}
				if err := ctx.BindHeader(&dst); err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, dst.Token)
			})
		}, req)
		if got.Status != http.StatusOK || got.Body != "present" {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "present")
		}
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

	// BindForm decodes the form body only; query values are BindQuery's.
	t.Run("BindFormIgnoresQuery", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/form/query?q=from-query", strings.NewReader("name=bob"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		got := r.serve(t, func(router httpx.Router) {
			router.POST("/form/query", func(ctx httpx.Context) error {
				var f struct {
					Name string `form:"name"`
					Q    string `form:"q"`
				}
				if err := ctx.BindForm(&f); err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, f.Name+"|"+f.Q)
			})
		}, req)
		if got.Status != http.StatusOK || got.Body != "bob|" {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "bob|")
		}
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

	// Multi-source sequential binding on one DTO must bind only fields carrying
	// an explicit tag for the active source; untagged and json-only fields must
	// neither be overwritten across sources nor fail parsing when an input
	// happens to share the Go field name.
	t.Run("BindExplicitTagsOnlyAndNoCrossSourceOverwrite", func(t *testing.T) {
		type embedded struct {
			Embedded string `query:"emb"`
			Role     string `json:"emb_role"`
		}
		type dto struct {
			embedded
			BodyOnly   string `json:"body_only"`
			BadInt     int    `json:"bad_int"`
			Name       string `query:",omitempty"`
			Token      string `header:"X-Token"`
			ParamOnly  string `uri:"param_only"`
			UntaggedID string
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"http://example.com/explicit/route-ok/smuggled-id?BodyOnly=smuggled&UntaggedID=smuggled&BadInt=not-an-int&Name=alice&emb=e1&Role=admin",
			strings.NewReader(`{"body_only":"legit","bad_int":7,"emb_role":"user"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Token", "tok-1")
		req.Header.Set("BodyOnly", "hdr-smuggled")
		req.Header.Set("BadInt", "not-an-int")
		req.Header.Set("UntaggedID", "hdr-smuggled")

		got := r.serve(t, func(router httpx.Router) {
			router.POST("/explicit/:param_only/:UntaggedID", func(ctx httpx.Context) error {
				var d dto
				if err := ctx.BindJSON(&d); err != nil {
					return err
				}
				if err := ctx.BindHeader(&d); err != nil {
					return err
				}
				if err := ctx.BindQuery(&d); err != nil {
					return err
				}
				if err := ctx.BindURI(&d); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"bodyOnly":   d.BodyOnly,
					"badInt":     d.BadInt,
					"name":       d.Name,
					"token":      d.Token,
					"paramOnly":  d.ParamOnly,
					"untaggedID": d.UntaggedID,
					"embedded":   d.Embedded,
					"role":       d.Role,
				})
			})
		}, req)
		if got.Status != http.StatusOK {
			t.Fatalf("status=%d body=%q, want 200", got.Status, got.Body)
		}
		wantSubstrs := []string{
			`"bodyOnly":"legit"`,
			`"badInt":7`,
			`"name":"alice"`,
			`"token":"tok-1"`,
			`"paramOnly":"route-ok"`,
			`"untaggedID":""`,
			`"embedded":"e1"`,
			`"role":"user"`,
		}
		for _, sub := range wantSubstrs {
			if !strings.Contains(got.Body, sub) {
				t.Fatalf("body=%q missing %s", got.Body, sub)
			}
		}

		// Same guarantee for BindForm (urlencoded).
		type formDTO struct {
			BodyOnly string `json:"body_only"`
			BadInt   int
			Title    string `form:"title"`
		}
		formReq := httptest.NewRequest(http.MethodPost, "http://example.com/explicit-form", strings.NewReader("title=ok&BodyOnly=smuggled&BadInt=not-an-int"))
		formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		formGot := r.serve(t, func(router httpx.Router) {
			router.POST("/explicit-form", func(ctx httpx.Context) error {
				f := formDTO{BodyOnly: "keep"}
				if err := ctx.BindForm(&f); err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, f.BodyOnly+"|"+f.Title)
			})
		}, formReq)
		if formGot.Status != http.StatusOK || formGot.Body != "keep|ok" {
			t.Fatalf("form status=%d body=%q, want 200 %q", formGot.Status, formGot.Body, "keep|ok")
		}
	})
}
