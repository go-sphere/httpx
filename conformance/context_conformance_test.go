package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func TestRequestInfoConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/req/:id", func(ctx httpx.Context) error {
			_, missingErr := ctx.Cookie("missing")
			return ctx.JSON(200, map[string]any{
				"method":          ctx.Method(),
				"path":            ctx.Path(),
				"fullPath":        ctx.FullPath(),
				"param":           ctx.Param("id"),
				"params":          ctx.Params(),
				"query":           ctx.Query("name"),
				"queries":         ctx.Queries(),
				"rawQuery":        ctx.RawQuery(),
				"header":          ctx.Header("X-Test"),
				"headers":         ctx.Headers(),
				"cookie":          mustCookie(ctx, "session"),
				"cookies":         ctx.Cookies(),
				"hasMissingError": missingErr != nil,
				"hasClientIP":     ctx.ClientIP() != "",
			})
		})
	}, func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/req/42?name=alice&name=bob&age=18", nil)
		req.Header.Set("X-Test", "v1")
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
		return req
	})

	assertMatchesGin(t, results)
}

func TestBodyFormAndBinderConformance(t *testing.T) {
	t.Run("BodyRaw", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.POST("/body/raw", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(200, string(raw))
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "http://example.com/body/raw", strings.NewReader(`{"name":"alice"}`))
		})
		assertMatchesGin(t, results)
	})

	t.Run("BodyReader", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.POST("/body/reader", func(ctx httpx.Context) error {
				body, err := io.ReadAll(ctx.BodyReader())
				if err != nil {
					return err
				}
				return ctx.Text(200, string(body))
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "http://example.com/body/reader", strings.NewReader("reader-body"))
		})
		assertMatchesGin(t, results)
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

		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.POST("/bind/:id", func(ctx httpx.Context) error {
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
				return ctx.JSON(200, map[string]any{
					"name":   p.Name,
					"age":    p.Age,
					"active": q.Active,
					"id":     u.ID,
					"token":  h.Token,
				})
			})
		}, func() *http.Request {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/bind/7?active=true", strings.NewReader(`{"name":"tom","age":11}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Token", "token-1")
			return req
		})
		assertMatchesGin(t, results)
	})

	t.Run("BindFormAndFormValue", func(t *testing.T) {
		type form struct {
			Name string `form:"name"`
			Age  int    `form:"age"`
		}

		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.POST("/form/:id", func(ctx httpx.Context) error {
				var f form
				if err := ctx.BindForm(&f); err != nil {
					return err
				}
				return ctx.JSON(200, map[string]any{
					"name":      f.Name,
					"age":       f.Age,
					"formValue": ctx.FormValue("name"),
					"id":        ctx.Param("id"),
				})
			})
		}, func() *http.Request {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/form/8", strings.NewReader("name=bob&age=12"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return req
		})
		assertMatchesGin(t, results)
	})

	t.Run("MultipartAndFormFile", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.POST("/upload", func(ctx httpx.Context) error {
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
				return ctx.JSON(200, map[string]any{
					"filename": f.Filename,
					"title":    ctx.FormValue("title"),
					"count":    count,
				})
			})
		}, func() *http.Request {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			_ = writer.WriteField("title", "sample")
			part, _ := writer.CreateFormFile("file", "a.txt")
			_, _ = part.Write([]byte("hello"))
			_ = writer.Close()

			req := httptest.NewRequest(http.MethodPost, "http://example.com/upload", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			return req
		})
		assertMatchesGin(t, results)
	})
}

func TestResponderConformance(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(filePath, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	tests := []struct {
		name     string
		register func(httpx.Router)
		request  func() *http.Request
	}{
		{
			name: "Status",
			register: func(r httpx.Router) {
				r.GET("/status", func(ctx httpx.Context) error {
					ctx.Status(202)
					return nil
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/status", nil) },
		},
		{
			name: "JSON",
			register: func(r httpx.Router) {
				r.GET("/json", func(ctx httpx.Context) error {
					return ctx.JSON(201, map[string]any{"ok": true})
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/json", nil) },
		},
		{
			name: "Text",
			register: func(r httpx.Router) {
				r.GET("/text", func(ctx httpx.Context) error {
					return ctx.Text(202, "hello")
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/text", nil) },
		},
		{
			name: "NoContent",
			register: func(r httpx.Router) {
				r.GET("/nocontent", func(ctx httpx.Context) error {
					return ctx.NoContent(204)
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/nocontent", nil) },
		},
		{
			name: "Bytes",
			register: func(r httpx.Router) {
				r.GET("/bytes", func(ctx httpx.Context) error {
					return ctx.Bytes(200, []byte("abc"), "application/octet-stream")
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/bytes", nil) },
		},
		{
			name: "DataFromReader",
			register: func(r httpx.Router) {
				r.GET("/reader", func(ctx httpx.Context) error {
					return ctx.DataFromReader(200, "text/plain", strings.NewReader("stream"), 6)
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/reader", nil) },
		},
		{
			name: "File",
			register: func(r httpx.Router) {
				r.GET("/file", func(ctx httpx.Context) error {
					return ctx.File(filePath)
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/file", nil) },
		},
		{
			name: "Redirect",
			register: func(r httpx.Router) {
				r.GET("/redirect", func(ctx httpx.Context) error {
					return ctx.Redirect(302, "/to")
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/redirect", nil) },
		},
		{
			name: "HeaderAndCookie",
			register: func(r httpx.Router) {
				r.GET("/cookie", func(ctx httpx.Context) error {
					ctx.SetHeader("X-Trace", "ok")
					ctx.SetCookie(&http.Cookie{Name: "session", Value: "abc", Path: "/"})
					return ctx.Text(200, "cookie")
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/cookie", nil) },
		},
		{
			name: "ErrorPropagation",
			register: func(r httpx.Router) {
				r.GET("/api/err", func(ctx httpx.Context) error {
					return errors.New("boom")
				})
			},
			request: func() *http.Request { return httptest.NewRequest(http.MethodGet, "http://example.com/api/err", nil) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := runAcrossFrameworks(t, tc.register, tc.request)
			assertMatchesGin(t, results)
		})
	}
}

func TestContextBaseConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/ctx/base", func(ctx httpx.Context) error {
			ctx.Set("trace-id", "trace-1")
			deadline, hasDeadline := ctx.Deadline()
			_ = deadline
			v := ctx.Value("trace-id")
			trace, _ := v.(string)
			return ctx.JSON(200, map[string]any{
				"hasDeadline": hasDeadline,
				"doneNotNil":  ctx.Done() != nil,
				"errIsNil":    ctx.Err() == nil,
				"value":       trace,
			})
		})
	}, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "http://example.com/ctx/base", nil)
	})

	assertMatchesGin(t, results)
}

func TestOptionalContextCapabilitiesConformance(t *testing.T) {
	t.Run("ResponseInfoAfterWrite", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err != nil {
					return err
				}
				info, ok := httpx.AsResponseInfo(ctx)
				if !ok {
					return errors.New("response info not supported")
				}
				if info.StatusCode() != http.StatusCreated {
					return errors.New("unexpected status code")
				}
				return nil
			})

			r.GET("/ctx/capabilities/response", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusCreated, map[string]any{"ok": true})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/ctx/capabilities/response", nil)
		})

		for _, name := range conformanceFrameworks {
			got := results[name]
			if got.Status != http.StatusCreated {
				t.Fatalf("%s status mismatch: want %d, got %d", name, http.StatusCreated, got.Status)
			}
		}
	})

	t.Run("NativeContextProvider", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.GET("/ctx/capabilities/native", func(ctx httpx.Context) error {
				native, ok := httpx.AsNativeContext[any](ctx)
				return ctx.JSON(http.StatusOK, map[string]any{"ok": ok && native != nil})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/ctx/capabilities/native", nil)
		})

		for _, name := range conformanceFrameworks {
			got := results[name]
			if got.Status != http.StatusOK {
				t.Fatalf("%s status mismatch: want %d, got %d", name, http.StatusOK, got.Status)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body failed: %v", name, err)
			}
			v, _ := payload["ok"].(bool)
			if !v {
				t.Fatalf("%s native context capability should be true", name)
			}
		}
	})
}

func TestWithJSONConformance(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.GET("/withjson/success", httpx.WithJson(func(ctx httpx.Context) (map[string]any, error) {
				return map[string]any{"name": "ok"}, nil
			}))
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/withjson/success", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("Panic", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.GET("/withjson/panic", httpx.WithJson(func(ctx httpx.Context) (map[string]any, error) {
				panic("boom")
			}))
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/withjson/panic", nil)
		})
		assertMatchesGin(t, results)
	})
}

func mustCookie(ctx httpx.Context, key string) string {
	v, err := ctx.Cookie(key)
	if err != nil {
		return ""
	}
	return v
}
