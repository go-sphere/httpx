package httpxmock

import (
	"io"
	"net/http"
	"slices"
	"strings"
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
}
