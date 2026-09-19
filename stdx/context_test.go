package stdx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// Responders

func TestJSON(t *testing.T) {
	t.Run("WritesCompactJSONWithoutTrailingNewline", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/j", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusCreated, map[string]any{"b": 1, "a": "x<y"})
		})
		rec := serve(engine, getReq("/j"))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
		// Exact bytes, not canonicalized: sorted keys, HTML escaping, no
		// newline — the same wire output as gin.
		if want := `{"a":"x\u003cy","b":1}`; rec.Body.String() != want {
			t.Fatalf("body = %q, want %q", rec.Body.String(), want)
		}
	})

	t.Run("BodylessStatusSendsHeaderOnly", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/j", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusNoContent, map[string]any{"a": 1})
		})
		rec := serve(engine, getReq("/j"))
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	// A value that cannot be encoded must leave the response untouched: the
	// error handler then renders on a clean writer with its own content type.
	t.Run("EncodeFailureLeavesResponseUncommitted", func(t *testing.T) {
		var committed bool
		var status int
		engine, r := newTestEngine(t, WithErrorHandler(func(ctx httpx.Context, err error) {
			native, _ := httpx.AsNativeContext[*Native](ctx)
			committed = native.Written()
			status = ctx.StatusCode()
			_ = ctx.Text(http.StatusInternalServerError, "encode failed")
		}))
		r.GET("/j", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusOK, map[string]any{"f": func() {}})
		})
		rec := serve(engine, getReq("/j"))
		if committed || status != http.StatusOK {
			t.Fatalf("error handler saw committed=%v status=%d, want an untouched response", committed, status)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Fatalf("Content-Type = %q, want the error handler's, not JSON's", ct)
		}
		if rec.Code != http.StatusInternalServerError || rec.Body.String() != "encode failed" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	// The pooled context serves many requests; an encoder problem on one must
	// not follow it to the next.
	t.Run("EncodeFailureDoesNotPoisonTheContext", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/bad", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusOK, map[string]any{"f": func() {}})
		})
		r.GET("/good", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusOK, map[string]int{"n": 1})
		})
		serve(engine, getReq("/bad"))
		rec := serve(engine, getReq("/good"))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"n":1}` {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("CustomMarshalerOutputIsCompacted", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/j", func(ctx httpx.Context) error {
			return ctx.JSON(http.StatusOK, prettyMarshaler{})
		})
		rec := serve(engine, getReq("/j"))
		if want := `{"a":[1,2]}`; rec.Body.String() != want {
			t.Fatalf("body = %q, want %q", rec.Body.String(), want)
		}
	})
}

type prettyMarshaler struct{}

func (prettyMarshaler) MarshalJSON() ([]byte, error) {
	return []byte("{\n  \"a\": [1,\n 2]\n}\n"), nil
}

func TestTextBytesNoContent(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/text", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "hi") })
	r.GET("/text204", func(ctx httpx.Context) error { return ctx.Text(http.StatusNoContent, "dropped") })
	r.GET("/sniff", func(ctx httpx.Context) error {
		return ctx.Bytes(http.StatusOK, []byte("<html><body>x</body></html>"), "")
	})
	r.GET("/typed", func(ctx httpx.Context) error {
		return ctx.Bytes(http.StatusOK, []byte{1, 2}, "application/octet-stream")
	})
	r.GET("/none", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusResetContent) })

	for _, tc := range []struct {
		path        string
		status      int
		contentType string
		body        string
	}{
		{"/text", http.StatusOK, "text/plain; charset=utf-8", "hi"},
		{"/text204", http.StatusNoContent, "text/plain; charset=utf-8", ""},
		{"/sniff", http.StatusOK, "text/html; charset=utf-8", "<html><body>x</body></html>"},
		{"/typed", http.StatusOK, "application/octet-stream", "\x01\x02"},
		{"/none", http.StatusResetContent, "", ""},
	} {
		rec := serve(engine, getReq(tc.path))
		if rec.Code != tc.status || rec.Body.String() != tc.body {
			t.Fatalf("%s: status = %d, body = %q", tc.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != tc.contentType {
			t.Fatalf("%s: Content-Type = %q, want %q", tc.path, ct, tc.contentType)
		}
	}
}

func TestRedirect(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/go", func(ctx httpx.Context) error { return ctx.Redirect(http.StatusFound, "/there") })
	r.GET("/bad", func(ctx httpx.Context) error { return ctx.Redirect(http.StatusOK, "/there") })

	rec := serve(engine, getReq("/go"))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/there" {
		t.Fatalf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	// A non-redirect code is a programming error reported as 500, never a
	// silent 200 with a Location header.
	rec = serve(engine, getReq("/bad"))
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" {
		t.Fatalf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestDataFromReaderAndFile(t *testing.T) {
	t.Run("DataFromReaderSetsLengthAndClosesSource", func(t *testing.T) {
		src := &closeTracker{Reader: strings.NewReader("hello")}
		engine, r := newTestEngine(t)
		r.GET("/d", func(ctx httpx.Context) error {
			return ctx.DataFromReader(http.StatusOK, "text/plain", src, 5)
		})
		rec := serve(engine, getReq("/d"))
		if rec.Body.String() != "hello" || rec.Header().Get("Content-Length") != "5" {
			t.Fatalf("body = %q, Content-Length = %q", rec.Body.String(), rec.Header().Get("Content-Length"))
		}
		if !src.closed {
			t.Fatal("source reader was not closed")
		}
	})

	t.Run("DataFromReaderUnknownSizeOmitsLength", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/d", func(ctx httpx.Context) error {
			return ctx.DataFromReader(http.StatusOK, "", strings.NewReader("abc"), -1)
		})
		rec := serve(engine, getReq("/d"))
		if rec.Body.String() != "abc" || rec.Header().Get("Content-Length") != "" {
			t.Fatalf("body = %q, Content-Length = %q", rec.Body.String(), rec.Header().Get("Content-Length"))
		}
	})

	t.Run("FileServesFromDisk", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a.txt")
		if err := os.WriteFile(path, []byte("file-body"), 0o600); err != nil {
			t.Fatal(err)
		}
		engine, r := newTestEngine(t)
		r.GET("/f", func(ctx httpx.Context) error { return ctx.File(path) })
		rec := serve(engine, getReq("/f"))
		if rec.Code != http.StatusOK || rec.Body.String() != "file-body" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})
}

func TestHeadersAndCookiesOut(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/h", func(ctx httpx.Context) error {
		ctx.SetHeader("X-A", "1")
		ctx.SetCookie(&http.Cookie{Name: "sid", Value: "abc", Path: "/"})
		ctx.SetCookie(nil)
		return ctx.NoContent(http.StatusOK)
	})
	rec := serve(engine, getReq("/h"))
	if rec.Header().Get("X-A") != "1" {
		t.Fatalf("X-A = %q", rec.Header().Get("X-A"))
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].Value != "abc" {
		t.Fatalf("cookies = %v", cookies)
	}
}

func TestStatusTracking(t *testing.T) {
	t.Run("FirstCommitWinsAndIsReported", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var reported []int
		r.GET("/s", func(ctx httpx.Context) error {
			ctx.Status(http.StatusAccepted)
			reported = append(reported, ctx.StatusCode())
			if err := ctx.Text(http.StatusCreated, "first"); err != nil {
				return err
			}
			reported = append(reported, ctx.StatusCode())
			// Already committed: must not change the status or report a new one.
			if err := ctx.NoContent(http.StatusNoContent); err != nil {
				return err
			}
			reported = append(reported, ctx.StatusCode())
			return nil
		})
		rec := serve(engine, getReq("/s"))
		if rec.Code != http.StatusCreated || rec.Body.String() != "first" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if want := []int{202, 201, 201}; !equalInts(reported, want) {
			t.Fatalf("StatusCode sequence = %v, want %v", reported, want)
		}
	})

	// Headers set after the commit are not sent: the wrapper must not defeat
	// net/http's snapshot by handing out a stale map.
	t.Run("HeaderAfterCommitIsNotSent", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/s", func(ctx httpx.Context) error {
			if err := ctx.Text(http.StatusOK, "x"); err != nil {
				return err
			}
			ctx.SetHeader("X-Late", "1")
			return nil
		})
		rec := serve(engine, getReq("/s"))
		if rec.Result().Header.Get("X-Late") != "" {
			t.Fatal("a header set after the commit reached the response")
		}
	})
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Request accessors

func TestRequestInfo(t *testing.T) {
	engine, r := newTestEngine(t)
	type info struct {
		method, path, fullPath, rawQuery string
		param, missingParam              string
		params                           map[string]string
		query, missingQuery              string
		queries                          map[string][]string
		header                           string
		headers                          map[string][]string
		cookie                           string
		cookieErr                        error
		cookies                          map[string]string
	}
	var got info
	r.GET("/users/:id/posts/:post", func(ctx httpx.Context) error {
		got = info{
			method: ctx.Method(), path: ctx.Path(), fullPath: ctx.FullPath(), rawQuery: ctx.RawQuery(),
			param: ctx.Param("post"), missingParam: ctx.Param("nope"), params: ctx.Params(),
			query: ctx.Query("q"), missingQuery: ctx.Query("nope"), queries: ctx.Queries(),
			header: ctx.Header("x-token"), headers: ctx.Headers(),
			cookies: ctx.Cookies(),
		}
		got.cookie, _ = ctx.Cookie("a")
		_, got.cookieErr = ctx.Cookie("missing")
		return nil
	})
	req := getReq("/users/42/posts/7?q=1&tag=a&tag=b")
	req.Header.Set("X-Token", "tok")
	req.Header["x-raw"] = []string{"v"} // a non-canonical key, as a client may send it
	req.AddCookie(&http.Cookie{Name: "a", Value: "1"})
	req.AddCookie(&http.Cookie{Name: "b", Value: "2"})
	serve(engine, req)

	if got.method != http.MethodGet || got.path != "/users/42/posts/7" || got.fullPath != "/users/:id/posts/:post" {
		t.Fatalf("method=%q path=%q fullPath=%q", got.method, got.path, got.fullPath)
	}
	if got.rawQuery != "q=1&tag=a&tag=b" {
		t.Fatalf("RawQuery = %q", got.rawQuery)
	}
	if got.param != "7" || got.missingParam != "" || got.params["id"] != "42" || got.params["post"] != "7" || len(got.params) != 2 {
		t.Fatalf("param=%q missing=%q params=%v", got.param, got.missingParam, got.params)
	}
	if got.query != "1" || got.missingQuery != "" || strings.Join(got.queries["tag"], ",") != "a,b" {
		t.Fatalf("query=%q missing=%q queries=%v", got.query, got.missingQuery, got.queries)
	}
	if got.header != "tok" || !slices.Equal(got.headers["X-Token"], []string{"tok"}) || !slices.Equal(got.headers["X-Raw"], []string{"v"}) {
		t.Fatalf("header=%q headers=%v (keys must be canonical)", got.header, got.headers)
	}
	if got.cookie != "1" || !errors.Is(got.cookieErr, http.ErrNoCookie) || got.cookies["b"] != "2" {
		t.Fatalf("cookie=%q err=%v cookies=%v", got.cookie, got.cookieErr, got.cookies)
	}
}

func TestQueriesAndHeadersAreCopies(t *testing.T) {
	engine, r := newTestEngine(t)
	var after string
	r.GET("/q", func(ctx httpx.Context) error {
		ctx.Queries()["q"][0] = "mutated"
		delete(ctx.Headers(), "X-Token")
		after = ctx.Query("q") + "|" + ctx.Header("X-Token")
		return nil
	})
	req := getReq("/q?q=orig")
	req.Header.Set("X-Token", "t")
	serve(engine, req)
	if after != "orig|t" {
		t.Fatalf("mutating the returned maps changed the request: %q", after)
	}
}

func TestEmptyCollectionsAreNil(t *testing.T) {
	engine, r := newTestEngine(t)
	var params map[string]string
	var queries map[string][]string
	var cookies map[string]string
	r.GET("/e", func(ctx httpx.Context) error {
		params, queries, cookies = ctx.Params(), ctx.Queries(), ctx.Cookies()
		return nil
	})
	serve(engine, getReq("/e"))
	if params != nil || queries != nil || cookies != nil {
		t.Fatalf("params=%v queries=%v cookies=%v, want all nil", params, queries, cookies)
	}
}

// Replacing the request through the native context (what AdaptStdMiddleware
// does) must be visible to every accessor, including URL-derived ones.
func TestQueryFollowsReplacedRequest(t *testing.T) {
	engine, r := newTestEngine(t)
	var before, after string
	r.GET("/q", func(ctx httpx.Context) error {
		before = ctx.Query("a")
		native, ok := httpx.AsNativeContext[*Native](ctx)
		if !ok {
			return errors.New("no native context")
		}
		native.SetRequest(getReq("/q?a=2"))
		after = ctx.Query("a")
		return nil
	})
	serve(engine, getReq("/q?a=1"))
	if before != "1" || after != "2" {
		t.Fatalf("before=%q after=%q", before, after)
	}
}

// Body

func TestBodyRaw(t *testing.T) {
	t.Run("ReturnsOwnedCopyAndKeepsBodyReadable", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var first, second []byte
		var bound struct {
			Name string `json:"name"`
		}
		r.POST("/b", func(ctx httpx.Context) error {
			var err error
			if first, err = ctx.BodyRaw(); err != nil {
				return err
			}
			if second, err = ctx.BodyRaw(); err != nil {
				return err
			}
			return ctx.BindJSON(&bound)
		})
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", `{"name":"a"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if string(first) != `{"name":"a"}` || !bytes.Equal(first, second) || bound.Name != "a" {
			t.Fatalf("first=%q second=%q bound=%q", first, second, bound.Name)
		}
		first[0] = 'X'
		if second[0] != '{' {
			t.Fatal("the two BodyRaw results share memory; each must be the caller's own copy")
		}
	})

	t.Run("NilBodyIsNil", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var raw []byte
		var err error
		var reader io.ReadCloser
		r.GET("/b", func(ctx httpx.Context) error {
			raw, err = ctx.BodyRaw()
			reader = ctx.BodyReader()
			return nil
		})
		req := getReq("/b")
		req.Body = nil
		serve(engine, req)
		if raw != nil || err != nil || reader != http.NoBody {
			t.Fatalf("raw=%v err=%v reader=%v", raw, err, reader)
		}
	})

	// Content-Length is a hint, never a limit: the body is read to EOF whatever
	// the declared size.
	for _, tc := range []struct {
		name          string
		contentLength int64
		body          string
	}{
		{"UnknownLength", -1, strings.Repeat("u", 3000)},
		{"DeclaredTooSmall", 4, "0123456789"},
		{"DeclaredTooLarge", 100, "abc"},
		{"DeclaredZeroWithBody", 0, "xyz"},
		{"LargerThanOneRead", 70000, strings.Repeat("l", 70000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, r := newTestEngine(t)
			var raw []byte
			r.POST("/b", func(ctx httpx.Context) error {
				var err error
				raw, err = ctx.BodyRaw()
				return err
			})
			req := bodyReq(http.MethodPost, "/b", "", tc.body)
			req.ContentLength = tc.contentLength
			req.Body = io.NopCloser(io.MultiReader(strings.NewReader(tc.body[:1]), strings.NewReader(tc.body[1:])))
			rec := serve(engine, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
			}
			if string(raw) != tc.body {
				t.Fatalf("BodyRaw read %d bytes, want %d", len(raw), len(tc.body))
			}
		})
	}
}

// Binders

func TestBindJSON(t *testing.T) {
	type dto struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	engine, r := newTestEngine(t)
	var got dto
	var bindErr error
	r.POST("/b", func(ctx httpx.Context) error {
		got = dto{}
		bindErr = ctx.BindJSON(&got)
		return bindErr
	})

	t.Run("Valid", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", `{"name":"tom","age":3}`))
		if rec.Code != http.StatusOK || got.Name != "tom" || got.Age != 3 {
			t.Fatalf("status = %d, got = %+v", rec.Code, got)
		}
	})
	t.Run("AbsentFieldKeepsZeroValue", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", `{"age":3}`))
		if rec.Code != http.StatusOK || bindErr != nil || got.Name != "" || got.Age != 3 {
			t.Fatalf("status = %d, err = %v, got = %+v", rec.Code, bindErr, got)
		}
	})
	t.Run("MalformedIs400", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", `{"name":`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("EmptyBodyIs400", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("NilBodyIs400", func(t *testing.T) {
		req := bodyReq(http.MethodPost, "/b", "application/json", "")
		req.Body = nil
		rec := serve(engine, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("WrongTypeIs400", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/b", "application/json", `{"name":"x","age":"old"}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestBindQueryURIHeader(t *testing.T) {
	type query struct {
		Name   string   `query:"name"`
		Tags   []string `query:"tags"`
		Active bool     `query:"active"`
	}
	type uri struct {
		ID   string `uri:"id"`
		Slug string `uri:"slug"`
	}
	type header struct {
		Token string `header:"X-Token"`
	}
	engine, r := newTestEngine(t)
	var q query
	var u uri
	var h header
	r.GET("/u/:id/:slug", func(ctx httpx.Context) error {
		q, u, h = query{}, uri{}, header{}
		if err := ctx.BindQuery(&q); err != nil {
			return err
		}
		if err := ctx.BindURI(&u); err != nil {
			return err
		}
		return ctx.BindHeader(&h)
	})
	// An unknown query key is client data, not an error.
	req := getReq("/u/42/hello-world?name=n&tags=a&tags=b&active=true&unknown=1")
	req.Header.Set("X-Token", "tok")
	if rec := serve(engine, req); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if q.Name != "n" || strings.Join(q.Tags, ",") != "a,b" || !q.Active {
		t.Fatalf("query = %+v", q)
	}
	if u.ID != "42" || u.Slug != "hello-world" {
		t.Fatalf("uri = %+v", u)
	}
	if h.Token != "tok" {
		t.Fatalf("header = %+v", h)
	}

	// Binding twice must not see the first call's values: uri values are rebuilt
	// per call.
	req = getReq("/u/1/two")
	if rec := serve(engine, req); rec.Code != http.StatusOK || u.ID != "1" || u.Slug != "two" {
		t.Fatalf("second bind: status = %d, uri = %+v", rec.Code, u)
	}
}

// No Bind* call validates: a `binding` tag is inert on every source, so an
// absent field decodes to its zero value instead of failing the request.
func TestBindDoesNotValidateOnAnySource(t *testing.T) {
	type need struct {
		V string `query:"v" uri:"v" header:"X-V" form:"v" binding:"required"`
	}
	engine, r := newTestEngine(t)
	var errs map[string]error
	r.POST("/plain", func(ctx httpx.Context) error {
		errs = map[string]error{}
		var n need
		errs["query"] = ctx.BindQuery(&n)
		// No route parameters at all, so BindURI decodes nothing.
		errs["uri"] = ctx.BindURI(&n)
		errs["header"] = ctx.BindHeader(&n)
		errs["form"] = ctx.BindForm(&n)
		return nil
	})
	if rec := serve(engine, bodyReq(http.MethodPost, "/plain", "application/x-www-form-urlencoded", "other=1")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for source, err := range errs {
		if err != nil {
			t.Fatalf("%s: bind reported %v, want no error", source, err)
		}
	}
}

func TestBindForm(t *testing.T) {
	type form struct {
		Title string   `form:"title"`
		Tags  []string `form:"tags"`
	}
	engine, r := newTestEngine(t)
	var got form
	var formValue string
	r.POST("/f", func(ctx httpx.Context) error {
		got = form{}
		if err := ctx.BindForm(&got); err != nil {
			return err
		}
		formValue = ctx.FormValue("title")
		return nil
	})

	t.Run("URLEncoded", func(t *testing.T) {
		rec := serve(engine, bodyReq(http.MethodPost, "/f?title=fromquery", "application/x-www-form-urlencoded", "title=t&tags=a&tags=b"))
		if rec.Code != http.StatusOK || got.Title != "t" || strings.Join(got.Tags, ",") != "a,b" {
			t.Fatalf("status = %d, got = %+v; the body must win over the query", rec.Code, got)
		}
	})

	t.Run("Multipart", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("title", "mp")
		_ = w.WriteField("tags", "x")
		part, err := w.CreateFormFile("file", "a.txt")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("content"))
		_ = w.Close()
		req := bodyReq(http.MethodPost, "/f", w.FormDataContentType(), buf.String())
		rec := serve(engine, req)
		if rec.Code != http.StatusOK || got.Title != "mp" || strings.Join(got.Tags, ",") != "x" || formValue != "mp" {
			t.Fatalf("status = %d, got = %+v, FormValue = %q", rec.Code, got, formValue)
		}
	})
}

func TestMultipartFormAndFormFile(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("title", "sample")
	part, err := w.CreateFormFile("file", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("0123456789"))
	_ = w.Close()

	engine, r := newTestEngine(t)
	var filename string
	var size int64
	var title string
	var missingErr error
	r.POST("/up", func(ctx httpx.Context) error {
		fh, err := ctx.FormFile("file")
		if err != nil {
			return err
		}
		filename, size = fh.Filename, fh.Size
		form, err := ctx.MultipartForm()
		if err != nil {
			return err
		}
		title = form.Value["title"][0]
		_, missingErr = ctx.FormFile("nope")
		return nil
	})
	rec := serve(engine, bodyReq(http.MethodPost, "/up", w.FormDataContentType(), buf.String()))
	if rec.Code != http.StatusOK || filename != "a.txt" || size != 10 || title != "sample" {
		t.Fatalf("status = %d, filename = %q, size = %d, title = %q", rec.Code, filename, size, title)
	}
	if !errors.Is(missingErr, http.ErrMissingFile) {
		t.Fatalf("FormFile(missing) = %v, want http.ErrMissingFile", missingErr)
	}
}

// State and context

func TestStateStore(t *testing.T) {
	engine, r := newTestEngine(t)
	var results []string
	r.GET("/s", func(ctx httpx.Context) error {
		results = nil
		record := func(key string) {
			v, ok := ctx.Get(key)
			results = append(results, key+"="+strings.TrimSpace(strings.Join([]string{toString(v), boolString(ok)}, "/")))
		}
		record("missing")
		ctx.Set("k", "v1")
		record("k")
		ctx.Set("k", "v2")
		record("k")
		// A stored nil reports as absent, the way every adapter agrees.
		ctx.Set("n", nil)
		record("n")
		return nil
	})
	serve(engine, getReq("/s"))
	if want := "missing=<nil>/false,k=v1/true,k=v2/true,n=<nil>/false"; strings.Join(results, ",") != want {
		t.Fatalf("got %q, want %q", strings.Join(results, ","), want)
	}
}

func toString(v any) string {
	if v == nil {
		return "<nil>"
	}
	s, _ := v.(string)
	return s
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type ctxKey struct{}

func TestSetContextPropagates(t *testing.T) {
	engine, r := newTestEngine(t)
	var fromCtx, fromReq any
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			ctx.SetContext(context.WithValue(ctx.Context(), ctxKey{}, "set"))
			return next(ctx)
		}
	})
	r.GET("/c", func(ctx httpx.Context) error {
		fromCtx = ctx.Context().Value(ctxKey{})
		native, _ := httpx.AsNativeContext[*Native](ctx)
		fromReq = native.Request().Context().Value(ctxKey{})
		return nil
	})
	serve(engine, getReq("/c"))
	if fromCtx != "set" || fromReq != "set" {
		t.Fatalf("Context()=%v Request().Context()=%v", fromCtx, fromReq)
	}
}

// Chain control

func TestChainControl(t *testing.T) {
	t.Run("NotCallingNextStopsTheChain", func(t *testing.T) {
		var tr trace
		engine, r := newTestEngine(t)
		r.Use(func(httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				tr.steps = append(tr.steps, "stop")
				return ctx.Text(http.StatusUnauthorized, "denied")
			}
		})
		r.Use(tr.mw("never"))
		r.GET("/n", tr.leaf("leaf"))
		rec := serve(engine, getReq("/n"))
		if rec.Code != http.StatusUnauthorized || tr.String() != "stop" {
			t.Fatalf("status = %d, chain = %q", rec.Code, tr.String())
		}
	})
}

// Streaming

func TestStreamAndFlush(t *testing.T) {
	t.Run("StreamCommitsBeforeCallbackAndFlushesEachWrite", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var statusInside int
		r.GET("/s", func(ctx httpx.Context) error {
			streamer, ok := httpx.AsStreamer(ctx)
			if !ok {
				return errors.New("no streamer")
			}
			return streamer.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
				statusInside = ctx.StatusCode()
				if _, err := io.WriteString(w, "a"); err != nil {
					return err
				}
				_, err := io.WriteString(w, "b")
				return err
			})
		})
		rec := serve(engine, getReq("/s"))
		if !rec.Flushed || rec.Body.String() != "ab" || statusInside != http.StatusOK {
			t.Fatalf("flushed=%v body=%q status=%d", rec.Flushed, rec.Body.String(), statusInside)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("FlushCommitsRecordedStatus", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/f", func(ctx httpx.Context) error {
			ctx.Status(http.StatusAccepted)
			flusher, ok := httpx.AsFlusher(ctx)
			if !ok {
				return errors.New("no flusher")
			}
			return flusher.Flush()
		})
		rec := serve(engine, getReq("/f"))
		if !rec.Flushed || rec.Code != http.StatusAccepted {
			t.Fatalf("flushed=%v status=%d", rec.Flushed, rec.Code)
		}
	})
}

// The writer wrapper and the native context

func TestResponseWriterWrapper(t *testing.T) {
	t.Run("WriteWithoutWriteHeaderUsesRecordedStatus", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/w", func(ctx httpx.Context) error {
			ctx.Status(http.StatusAccepted)
			native, _ := httpx.AsNativeContext[*Native](ctx)
			_, err := native.ResponseWriter().Write([]byte("raw"))
			return err
		})
		rec := serve(engine, getReq("/w"))
		if rec.Code != http.StatusAccepted || rec.Body.String() != "raw" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("ReadFromCommitsAndCopies", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/w", func(ctx httpx.Context) error {
			ctx.Status(http.StatusCreated)
			native, _ := httpx.AsNativeContext[*Native](ctx)
			// io.Copy prefers the destination's ReadFrom, which is the path
			// http.ServeContent and sendfile take.
			_, err := io.Copy(native.ResponseWriter(), strings.NewReader("copied"))
			return err
		})
		rec := serve(engine, getReq("/w"))
		if rec.Code != http.StatusCreated || rec.Body.String() != "copied" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("HijackMarksTheResponseProduced", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var written bool
		r.GET("/h", func(ctx httpx.Context) error {
			native, _ := httpx.AsNativeContext[*Native](ctx)
			hijacker, ok := native.ResponseWriter().(http.Hijacker)
			if !ok {
				return errors.New("wrapper is not a Hijacker")
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				return err
			}
			_ = conn.Close()
			written = native.Written()
			// Nothing may render over a hijacked connection.
			return errors.New("after hijack")
		})
		rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
		engine.ServeHTTP(rec, getReq("/h"))
		if !rec.hijacked || !written || rec.Body.Len() != 0 {
			t.Fatalf("hijacked=%v written=%v body=%q", rec.hijacked, written, rec.Body.String())
		}
	})

	t.Run("FlushOnWrapperCommits", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/f", func(ctx httpx.Context) error {
			ctx.Status(http.StatusAccepted)
			native, _ := httpx.AsNativeContext[*Native](ctx)
			flusher, ok := native.ResponseWriter().(http.Flusher)
			if !ok {
				return errors.New("wrapper is not a Flusher")
			}
			flusher.Flush()
			return nil
		})
		rec := serve(engine, getReq("/f"))
		if !rec.Flushed || rec.Code != http.StatusAccepted {
			t.Fatalf("flushed=%v status=%d", rec.Flushed, rec.Code)
		}
	})

	t.Run("UnwrapReachesTheRealWriter", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var unwrapped http.ResponseWriter
		r.GET("/u", func(ctx httpx.Context) error {
			native, _ := httpx.AsNativeContext[*Native](ctx)
			rw, ok := native.ResponseWriter().(interface{ Unwrap() http.ResponseWriter })
			if !ok {
				return errors.New("no Unwrap")
			}
			unwrapped = rw.Unwrap()
			return nil
		})
		rec := serve(engine, getReq("/u"))
		if unwrapped != rec {
			t.Fatalf("Unwrap = %T, want the recorder", unwrapped)
		}
	})
}

// ownHeaderWriter is a wrapping writer with its own header map, the shape a
// buffering middleware (gzip, caching) hands downstream.
type ownHeaderWriter struct {
	http.ResponseWriter
	header http.Header
	buf    bytes.Buffer
}

func (w *ownHeaderWriter) Header() http.Header         { return w.header }
func (w *ownHeaderWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *ownHeaderWriter) WriteHeader(int)             {}

func TestNative(t *testing.T) {
	engine, r := newTestEngine(t)
	wrapper := &ownHeaderWriter{header: http.Header{}}
	var checks []string
	r.GET("/n", func(ctx httpx.Context) error {
		native, ok := httpx.AsNativeContext[*Native](ctx)
		if !ok {
			return errors.New("no native context")
		}
		if native.Engine() != engine {
			checks = append(checks, "engine")
		}
		w, req := native.Unwrap()
		if w != native.ResponseWriter() || req != native.Request() {
			checks = append(checks, "unwrap")
		}
		if native.Written() {
			checks = append(checks, "written-early")
		}
		ctx.SetHeader("X-Before", "1")

		// Swapping the writer redirects both headers and body.
		wrapper.ResponseWriter = w
		native.SetWriter(wrapper)
		ctx.SetHeader("X-After", "1")
		if err := ctx.Text(http.StatusOK, "wrapped"); err != nil {
			return err
		}
		if !native.Written() || ctx.StatusCode() != http.StatusOK {
			checks = append(checks, "written-late")
		}
		return nil
	})
	rec := serve(engine, getReq("/n"))
	if len(checks) != 0 {
		t.Fatalf("failed checks: %v", checks)
	}
	if rec.Header().Get("X-Before") != "1" || rec.Header().Get("X-After") != "" {
		t.Fatalf("recorder headers = %v; X-After must have gone to the wrapper", rec.Header())
	}
	if wrapper.header.Get("X-After") != "1" || wrapper.buf.String() != "wrapped" || rec.Body.Len() != 0 {
		t.Fatalf("wrapper header=%v body=%q, recorder body=%q", wrapper.header, wrapper.buf.String(), rec.Body.String())
	}
}

func TestMarkWrittenSuppressesErrorRendering(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/m", func(ctx httpx.Context) error {
		native, _ := httpx.AsNativeContext[*Native](ctx)
		native.MarkWritten(http.StatusCreated)
		if ctx.StatusCode() != http.StatusCreated {
			return errors.New("status not recorded")
		}
		return errors.New("already answered elsewhere")
	})
	rec := serve(engine, getReq("/m"))
	if rec.Body.Len() != 0 {
		t.Fatalf("error body rendered over a response marked as written: %q", rec.Body.String())
	}
}
