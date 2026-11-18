package hertzx

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/go-sphere/httpx"
)

// A Stop issued while Start is still on its way to the listener must still end
// Start: hertz marks itself running only after its hooks and binds after
// that, so a single shutdown can land before there is anything to close.
func TestStopRightAfterStartEndsStart(t *testing.T) {
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	for i := range 30 {
		engine := New(WithAddr("127.0.0.1:0")).(*Engine)
		started := make(chan error, 1)
		go func() { started <- engine.Start() }()
		for !engine.IsRunning() {
			runtime.Gosched()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stopErr := engine.Stop(ctx)
		cancel()
		select {
		case err := <-started:
			if err != nil {
				t.Fatalf("iteration %d: Start returned %v, want nil", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Start did not return after Stop (Stop returned %v, IsRunning %v)", i, stopErr, engine.IsRunning())
		}
		if stopErr != nil {
			t.Fatalf("iteration %d: Stop returned %v, want nil", i, stopErr)
		}
		if engine.IsRunning() {
			t.Fatalf("iteration %d: IsRunning is true after Start returned", i)
		}
	}
}

// Stop's drain is bounded by the caller's context, not by hertz's
// ExitWaitTimeout default of 5 seconds.
func TestShutdownWaitFollowsContext(t *testing.T) {
	engine := New().(*Engine)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The engine is not running, so hertz refuses; the option is set first.
	_ = engine.shutdown(ctx)
	if got := engine.engine.GetOptions().ExitWaitTimeout; got < 59*time.Second || got > time.Minute {
		t.Fatalf("ExitWaitTimeout = %v with a one-minute deadline", got)
	}
	_ = engine.shutdown(context.Background())
	if got := engine.engine.GetOptions().ExitWaitTimeout; got != math.MaxInt64 {
		t.Fatalf("ExitWaitTimeout = %v without a deadline, want no limit", got)
	}
}

// WithErrorHandler(nil) keeps the handler configured before it.
func TestWithErrorHandlerNilIsIgnored(t *testing.T) {
	engine := New(
		WithErrorHandler(func(ctx httpx.Context, err error) {
			_ = ctx.Text(http.StatusTeapot, "kept")
		}),
		WithErrorHandler(nil),
	)
	engine.Group("").GET("/fail", func(httpx.Context) error {
		return errors.New("boom")
	})
	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "http://example.com/fail", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusTeapot || string(body) != "kept" {
		t.Fatalf("status=%d body=%q, want 418 %q", resp.StatusCode, body, "kept")
	}
}

func TestHertzxBodyRawSurvivesRequestContextReuse(t *testing.T) {
	const (
		firstBody  = "body-of-the-first-request"
		secondBody = "body-of-the-second-reques" // same length, different bytes
	)
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	native := server.New()
	engine := New(WithEngine(native))

	var captured []byte
	engine.Group("").POST("/body/capture", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		captured = raw
		return ctx.Text(http.StatusOK, string(raw))
	})
	engine.Group("").POST("/body", func(ctx httpx.Context) error {
		raw, err := ctx.BodyRaw()
		if err != nil {
			return err
		}
		return ctx.Text(http.StatusOK, string(raw))
	})

	rc := native.NewContext()
	ctx := context.Background()
	serve := func(path, body string) {
		t.Helper()
		rc.ResetWithoutConn()
		rc.Request.Header.SetMethod(http.MethodPost)
		rc.Request.SetRequestURI("http://example.com" + path)
		rc.Request.SetBodyString(body)
		native.ServeHTTP(ctx, rc)
		if status := rc.Response.StatusCode(); status != http.StatusOK {
			t.Fatalf("POST %s: status = %d, want 200", path, status)
		}
	}

	serve("/body/capture", firstBody)
	serve("/body", secondBody)

	if string(captured) != firstBody {
		t.Fatalf("body captured = %q, want %q", captured, firstBody)
	}
}

func TestFileNoSidecarNoCacheAndReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := New()
	engine.Group("").GET("/file", func(ctx httpx.Context) error {
		return ctx.File(path)
	})
	doFile := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/file", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := engine.(*Engine).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /file status=%d body=%q", resp.StatusCode, body)
		}
		return string(body)
	}

	if got := doFile(); got != "v1" {
		t.Fatalf("first read = %q, want v1", got)
	}
	if _, err := os.Stat(path + ".hertz.gz"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected sidecar file stat err = %v, want not-exist", err)
	}

	if err := os.WriteFile(path, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := doFile(); got != "v2" {
		t.Fatalf("second read after overwrite = %q, want v2 (stale cache)", got)
	}

	if os.Geteuid() != 0 {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if got := doFile(); got != "v2" {
			t.Fatalf("read from read-only dir = %q, want v2", got)
		}
	}
}

func TestStartOnOccupiedPortReturnsErrorWithoutPanic(t *testing.T) {
	hlog.SetSilentMode(true)
	hlog.SetOutput(io.Discard)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	engine := New(WithAddr(ln.Addr().String()))
	if err := engine.Start(); err == nil {
		t.Fatal("Start() on occupied address = nil, want error")
	}
	if engine.IsRunning() {
		t.Fatal("IsRunning() = true after failed Start")
	}
}

func TestResponseHeaderValuesEmptyAndUnset(t *testing.T) {
	engine := New()
	var emptyVal, clVal, unsetVal []string
	engine.Group("").GET("/hdr", func(ctx httpx.Context) error {
		editor, ok := httpx.AsResponseHeaderEditor(ctx)
		if !ok {
			t.Fatal("missing ResponseHeaderEditor")
		}
		ctx.SetHeader("X-Empty", "")
		emptyVal = editor.ResponseHeaderValues("X-Empty")
		clVal = editor.ResponseHeaderValues("Content-Length")
		unsetVal = editor.ResponseHeaderValues("X-Unset")
		return ctx.NoContent(http.StatusNoContent)
	})
	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "http://example.com/hdr", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !reflect.DeepEqual(emptyVal, []string{""}) {
		t.Fatalf("X-Empty = %#v, want []string{\"\"}", emptyVal)
	}
	if clVal != nil || unsetVal != nil {
		t.Fatalf("unset values = Content-Length:%#v X-Unset:%#v, want nil", clVal, unsetVal)
	}
}
