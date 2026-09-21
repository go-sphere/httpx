package httpxmock_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

// TestWriteAfterCommitMatchesNetHTTP pins the three halves of net/http's rule
// in one place, because a mock that got any of them wrong would quietly
// disagree with ginx, echox and stdx, which all behave this way: the status
// freezes, the headers freeze, and the body appends. None of it is an error.
func TestWriteAfterCommitMatchesNetHTTP(t *testing.T) {
	ctx := httpxmock.New(nil)

	ctx.SetHeader("X-Before", "kept")
	if err := ctx.JSON(http.StatusOK, map[string]string{"first": "a"}); err != nil {
		t.Fatalf("first JSON: %v", err)
	}

	// Everything below happens after the response committed.
	ctx.Status(http.StatusTeapot)
	ctx.SetHeader("X-After", "dropped")
	ctx.SetHeader("X-Before", "overwritten")
	ctx.SetCookie(&http.Cookie{Name: "late", Value: "dropped"})
	if err := ctx.JSON(http.StatusInternalServerError, map[string]string{"second": "b"}); err != nil {
		t.Fatalf("second JSON: %v", err)
	}
	if err := ctx.Text(http.StatusBadRequest, "tail"); err != nil {
		t.Fatalf("Text: %v", err)
	}

	if got := ctx.StatusCode(); got != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200: the first write freezes the status", got)
	}
	if got := ctx.ResponseHeader("X-Before"); got != "kept" {
		t.Errorf("X-Before = %q, want %q", got, "kept")
	}
	if got := ctx.ResponseHeader("X-After"); got != "" {
		t.Errorf("X-After = %q, want empty: headers freeze at commit", got)
	}
	if cookies := ctx.ResponseCookies(); len(cookies) != 0 {
		t.Errorf("ResponseCookies = %v, want none", cookies)
	}
	want := `{"first":"a"}{"second":"b"}tail`
	if got := ctx.BodyString(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	// The calls still happened, and the codes they asked for are recorded
	// even though none of them took effect.
	writes := ctx.Writes()
	if len(writes) != 3 {
		t.Fatalf("Writes() = %d entries, want 3", len(writes))
	}
	if writes[1].Code != http.StatusInternalServerError {
		t.Errorf("writes[1].Code = %d, want 500", writes[1].Code)
	}
}

func TestStatusBeforeCommitIsPending(t *testing.T) {
	ctx := httpxmock.New(nil)

	if got := ctx.StatusCode(); got != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 before anything is written", got)
	}
	if ctx.Committed() {
		t.Error("Committed = true before anything is written")
	}

	ctx.Status(http.StatusCreated)
	if got := ctx.StatusCode(); got != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", got)
	}
	if ctx.Committed() {
		t.Error("Committed = true after a bare Status: it writes no body")
	}

	if err := ctx.Text(http.StatusAccepted, "body"); err != nil {
		t.Fatal(err)
	}
	if got := ctx.StatusCode(); got != http.StatusAccepted {
		t.Errorf("StatusCode = %d, want 202", got)
	}
	if !ctx.Committed() {
		t.Error("Committed = false after Text")
	}
}

func TestResponders(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		payload := map[string]bool{"ok": true}
		if err := ctx.JSON(http.StatusCreated, payload); err != nil {
			t.Fatal(err)
		}
		if got := ctx.StatusCode(); got != http.StatusCreated {
			t.Errorf("status = %d, want 201", got)
		}
		if got := ctx.ResponseHeader("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("content-type = %q", got)
		}
		if got := ctx.BodyString(); got != `{"ok":true}` {
			t.Errorf("body = %q", got)
		}
		// The unencoded value is kept so a test can assert on the typed
		// envelope a handler built rather than on its JSON shape.
		last, ok := ctx.LastJSON()
		if !ok {
			t.Fatal("LastJSON: want ok")
		}
		if _, isMap := last.(map[string]bool); !isMap {
			t.Errorf("LastJSON type = %T, want map[string]bool", last)
		}
	})

	t.Run("JSONEncodeFailureWritesNothing", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		err := ctx.JSON(http.StatusOK, make(chan int))
		if err == nil {
			t.Fatal("JSON: want an error for an unencodable value")
		}
		if ctx.Committed() {
			t.Error("Committed = true: a failed encode must not commit")
		}
		if got := ctx.BodyString(); got != "" {
			t.Errorf("body = %q, want empty", got)
		}
	})

	t.Run("Text", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.Text(http.StatusOK, "hello"); err != nil {
			t.Fatal(err)
		}
		if got := ctx.ResponseHeader("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("content-type = %q", got)
		}
		if got := ctx.BodyString(); got != "hello" {
			t.Errorf("body = %q", got)
		}
	})

	t.Run("NoContent", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.NoContent(http.StatusNoContent); err != nil {
			t.Fatal(err)
		}
		if got := ctx.StatusCode(); got != http.StatusNoContent {
			t.Errorf("status = %d, want 204", got)
		}
		if got := ctx.BodyString(); got != "" {
			t.Errorf("body = %q, want empty", got)
		}
		if got := ctx.ResponseHeader("Content-Type"); got != "" {
			t.Errorf("content-type = %q, want empty", got)
		}
		// Distinguishable from a 204 that arrived some other way.
		writes := ctx.Writes()
		if len(writes) != 1 || writes[0].Kind != httpxmock.KindNoContent {
			t.Errorf("Writes() = %+v, want one NoContent", writes)
		}
	})

	t.Run("BodylessStatusDropsTheBody", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.JSON(http.StatusNotModified, map[string]string{"a": "b"}); err != nil {
			t.Fatal(err)
		}
		if got := ctx.BodyString(); got != "" {
			t.Errorf("body = %q, want empty for 304", got)
		}
		if got := ctx.StatusCode(); got != http.StatusNotModified {
			t.Errorf("status = %d, want 304", got)
		}
	})

	t.Run("BytesSniffsEmptyContentType", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.Bytes(http.StatusOK, []byte("plain text payload"), ""); err != nil {
			t.Fatal(err)
		}
		got := ctx.ResponseHeader("Content-Type")
		if !strings.HasPrefix(got, "text/plain") {
			t.Errorf("content-type = %q, want sniffed text/plain", got)
		}
		if got := ctx.Writes()[0].ContentType; !strings.HasPrefix(got, "text/plain") {
			t.Errorf("recorded content-type = %q, want the sniffed value", got)
		}
	})

	t.Run("BytesKeepsAnExplicitContentType", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.Bytes(http.StatusOK, []byte("abc"), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		if got := ctx.ResponseHeader("Content-Type"); got != "application/octet-stream" {
			t.Errorf("content-type = %q", got)
		}
	})
}

// closeReader reports whether DataFromReader closed what it was handed, which
// the Responder contract requires of an io.Closer.
type closeReader struct {
	io.Reader
	closed bool
}

func (r *closeReader) Close() error {
	r.closed = true
	return nil
}

func TestDataFromReader(t *testing.T) {
	t.Run("KnownSize", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		src := &closeReader{Reader: strings.NewReader("stream")}
		if err := ctx.DataFromReader(http.StatusOK, "text/plain", src, 6); err != nil {
			t.Fatal(err)
		}
		if got := ctx.BodyString(); got != "stream" {
			t.Errorf("body = %q", got)
		}
		if got := ctx.ResponseHeader("Content-Length"); got != "6" {
			t.Errorf("content-length = %q, want 6", got)
		}
		if !src.closed {
			t.Error("reader was not closed")
		}
	})

	t.Run("UnknownSize", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		if err := ctx.DataFromReader(http.StatusOK, "text/plain", strings.NewReader("stream"), -1); err != nil {
			t.Fatal(err)
		}
		if got := ctx.ResponseHeader("Content-Length"); got != "" {
			t.Errorf("content-length = %q, want unset for an unknown size", got)
		}
		if got := ctx.BodyString(); got != "stream" {
			t.Errorf("body = %q", got)
		}
	})

	t.Run("ReadFailurePropagates", func(t *testing.T) {
		ctx := httpxmock.New(nil)
		want := errors.New("boom")
		r := io.MultiReader(strings.NewReader("half"), errReader{want})
		err := ctx.DataFromReader(http.StatusOK, "text/plain", r, -1)
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
		// The response committed before the copy started, so the partial
		// body stands: that is what a real connection would have sent.
		if got := ctx.BodyString(); got != "half" {
			t.Errorf("body = %q, want the bytes that got through", got)
		}
	})
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestRedirect(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		ctx := httpxmock.NewRequest(http.MethodGet, "/old", nil)
		if err := ctx.Redirect(http.StatusFound, "/new"); err != nil {
			t.Fatal(err)
		}
		if got := ctx.StatusCode(); got != http.StatusFound {
			t.Errorf("status = %d, want 302", got)
		}
		if got := ctx.ResponseHeader("Location"); got != "/new" {
			t.Errorf("location = %q", got)
		}
	})

	for _, code := range []int{http.StatusOK, 299, 309, http.StatusBadRequest} {
		t.Run("Rejected", func(t *testing.T) {
			ctx := httpxmock.New(nil)
			err := ctx.Redirect(code, "/new")
			if err == nil {
				t.Fatalf("Redirect(%d): want an error", code)
			}
			if ctx.Committed() {
				t.Errorf("Redirect(%d): committed a response it should have refused", code)
			}
			if got := ctx.ResponseHeader("Location"); got != "" {
				t.Errorf("Redirect(%d): wrote Location = %q", code, got)
			}
		})
	}
}

func TestFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := httpxmock.NewRequest(http.MethodGet, "/download", nil)
	if err := ctx.File(path); err != nil {
		t.Fatal(err)
	}
	if got := ctx.StatusCode(); got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
	if got := ctx.BodyString(); got != "from-file" {
		t.Errorf("body = %q", got)
	}
	if got := ctx.ResponseHeader("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content-type = %q", got)
	}
}

func TestHeadersAndCookiesAreReadBack(t *testing.T) {
	ctx := httpxmock.New(nil)
	ctx.SetHeader("X-Trace", "ok")
	ctx.SetCookie(&http.Cookie{
		Name:     "session",
		Value:    "abc",
		Path:     "/app",
		MaxAge:   600,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC),
	})
	if err := ctx.Text(http.StatusOK, "cookie"); err != nil {
		t.Fatal(err)
	}

	if got := ctx.ResponseHeader("X-Trace"); got != "ok" {
		t.Errorf("X-Trace = %q", got)
	}
	cookies := ctx.ResponseCookies()
	if len(cookies) != 1 {
		t.Fatalf("ResponseCookies = %d, want 1", len(cookies))
	}
	got := cookies[0]
	if got.Name != "session" || got.Value != "abc" || got.Path != "/app" ||
		got.MaxAge != 600 || !got.Secure || !got.HttpOnly || got.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v", got)
	}

	// Result is the same response seen through the standard library.
	res := ctx.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Result().StatusCode = %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, []byte("cookie")) {
		t.Errorf("Result() body = %q", body)
	}
	// Reading Result must not consume the recorded body.
	if got := ctx.BodyString(); got != "cookie" {
		t.Errorf("body after Result = %q", got)
	}
}

// TestDefaultErrorHandlerRendersThroughTheMock is the end the package is for:
// a shared piece of httpx machinery driven against the mock and asserted on.
func TestDefaultErrorHandlerRendersThroughTheMock(t *testing.T) {
	ctx := httpxmock.New(nil)
	httpx.DefaultErrorHandler(ctx, httpx.NewForbiddenError("nope"))

	if got := ctx.StatusCode(); got != http.StatusForbidden {
		t.Errorf("status = %d, want 403", got)
	}
	if got := ctx.BodyString(); got != `{"success":false,"code":0,"message":"nope"}` {
		t.Errorf("body = %q", got)
	}
}
