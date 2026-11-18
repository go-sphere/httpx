package stdx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

type unwrapLayerWriter struct {
	w http.ResponseWriter
}

func (u *unwrapLayerWriter) Header() http.Header         { return u.w.Header() }
func (u *unwrapLayerWriter) Write(p []byte) (int, error) { return u.w.Write(p) }
func (u *unwrapLayerWriter) WriteHeader(code int)        { u.w.WriteHeader(code) }
func (u *unwrapLayerWriter) Unwrap() http.ResponseWriter { return u.w }

func TestStdxContextEdges(t *testing.T) {
	t.Run("SetContext_nil_ignored", func(t *testing.T) {
		c := FromStd(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		c.SetContext(nil) //nolint:staticcheck // intentionally testing nil guard
		if c.Context() == nil {
			t.Fatal("Context() became nil after SetContext(nil)")
		}
	})

	t.Run("DataFromReader_nil_and_Stream_nil", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := FromStd(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if err := c.DataFromReader(http.StatusCreated, "text/plain", nil, 0); err != nil {
			t.Fatalf("DataFromReader(nil): %v", err)
		}
		if rec.Code != http.StatusCreated || rec.Body.Len() != 0 {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}

		rec2 := httptest.NewRecorder()
		c2 := FromStd(rec2, httptest.NewRequest(http.MethodGet, "/", nil)).(httpx.Streamer)
		if err := c2.Stream(http.StatusAccepted, "text/plain", nil); err != nil {
			t.Fatalf("Stream(nil): %v", err)
		}
		if rec2.Code != http.StatusAccepted || !rec2.Flushed {
			t.Fatalf("Stream(nil) status=%d flushed=%v", rec2.Code, rec2.Flushed)
		}
	})

	t.Run("flush_walks_Unwrap", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapped := &unwrapLayerWriter{w: &unwrapLayerWriter{w: rec}}
		if err := flush(wrapped); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if !rec.Flushed {
			t.Fatal("flush did not reach inner httptest.ResponseRecorder via Unwrap")
		}
	})

	t.Run("BindForm_ignores_bad_query_escape_and_rejects_unbalanced_brackets", func(t *testing.T) {
		type formIn struct {
			Name string `form:"name"`
		}
		req := httptest.NewRequest(http.MethodPost, "/?bad=%ZZ", strings.NewReader("name=alice"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		c := FromStd(httptest.NewRecorder(), req)
		var out formIn
		if err := c.BindForm(&out); err != nil {
			t.Fatalf("BindForm with bad query escape: %v", err)
		}
		if out.Name != "alice" {
			t.Fatalf("out.Name = %q, want alice", out.Name)
		}

		for _, key := range []string{"a[", "a]", "a[[0]]"} {
			reqBad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(key+"=1"))
			reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			cb := FromStd(httptest.NewRecorder(), reqBad)
			err := cb.BindForm(&out)
			if err == nil {
				t.Fatalf("BindForm(%q) = nil, want 400 WrapBindError", key)
			}
			_, status, _ := httpx.ParseError(err)
			if status != http.StatusBadRequest {
				t.Fatalf("BindForm(%q) status = %d, want 400", key, status)
			}
		}
	})

	t.Run("Headers_canonical_append", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["x-dup"] = []string{"a"}
		req.Header["X-Dup"] = []string{"b"}
		c := FromStd(httptest.NewRecorder(), req)
		vals := slices.Clone(c.Headers()["X-Dup"])
		slices.Sort(vals)
		if !slices.Equal(vals, []string{"a", "b"}) {
			t.Fatalf("Headers()[\"X-Dup\"] = %v", vals)
		}
	})

	t.Run("isMultipart_precise", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodPost, "/", nil)
		req1.Header.Set("Content-Type", " multipart/form-data ; boundary=abc")
		if !isMultipart(req1) {
			t.Fatal("expected multipart/form-data to match")
		}
		req2 := httptest.NewRequest(http.MethodPost, "/", nil)
		req2.Header.Set("Content-Type", "multipart/related; boundary=abc")
		if isMultipart(req2) {
			t.Fatal("multipart/related must not match isMultipart")
		}
	})

	t.Run("JSON_204_marshal_error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := FromStd(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if err := c.JSON(http.StatusNoContent, make(chan int)); err == nil {
			t.Fatal("JSON(204, chan) = nil, want error")
		}
		if c.Committed() {
			t.Fatal("JSON(204, chan) must not commit on marshal error")
		}
	})

	t.Run("recycle_drops_oversized_query_and_clears_state", func(t *testing.T) {
		var qb strings.Builder
		for i := range maxRetainedKeys + 1 {
			fmt.Fprintf(&qb, "k%d=v&", i)
		}
		c := FromStd(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?"+qb.String(), nil)).(*stdContext)
		_ = c.queryValues()
		c.values = append(c.values, "secret-param")
		c.Set("k", "v")
		c.recycle()
		if c.req != nil || c.rw.ResponseWriter != nil || c.route != nil || c.query != nil || c.queryRaw != "" || len(c.keys) != 0 {
			t.Fatalf("recycle left state behind: req=%v query=%v queryRaw=%q keys=%v", c.req, c.query, c.queryRaw, c.keys)
		}
		if c.valueBuf[0] != "" {
			t.Fatalf("valueBuf not cleared: %v", c.valueBuf)
		}
	})

	t.Run("double_Start_returns_errAlreadyStarted", func(t *testing.T) {
		e := New(WithAddr("127.0.0.1:0")).(*Engine)
		e.Group("").GET("/ping", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "pong") })

		firstDone := make(chan error, 1)
		go func() { firstDone <- e.Start() }()

		deadline := time.Now().Add(2 * time.Second)
		for !e.IsRunning() {
			if time.Now().After(deadline) {
				t.Fatal("engine failed to start")
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := e.Start(); !errors.Is(err, errAlreadyStarted) {
			t.Fatalf("second Start() = %v, want %v", err, errAlreadyStarted)
		}
		if err := e.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if err := <-firstDone; err != nil {
			t.Fatalf("first Start() = %v", err)
		}
	})

	t.Run("jsonBodyWriter_respects_empty_ContentType_header_key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := FromStd(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		c.SetHeader("Content-Type", "")
		if err := c.JSON(http.StatusOK, map[string]int{"a": 1}); err != nil {
			t.Fatalf("JSON: %v", err)
		}
		var b bytes.Buffer
		b.WriteString(rec.Header().Get("Content-Type"))
		if b.String() != "" {
			t.Fatalf("preset empty Content-Type overwritten with %q", b.String())
		}
	})

	t.Run("PostCommitRedirectAndNilReaderAndStream204AndNilBind", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := FromStd(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		_ = c.Text(http.StatusOK, "first")
		if err := c.Redirect(http.StatusFound, "/to"); err != nil {
			t.Fatal(err)
		}
		if rec.Body.String() != "first" || rec.Header().Get("Location") != "" {
			t.Fatalf("post-commit Redirect wrote body=%q Location=%q", rec.Body.String(), rec.Header().Get("Location"))
		}

		rec2 := httptest.NewRecorder()
		c2 := FromStd(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
		if err := c2.DataFromReader(http.StatusOK, "text/plain", nil, 10); err != nil {
			t.Fatal(err)
		}
		if cl := rec2.Header().Get("Content-Length"); cl != "" {
			t.Fatalf("DataFromReader(nil, 10) Content-Length = %q, want empty", cl)
		}

		rec3 := httptest.NewRecorder()
		c3 := FromStd(rec3, httptest.NewRequest(http.MethodGet, "/", nil)).(httpx.Streamer)
		if err := c3.Stream(http.StatusNoContent, "text/plain", func(w io.Writer) error {
			_, err := io.WriteString(w, "ignored")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if rec3.Body.Len() != 0 {
			t.Fatalf("Stream(204) body = %q, want empty", rec3.Body.String())
		}

		c4 := FromStd(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
		for _, fn := range []func(any) error{c4.BindJSON, c4.BindQuery, c4.BindForm, c4.BindURI, c4.BindHeader} {
			if _, status, _ := httpx.ParseError(fn(nil)); status != http.StatusBadRequest {
				t.Fatalf("bind(nil) status = %d, want 400", status)
			}
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("k", "v")
		_ = mw.Close()
		mpReq := httptest.NewRequest(http.MethodPost, "/?bad=%ZZ", &body)
		mpReq.Header.Set("Content-Type", mw.FormDataContentType())
		mpCtx := FromStd(httptest.NewRecorder(), mpReq)
		if mf, err := mpCtx.MultipartForm(); err != nil || len(mf.Value["k"]) != 1 || mf.Value["k"][0] != "v" {
			t.Fatalf("MultipartForm with bad RawQuery = (%v, %v)", mf, err)
		}
	})

	t.Run("AdaptStdMiddlewarePropagatesMarkWritten", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var committed bool
		var code int
		r.Use(func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				err := next(ctx)
				committed = ctx.Committed()
				code = ctx.StatusCode()
				return err
			}
		})
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req)
			})
		}))
		r.GET("/mw", func(ctx httpx.Context) error {
			native, ok := httpx.AsNativeContext[*Native](ctx)
			if !ok {
				return errors.New("missing Native")
			}
			native.MarkWritten(http.StatusAccepted)
			return errors.New("must not render over MarkWritten")
		})
		rec := serve(engine, getReq("/mw"))
		if !committed || code != http.StatusAccepted || rec.Body.Len() != 0 {
			t.Fatalf("MarkWritten propagation: committed=%v code=%d body=%q", committed, code, rec.Body.String())
		}
	})
}
