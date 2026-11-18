package httpxmock

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

type trackingReadCloser struct {
	r      io.Reader
	read   int
	closed bool
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	t.read += n
	return n, err
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestJSONPreservesPresetContentTypeAndErrorsBefore204(t *testing.T) {
	c := NewRequest(http.MethodGet, "/", nil)
	c.SetHeader("Content-Type", "application/problem+json")
	if err := c.JSON(http.StatusOK, map[string]string{"ok": "yes"}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if got := c.ResponseHeaders().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}

	c2 := New(nil)
	if err := c2.JSON(http.StatusNoContent, make(chan int)); err == nil {
		t.Fatal("JSON(204, chan) = nil, want error")
	}
	if c2.res.committed {
		t.Fatal("JSON(204, chan) committed response on marshal error")
	}
}

func TestBodylessResponsesAndDataFromReaderUnread(t *testing.T) {
	for _, code := range []int{http.StatusContinue, http.StatusProcessing, http.StatusEarlyHints} {
		t.Run("informational_drops_body", func(t *testing.T) {
			c := New(nil)
			if err := c.Text(code, "ignored"); err != nil {
				t.Fatalf("Text(%d): %v", code, err)
			}
			if c.res.committed || c.res.body.Len() != 0 {
				t.Fatalf("Text(%d) committed=%v body=%q, want uncommitted & empty", code, c.res.committed, c.res.body.String())
			}

			c2 := New(nil)
			if err := c2.Bytes(code, []byte("ignored"), "application/octet-stream"); err != nil {
				t.Fatalf("Bytes(%d): %v", code, err)
			}
			if c2.res.committed || c2.res.body.Len() != 0 {
				t.Fatalf("Bytes(%d) committed=%v body=%q, want uncommitted & empty", code, c2.res.committed, c2.res.body.String())
			}

			c3 := New(nil)
			rc := &trackingReadCloser{r: strings.NewReader("ignored")}
			if err := c3.DataFromReader(code, "text/plain", rc, 7); err != nil {
				t.Fatalf("DataFromReader(%d): %v", code, err)
			}
			if c3.res.committed || c3.res.body.Len() != 0 || rc.read != 0 || !rc.closed {
				t.Fatalf("DataFromReader(%d) committed=%v body=%q read=%d closed=%v", code, c3.res.committed, c3.res.body.String(), rc.read, rc.closed)
			}
			if cl := c3.ResponseHeaders().Get("Content-Length"); cl != "" {
				t.Fatalf("DataFromReader(%d) Content-Length = %q, want empty", code, cl)
			}
		})
	}

	for _, code := range []int{http.StatusSwitchingProtocols, http.StatusNoContent, http.StatusNotModified} {
		t.Run("committed_bodyless", func(t *testing.T) {
			c := New(nil)
			rc := &trackingReadCloser{r: strings.NewReader("secret")}
			if err := c.DataFromReader(code, "text/plain", rc, 6); err != nil {
				t.Fatalf("DataFromReader(%d): %v", code, err)
			}
			if c.StatusCode() != code || c.res.body.Len() != 0 || rc.read != 0 || !rc.closed {
				t.Fatalf("status=%d body=%q read=%d closed=%v", c.StatusCode(), c.res.body.String(), rc.read, rc.closed)
			}
			if cl := c.ResponseHeaders().Get("Content-Length"); cl != "" {
				t.Fatalf("Content-Length = %q, want empty", cl)
			}
		})
	}

	// Post-commit 204 -> 200 drops body.
	c204 := New(nil)
	_ = c204.NoContent(http.StatusNoContent)
	_ = c204.Text(http.StatusOK, "should drop")
	if c204.StatusCode() != http.StatusNoContent || c204.BodyString() != "" {
		t.Fatalf("204->200 status=%d body=%q", c204.StatusCode(), c204.BodyString())
	}

	// Post-commit 200 -> 204 appends body to committed 200 response.
	c200 := New(nil)
	_ = c200.Text(http.StatusOK, "first ")
	_ = c200.Text(http.StatusNoContent, "second")
	if c200.StatusCode() != http.StatusOK || c200.BodyString() != "first second" {
		t.Fatalf("200->204 status=%d body=%q", c200.StatusCode(), c200.BodyString())
	}
}

func TestHostExcludedAndHeadersCanonicalAppend(t *testing.T) {
	c := New(nil, WithHeader("Host", "example.com"))
	if c.req.Host != "example.com" {
		t.Fatalf("req.Host = %q, want example.com", c.req.Host)
	}
	if got := c.Header("Host"); got != "" {
		t.Fatalf("Header(\"Host\") = %q, want empty", got)
	}
	if hdrs := c.Headers(); hdrs != nil {
		t.Fatalf("Headers() = %v, want nil when only Host was set", hdrs)
	}

	// Simulate manual req.Header injection with non-canonical keys + Host.
	c.req.Header["Host"] = []string{"bad.example"}
	c.req.Header["x-custom"] = []string{"a"}
	c.req.Header["X-Custom"] = []string{"b"}
	if got := c.Header("host"); got != "" {
		t.Fatalf("Header(\"host\") = %q, want empty", got)
	}
	hdrs := c.Headers()
	if _, hasHost := hdrs["Host"]; hasHost {
		t.Fatalf("Headers() contains Host: %v", hdrs)
	}
	gotCustom := slices.Clone(hdrs["X-Custom"])
	slices.Sort(gotCustom)
	if !slices.Equal(gotCustom, []string{"a", "b"}) {
		t.Fatalf("Headers()[\"X-Custom\"] = %v, want [a b]", gotCustom)
	}
	if got := c.Header("x-custom"); got != "a" && got != "b" {
		t.Fatalf("Header(\"x-custom\") = %q, want a or b", got)
	}
}

func TestBareRequestAndNoBodyAndBadQueryMultipart(t *testing.T) {
	// Zero-value &http.Request{} with WithHeader and WithCookie must not panic.
	bare := New(&http.Request{}, WithHeader("X-Key", "val"), WithCookie(&http.Cookie{Name: "c", Value: "1"}))
	if bare.Path() != "" || bare.RawQuery() != "" || bare.Query("k") != "" || bare.Queries() != nil {
		t.Fatalf("bare request URL info = (%q, %q, %q, %v)", bare.Path(), bare.RawQuery(), bare.Query("k"), bare.Queries())
	}
	if bare.Header("X-Key") != "val" {
		t.Fatalf("Header = %q, want val", bare.Header("X-Key"))
	}
	if ck, err := bare.Cookie("c"); err != nil || ck != "1" {
		t.Fatalf("Cookie = (%q, %v)", ck, err)
	}

	// http.NoBody in BodyRaw returns nil without replacement.
	noBodyCtx := New(httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	raw, err := noBodyCtx.BodyRaw()
	if err != nil || raw != nil {
		t.Fatalf("BodyRaw(NoBody) = (%v, %v), want (nil, nil)", raw, err)
	}

	// Malformed URL query escape must not break MultipartForm/FormFile.
	mpReq := httptest.NewRequest(http.MethodPost, "/upload?bad=%ZZ", nil)
	var body bytes.Buffer
	mw := multipartWriterForTest(t, &body, "file", "a.txt", "hello")
	mpReq.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	mpReq.Header.Set("Content-Type", mw)
	mpCtx := New(mpReq)
	fh, err := mpCtx.FormFile("file")
	if err != nil || fh.Filename != "a.txt" {
		t.Fatalf("FormFile with bad RawQuery = (%v, %v)", fh, err)
	}
}

func multipartWriterForTest(t *testing.T, buf *bytes.Buffer, field, filename, content string) string {
	t.Helper()
	w := io.Writer(buf)
	_ = w
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	_ = req
	// Build minimal multipart payload manually to keep imports minimal.
	boundary := "httpxmocktestboundary"
	buf.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"" + field + "\"; filename=\"" + filename + "\"\r\nContent-Type: application/octet-stream\r\n\r\n" + content + "\r\n--" + boundary + "--\r\n")
	return "multipart/form-data; boundary=" + boundary
}

func TestResponseMockParityEdges(t *testing.T) {
	// SetHeader(""), JSON, post-commit Redirect, HEAD Content-Length, Flush after 1xx, Writes clone.
	c := New(nil)
	c.SetHeader("Content-Type", "")
	if err := c.JSON(http.StatusOK, map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	w0 := c.Writes()
	if len(w0) != 1 || w0[0].ContentType != "" {
		t.Fatalf("Writes() = %+v, want 1 write with empty ContentType", w0)
	}

	c2 := New(nil)
	_ = c2.Text(http.StatusOK, "first")
	c2.SetHeader("X-Late", "no")
	c2.SetCookie(&http.Cookie{Name: "late", Value: "no"})
	if err := c2.Redirect(http.StatusFound, "/to"); err != nil {
		t.Fatal(err)
	}
	if c2.res.header.Get("X-Late") != "" || len(c2.ResponseCookies()) != 0 {
		t.Fatalf("post-commit SetHeader/SetCookie mutated header map: %v", c2.res.header)
	}
	if c2.BodyString() != "first" || c2.StatusCode() != http.StatusOK {
		t.Fatalf("post-commit Redirect altered status/body: %d %q", c2.StatusCode(), c2.BodyString())
	}
	writes := c2.Writes()
	if len(writes) < 1 || len(writes[0].Body) < 1 {
		t.Fatalf("Writes() = %+v, want body bytes", writes)
	}
	writes[0].Body[0] = 'X'
	writesAfter := c2.Writes()
	if len(writesAfter) < 1 || len(writesAfter[0].Body) < 1 || writesAfter[0].Body[0] != 'f' {
		t.Fatal("mutating Writes()[0].Body corrupted internal write record")
	}

	// HEAD Content-Length preserved when body is empty.
	headCtx := NewRequest(http.MethodHead, "/", nil)
	headCtx.SetHeader("Content-Length", "42")
	_ = headCtx.NoContent(http.StatusOK)
	if res := headCtx.Result(); res.ContentLength != 42 {
		t.Fatalf("HEAD Result().ContentLength = %d, want 42", res.ContentLength)
	}

	// Flush after 103 commits 200.
	earlyCtx := New(nil)
	earlyCtx.Status(http.StatusEarlyHints)
	_ = earlyCtx.Flush()
	if !earlyCtx.Committed() || earlyCtx.StatusCode() != http.StatusOK {
		t.Fatalf("Flush after 103: committed=%v status=%d, want true / 200", earlyCtx.Committed(), earlyCtx.StatusCode())
	}

	// DataFromReader with nil reader must not set Content-Length: 10.
	nilReaderCtx := New(nil)
	_ = nilReaderCtx.DataFromReader(http.StatusOK, "text/plain", nil, 10)
	if cl := nilReaderCtx.ResponseHeader("Content-Length"); cl != "" {
		t.Fatalf("DataFromReader(nil) Content-Length = %q, want empty", cl)
	}

	// Stream(204) drops written bytes.
	s204 := New(nil)
	_ = s204.Stream(http.StatusNoContent, "text/plain", func(w io.Writer) error {
		_, err := io.WriteString(w, "ignored")
		return err
	})
	sWrites := s204.Writes()
	if s204.BodyString() != "" || len(sWrites) != 1 || len(sWrites[0].Body) != 0 {
		t.Fatalf("Stream(204) leaked body: %q / %+v", s204.BodyString(), sWrites)
	}
}

func TestConcurrentBodyAccessRaceFree(t *testing.T) {
	c := NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = c.BodyRaw()
	}()
	go func() {
		defer wg.Done()
		r := c.BodyReader()
		_, _ = io.ReadAll(r)
		_ = r.Close()
	}()
	wg.Wait()
}
