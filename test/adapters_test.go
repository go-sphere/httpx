package test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
)

type adapter struct {
	name string
	new  func() httpx.Engine
}

func adapters() []adapter {
	errHandler := func(ctx httpx.Context, err error) {
		if err == nil {
			return
		}
		_ = ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	gin.SetMode(gin.TestMode)

	return []adapter{
		{
			name: "gin",
			new: func() httpx.Engine {
				return ginx.New(httpx.WithErrorHandler[*gin.Engine](errHandler))
			},
		},
		{
			name: "fiber",
			new: func() httpx.Engine {
				return fiberx.New(httpx.WithErrorHandler[*fiber.App](errHandler))
			},
		},
		{
			name: "hertz",
			new: func() httpx.Engine {
				return hertzx.New(httpx.WithErrorHandler[*server.Hertz](errHandler))
			},
		},
	}
}

func serveAdapter(t *testing.T, ad adapter, register func(httpx.Router)) string {
	t.Helper()
	engine := ad.new()
	register(engine)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server.URL
}

func client() *http.Client {
	return &http.Client{Timeout: 3 * time.Second}
}

func decodeResponse(t *testing.T, r io.Reader, dst any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertStatus(t *testing.T, resp *http.Response, code int) {
	t.Helper()
	if resp.StatusCode != code {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, code)
	}
}

type requestSnapshot struct {
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	FullPath   string              `json:"full_path"`
	Param      string              `json:"param"`
	Params     map[string]string   `json:"params"`
	Query      string              `json:"query"`
	Queries    map[string][]string `json:"queries"`
	FormValue  string              `json:"form_value"`
	FormValues map[string][]string `json:"form_values"`
	Header     string              `json:"header"`
	Cookie     string              `json:"cookie"`
}

func assertRequestSnapshot(t *testing.T, got, expected requestSnapshot) {
	t.Helper()
	if got.Method != expected.Method ||
		got.Path != expected.Path ||
		got.FullPath != expected.FullPath ||
		got.Param != expected.Param ||
		got.Query != expected.Query ||
		got.FormValue != expected.FormValue ||
		got.Header != expected.Header ||
		got.Cookie != expected.Cookie ||
		!reflect.DeepEqual(got.Params, expected.Params) ||
		!reflect.DeepEqual(got.Queries, expected.Queries) ||
		!reflect.DeepEqual(got.FormValues, expected.FormValues) {
		t.Fatalf("request snapshot mismatch:\nexpected: %+v\n     got: %+v", expected, got)
	}
}

func captureRequestHandler() httpx.Handler {
	return func(ctx httpx.Context) error {
		session, _ := ctx.Cookie("session")
		snapshot := requestSnapshot{
			Method:     ctx.Method(),
			Path:       ctx.Path(),
			FullPath:   ctx.FullPath(),
			Param:      ctx.Param("id"),
			Params:     ctx.Params(),
			Query:      ctx.Query("tag"),
			Queries:    ctx.Queries(),
			FormValue:  ctx.FormValue("title"),
			FormValues: ctx.FormValues(),
			Header:     ctx.Header("X-Color"),
			Cookie:     session,
		}
		return ctx.JSON(http.StatusOK, snapshot)
	}
}

func TestRequestAccessors(t *testing.T) {
	for _, ad := range adapters() {
		ad := ad
		t.Run(ad.name, func(t *testing.T) {
			t.Parallel()
			base := serveAdapter(t, ad, func(r httpx.Router) {
				r.Handle(http.MethodPost, "/books/:id", captureRequestHandler())
			})

			form := url.Values{}
			form.Set("title", "go")
			req, err := http.NewRequest(http.MethodPost, base+"/books/42?tag=go", strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("X-Color", "blue")
			req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

			resp, err := client().Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			assertStatus(t, resp, http.StatusOK)

			var got requestSnapshot
			decodeResponse(t, resp.Body, &got)

			expected := requestSnapshot{
				Method:     http.MethodPost,
				Path:       "/books/42",
				FullPath:   "/books/:id",
				Param:      "42",
				Params:     map[string]string{"id": "42"},
				Query:      "go",
				Queries:    map[string][]string{"tag": {"go"}},
				FormValue:  "go",
				FormValues: map[string][]string{"title": {"go"}},
				Header:     "blue",
				Cookie:     "abc",
			}

			assertRequestSnapshot(t, got, expected)
		})
	}
}

type bindPayload struct {
	Name string `json:"name" form:"name" query:"name" uri:"name" path:"name" header:"X-Name"`
	Age  int    `json:"age" form:"age" query:"age" uri:"age" path:"age" header:"X-Age"`
}

func TestBinders(t *testing.T) {
	for _, ad := range adapters() {
		ad := ad
		t.Run(ad.name, func(t *testing.T) {
			t.Parallel()
			base := serveAdapter(t, ad, func(r httpx.Router) {
				r.Handle(http.MethodPost, "/bind/json", func(ctx httpx.Context) error {
					var p bindPayload
					if err := ctx.BindJSON(&p); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, p)
				})
				r.Handle(http.MethodGet, "/bind/query", func(ctx httpx.Context) error {
					var p bindPayload
					if err := ctx.BindQuery(&p); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, p)
				})
				r.Handle(http.MethodPost, "/bind/form", func(ctx httpx.Context) error {
					var p bindPayload
					if err := ctx.BindForm(&p); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, p)
				})
				r.Handle(http.MethodGet, "/bind/uri/:name/:age", func(ctx httpx.Context) error {
					var p bindPayload
					if err := ctx.BindURI(&p); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, p)
				})
				r.Handle(http.MethodGet, "/bind/header", func(ctx httpx.Context) error {
					var p bindPayload
					if err := ctx.BindHeader(&p); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, p)
				})
			})

			t.Run("json", func(t *testing.T) {
				payload := `{"name":"alice","age":30}`
				resp, err := client().Post(base+"/bind/json", "application/json", strings.NewReader(payload))
				if err != nil {
					t.Fatalf("do json request: %v", err)
				}
				defer resp.Body.Close()

				assertStatus(t, resp, http.StatusOK)

				var got bindPayload
				decodeResponse(t, resp.Body, &got)
				expected := bindPayload{Name: "alice", Age: 30}
				if got != expected {
					t.Fatalf("json bind mismatch: expected %+v got %+v", expected, got)
				}
			})

			t.Run("query", func(t *testing.T) {
				resp, err := client().Get(base + "/bind/query?name=bob&age=25")
				if err != nil {
					t.Fatalf("do query request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)

				var got bindPayload
				decodeResponse(t, resp.Body, &got)
				expected := bindPayload{Name: "bob", Age: 25}
				if got != expected {
					t.Fatalf("query bind mismatch: expected %+v got %+v", expected, got)
				}
			})

			t.Run("form", func(t *testing.T) {
				form := url.Values{}
				form.Set("name", "carol")
				form.Set("age", "28")
				resp, err := client().Post(base+"/bind/form", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
				if err != nil {
					t.Fatalf("do form request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)

				var got bindPayload
				decodeResponse(t, resp.Body, &got)
				expected := bindPayload{Name: "carol", Age: 28}
				if got != expected {
					t.Fatalf("form bind mismatch: expected %+v got %+v", expected, got)
				}
			})

			t.Run("uri", func(t *testing.T) {
				resp, err := client().Get(base + "/bind/uri/dave/41")
				if err != nil {
					t.Fatalf("do uri request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)

				var got bindPayload
				decodeResponse(t, resp.Body, &got)
				expected := bindPayload{Name: "dave", Age: 41}
				if got != expected {
					t.Fatalf("uri bind mismatch: expected %+v got %+v", expected, got)
				}
			})

			t.Run("header", func(t *testing.T) {
				req, err := http.NewRequest(http.MethodGet, base+"/bind/header", nil)
				if err != nil {
					t.Fatalf("build header request: %v", err)
				}
				req.Header.Set("X-Name", "erin")
				req.Header.Set("X-Age", "22")

				resp, err := client().Do(req)
				if err != nil {
					t.Fatalf("do header request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)

				var got bindPayload
				decodeResponse(t, resp.Body, &got)
				expected := bindPayload{Name: "erin", Age: 22}
				if got != expected {
					t.Fatalf("header bind mismatch: expected %+v got %+v", expected, got)
				}
			})
		})
	}
}

func TestResponder(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("file-body"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	for _, ad := range adapters() {
		ad := ad
		t.Run(ad.name, func(t *testing.T) {
			t.Parallel()
			base := serveAdapter(t, ad, func(r httpx.Router) {
				r.Handle(http.MethodGet, "/respond/json", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusCreated, map[string]string{"msg": "ok"})
				})
				r.Handle(http.MethodGet, "/respond/text", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusAccepted, "hello")
				})
				r.Handle(http.MethodGet, "/respond/bytes", func(ctx httpx.Context) error {
					return ctx.Bytes(http.StatusOK, []byte{0x01, 0x02}, "application/octet-stream")
				})
				r.Handle(http.MethodGet, "/respond/stream", func(ctx httpx.Context) error {
					return ctx.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
						_, err := w.Write([]byte("streaming"))
						return err
					})
				})
				r.Handle(http.MethodGet, "/respond/file", func(ctx httpx.Context) error {
					return ctx.File(filePath)
				})
				r.Handle(http.MethodGet, "/respond/redirect", func(ctx httpx.Context) error {
					return ctx.Redirect(http.StatusFound, "/target")
				})
				r.Handle(http.MethodGet, "/respond/header-cookie", func(ctx httpx.Context) error {
					ctx.SetHeader("X-Test", "value")
					ctx.SetCookie(&http.Cookie{Name: "choco", Value: "chip"})
					ctx.Status(http.StatusNoContent)
					return nil
				})
			})

			t.Run("json", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/json")
				if err != nil {
					t.Fatalf("json request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusCreated)

				var body map[string]string
				decodeResponse(t, resp.Body, &body)
				if body["msg"] != "ok" {
					t.Fatalf("json body mismatch: %+v", body)
				}
			})

			t.Run("text", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/text")
				if err != nil {
					t.Fatalf("text request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusAccepted)
				data, _ := io.ReadAll(resp.Body)
				if string(data) != "hello" {
					t.Fatalf("text body mismatch: %q", string(data))
				}
			})

			t.Run("bytes", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/bytes")
				if err != nil {
					t.Fatalf("bytes request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)
				if resp.Header.Get("Content-Type") != "application/octet-stream" {
					t.Fatalf("content type mismatch: %s", resp.Header.Get("Content-Type"))
				}
				data, _ := io.ReadAll(resp.Body)
				if string(data) != "\x01\x02" {
					t.Fatalf("bytes mismatch: %v", data)
				}
			})

			t.Run("stream", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/stream")
				if err != nil {
					t.Fatalf("stream request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)
				if ct := resp.Header.Get("Content-Type"); ct != "" && ct != "text/plain" {
					t.Fatalf("unexpected content type: %s", ct)
				}
				data, _ := io.ReadAll(resp.Body)
				if string(data) != "streaming" {
					t.Fatalf("stream body mismatch: %q", string(data))
				}
			})

			t.Run("file", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/file")
				if err != nil {
					t.Fatalf("file request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)
				data, _ := io.ReadAll(resp.Body)
				if string(data) != "file-body" {
					t.Fatalf("file body mismatch: %q", string(data))
				}
			})

			t.Run("redirect", func(t *testing.T) {
				cl := &http.Client{
					Timeout: 3 * time.Second,
					CheckRedirect: func(req *http.Request, via []*http.Request) error {
						return http.ErrUseLastResponse
					},
				}
				resp, err := cl.Get(base + "/respond/redirect")
				if err != nil {
					t.Fatalf("redirect request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusFound)
				if loc := resp.Header.Get("Location"); loc != "/target" {
					t.Fatalf("location mismatch: %s", loc)
				}
			})

			t.Run("header and cookie", func(t *testing.T) {
				resp, err := client().Get(base + "/respond/header-cookie")
				if err != nil {
					t.Fatalf("header-cookie request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusNoContent)

				if resp.Header.Get("X-Test") != "value" {
					t.Fatalf("header not set: %+v", resp.Header)
				}
				foundCookie := false
				for _, c := range resp.Cookies() {
					if c.Name == "choco" && c.Value == "chip" {
						foundCookie = true
						break
					}
				}
				if !foundCookie {
					t.Fatalf("cookie not found in response")
				}
			})
		})
	}
}

func TestRouterFeatures(t *testing.T) {
	for _, ad := range adapters() {
		ad := ad
		t.Run(ad.name, func(t *testing.T) {
			t.Parallel()
			base := serveAdapter(t, ad, func(r httpx.Router) {
				r.Use(func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						ctx.SetHeader("X-Global", "yes")
						ctx.Set("trace", "global")
						return next(ctx)
					}
				})

				api := r.Group("/api", func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						ctx.SetHeader("X-Group", "yes")
						return next(ctx)
					}
				})
				api.Handle(http.MethodGet, "/ping", func(ctx httpx.Context) error {
					trace, _ := ctx.Get("trace")
					return ctx.JSON(http.StatusOK, map[string]any{
						"method": ctx.Method(),
						"trace":  trace,
					})
				})

				guarded := r.Group("/", func(_ httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						ctx.AbortWithStatus(http.StatusTeapot)
						return nil
					}
				})
				guarded.Handle(http.MethodGet, "/abort", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "should not run")
				})

				r.Any("/anything", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, ctx.Method())
				})

				r.Mount("/legacy", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("X-Legacy", "ok")
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("legacy"))
				}))
			})

			t.Run("group and middleware", func(t *testing.T) {
				resp, err := client().Get(base + "/api/ping")
				if err != nil {
					t.Fatalf("ping request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)
				if resp.Header.Get("X-Global") != "yes" || resp.Header.Get("X-Group") != "yes" {
					t.Fatalf("missing middleware headers: %+v", resp.Header)
				}
				var body map[string]any
				decodeResponse(t, resp.Body, &body)
				if body["method"] != http.MethodGet || body["trace"] != "global" {
					t.Fatalf("unexpected body: %+v", body)
				}
			})

			t.Run("abort stops handler", func(t *testing.T) {
				resp, err := client().Get(base + "/abort")
				if err != nil {
					t.Fatalf("abort request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusTeapot)
				body, _ := io.ReadAll(resp.Body)
				if len(body) != 0 {
					t.Fatalf("handler should not run, got body: %q", string(body))
				}
			})

			t.Run("any handles different methods", func(t *testing.T) {
				req, err := http.NewRequest(http.MethodPatch, base+"/anything", nil)
				if err != nil {
					t.Fatalf("build any request: %v", err)
				}
				resp, err := client().Do(req)
				if err != nil {
					t.Fatalf("any request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusOK)
				data, _ := io.ReadAll(resp.Body)
				if string(data) != http.MethodPatch {
					t.Fatalf("unexpected any body: %q", string(data))
				}
			})

			t.Run("mount http.Handler", func(t *testing.T) {
				resp, err := client().Get(base + "/legacy/hello")
				if err != nil {
					t.Fatalf("mount request: %v", err)
				}
				defer resp.Body.Close()
				assertStatus(t, resp, http.StatusCreated)
				if resp.Header.Get("X-Legacy") != "ok" {
					t.Fatalf("missing legacy header: %+v", resp.Header)
				}
				data, _ := io.ReadAll(resp.Body)
				if string(data) != "legacy" {
					t.Fatalf("unexpected mount body: %q", string(data))
				}
			})
		})
	}
}
