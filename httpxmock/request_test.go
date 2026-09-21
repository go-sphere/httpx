package httpxmock_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx/httpxmock"
)

func TestRequestInfo(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/v1/users/42?tag=a&tag=b&q=x", nil)
	req.Header.Set("X-Api-Key", "secret")
	req.Header.Add("X-Multi", "one")
	req.Header.Add("X-Multi", "two")
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

	ctx := httpxmock.New(req,
		httpxmock.WithFullPath("/v1/users/:id"),
		httpxmock.WithParams(map[string]string{"id": "42"}),
	)

	if got := ctx.Method(); got != http.MethodPut {
		t.Errorf("Method = %q", got)
	}
	if got := ctx.Path(); got != "/v1/users/42" {
		t.Errorf("Path = %q", got)
	}
	if got := ctx.FullPath(); got != "/v1/users/:id" {
		t.Errorf("FullPath = %q", got)
	}
	if got := ctx.Param("id"); got != "42" {
		t.Errorf("Param(id) = %q", got)
	}
	if got := ctx.Param("absent"); got != "" {
		t.Errorf("Param(absent) = %q, want empty", got)
	}
	if got := ctx.Params(); len(got) != 1 || got["id"] != "42" {
		t.Errorf("Params = %v", got)
	}
	if got := ctx.RawQuery(); got != "tag=a&tag=b&q=x" {
		t.Errorf("RawQuery = %q", got)
	}
	if got := ctx.Query("tag"); got != "a" {
		t.Errorf("Query(tag) = %q, want the first value", got)
	}
	if got := ctx.Queries()["tag"]; len(got) != 2 {
		t.Errorf("Queries()[tag] = %v, want both values", got)
	}
	// Header lookup is case-insensitive, unlike the raw-map fakes it replaces.
	if got := ctx.Header("x-api-key"); got != "secret" {
		t.Errorf("Header(x-api-key) = %q", got)
	}
	if got := ctx.Headers()["X-Multi"]; len(got) != 2 {
		t.Errorf("Headers()[X-Multi] = %v", got)
	}
	if _, ok := ctx.Headers()["Host"]; ok {
		t.Error("Headers() includes Host; net/http carries it outside the header map")
	}
	value, err := ctx.Cookie("session")
	if err != nil || value != "abc" {
		t.Errorf("Cookie(session) = %q, %v", value, err)
	}
	if _, err := ctx.Cookie("absent"); !errors.Is(err, http.ErrNoCookie) {
		t.Errorf("Cookie(absent) err = %v, want http.ErrNoCookie", err)
	}
	if got := ctx.Cookies(); got["session"] != "abc" {
		t.Errorf("Cookies = %v", got)
	}
}

func TestEmptyRequestInfoReturnsZeroValues(t *testing.T) {
	ctx := httpxmock.New(nil)
	if got := ctx.Params(); got != nil {
		t.Errorf("Params = %v, want nil", got)
	}
	if got := ctx.Queries(); got != nil {
		t.Errorf("Queries = %v, want nil", got)
	}
	if got := ctx.Cookies(); got != nil {
		t.Errorf("Cookies = %v, want nil", got)
	}
	if got := ctx.FullPath(); got != "" {
		t.Errorf("FullPath = %q, want empty", got)
	}
	if got := ctx.BodyReader(); got != http.NoBody {
		t.Errorf("BodyReader = %v, want http.NoBody", got)
	}
}

func TestClientIP(t *testing.T) {
	ctx := httpxmock.New(nil)
	// httptest.NewRequest's peer, with the port dropped.
	if got := ctx.ClientIP(); got != "192.0.2.1" {
		t.Errorf("ClientIP = %q, want the peer address", got)
	}

	// A forwarding header is not believed: there is no trusted-proxy policy
	// to attribute it to, which is what an adapter with an empty policy does.
	spoofed := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofed.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := httpxmock.New(spoofed).ClientIP(); got != "192.0.2.1" {
		t.Errorf("ClientIP = %q, want the peer address, not X-Forwarded-For", got)
	}

	if got := httpxmock.New(nil, httpxmock.WithClientIP("203.0.113.10")).ClientIP(); got != "203.0.113.10" {
		t.Errorf("ClientIP = %q, want the configured value", got)
	}
}

func TestBodyAccess(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))

	raw, err := ctx.BodyRaw()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"a":1}` {
		t.Errorf("BodyRaw = %q", raw)
	}
	// The returned slice belongs to the caller: mutating it must not change
	// what the restored body holds.
	if len(raw) > 0 {
		raw[0] = 'X'
	}
	second, err := ctx.BodyRaw()
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != `{"a":1}` {
		t.Errorf("BodyRaw after mutating the first result = %q", second)
	}

	// The body is restored after BodyRaw, so a reader still sees it — and
	// BodyReader, unlike BodyRaw, consumes it.
	again, err := io.ReadAll(ctx.BodyReader())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != `{"a":1}` {
		t.Errorf("BodyReader after BodyRaw = %q", again)
	}

	empty := httpxmock.New(nil)
	body, err := empty.BodyRaw()
	if err != nil || len(body) != 0 {
		t.Errorf("BodyRaw on an empty request = %q, %v; want empty, nil", body, err)
	}
}

func TestFormValue(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodPost, "/?q=fromquery", strings.NewReader("name=alice"))
	ctx.Request().Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if got := ctx.FormValue("name"); got != "alice" {
		t.Errorf("FormValue(name) = %q", got)
	}
	if got := ctx.FormValue("q"); got != "fromquery" {
		t.Errorf("FormValue(q) = %q", got)
	}
	if got := ctx.FormValue("absent"); got != "" {
		t.Errorf("FormValue(absent) = %q, want empty", got)
	}
}

// multipartRequest builds a real multipart body, so FormFile returns a header
// backed by actual content rather than a canned value.
func multipartRequest(t *testing.T, field, filename, content, extraKey, extraValue string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if extraKey != "" {
		if err := w.WriteField(extraKey, extraValue); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestMultipartFormAndFormFile(t *testing.T) {
	ctx := httpxmock.New(multipartRequest(t, "file", "photo.JPG", "image-bytes", "caption", "hello"))

	header, err := ctx.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}
	if header.Filename != "photo.JPG" {
		t.Errorf("Filename = %q, want the original case preserved", header.Filename)
	}
	if header.Size != int64(len("image-bytes")) {
		t.Errorf("Size = %d, want %d", header.Size, len("image-bytes"))
	}
	f, err := header.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "image-bytes" {
		t.Errorf("content = %q", content)
	}

	// Parsing happens at most once and the form is reusable afterwards.
	form, err := ctx.MultipartForm()
	if err != nil {
		t.Fatal(err)
	}
	if len(form.File["file"]) != 1 {
		t.Errorf("form.File[file] = %v", form.File["file"])
	}
	if got := form.Value["caption"]; len(got) != 1 || got[0] != "hello" {
		t.Errorf("form.Value[caption] = %v", got)
	}

	if _, err := ctx.FormFile("absent"); err == nil {
		t.Error("FormFile(absent): want an error")
	}
}

func TestNonMultipartFormFileReportsAnError(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodPost, "/upload", strings.NewReader("not multipart"))
	if _, err := ctx.FormFile("file"); err == nil {
		t.Error("FormFile on a non-multipart body: want an error")
	}
	if _, err := ctx.MultipartForm(); err == nil {
		t.Error("MultipartForm on a non-multipart body: want an error")
	}
}

func TestOptionsAndRequestAccessor(t *testing.T) {
	type key string
	base := context.WithValue(context.Background(), key("seed"), "v")

	ctx := httpxmock.NewRequest(http.MethodGet, "/", nil,
		httpxmock.WithContext(base),
		httpxmock.WithHeader("Authorization", "Bearer token"),
		httpxmock.WithCookie(&http.Cookie{Name: "c", Value: "1"}),
		httpxmock.WithParam("a", "1"),
		httpxmock.WithParam("b", "2"),
		httpxmock.WithState("principal", "alice"),
		nil, // a nil option is skipped rather than panicking
	)

	if got := ctx.Context().Value(key("seed")); got != "v" {
		t.Errorf("seeded context value = %v", got)
	}
	if got := ctx.Header("Authorization"); got != "Bearer token" {
		t.Errorf("Header = %q", got)
	}
	if got, _ := ctx.Cookie("c"); got != "1" {
		t.Errorf("Cookie = %q", got)
	}
	if got := ctx.Params(); len(got) != 2 {
		t.Errorf("Params = %v, want both", got)
	}
	if got, ok := ctx.Get("principal"); !ok || got != "alice" {
		t.Errorf("Get(principal) = %v, %v", got, ok)
	}
	if ctx.Request().Method != http.MethodGet {
		t.Errorf("Request().Method = %q", ctx.Request().Method)
	}
}
