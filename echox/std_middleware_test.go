package echox

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

func TestStdMiddlewareShortCircuitThenError(t *testing.T) {
	e := echo.New()
	engine := New(WithEngine(e))
	r := engine.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			if err := next(ctx); err != nil {
				return err
			}
			ec, _ := httpx.AsNativeContext[echo.Context](ctx)
			if !ec.Response().Committed || ec.Response().Status != http.StatusUnauthorized {
				t.Errorf("response state = %+v", ec.Response())
			}
			return errors.New("late failure")
		}
	}, AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("blocked"))
		})
	}))
	r.GET("/", func(ctx httpx.Context) error {
		t.Error("handler ran after short circuit")
		return nil
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized || w.Body.String() != "blocked" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
}

type unwrapWriter struct{ http.ResponseWriter }

func (w unwrapWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestStdMiddlewarePreservesFlush(t *testing.T) {
	e := echo.New()
	engine := New(WithEngine(e))
	r := engine.Group("")
	w := httptest.NewRecorder()
	r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := w.(http.Flusher); !ok {
				t.Error("middleware writer lost http.Flusher")
			}
			next.ServeHTTP(unwrapWriter{w}, r)
		})
	}))
	r.GET("/", func(ctx httpx.Context) error {
		flusher, _ := httpx.AsFlusher(ctx)
		if err := flusher.Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
		if !w.Flushed {
			t.Error("Flush did not reach the underlying writer")
		}
		return ctx.Text(http.StatusOK, "stream")
	})
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "stream" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
}

func TestCommittedErrorReachesEchoMiddleware(t *testing.T) {
	for _, fromMiddleware := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler", true: "middleware"}[fromMiddleware], func(t *testing.T) {
			e := echo.New()
			want := errors.New("late failure")
			var observed error
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					observed = next(c)
					return observed
				}
			})
			engine := New(WithEngine(e), WithErrorHandler(func(ctx httpx.Context, err error) {
				t.Error("custom error handler ran after response was committed")
			}))
			r := engine.Group("")
			if fromMiddleware {
				r.Use(func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						if err := next(ctx); err != nil {
							return err
						}
						return want
					}
				})
			}
			r.GET("/", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "ok"); err != nil {
					return err
				}
				if fromMiddleware {
					return nil
				}
				return want
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if !errors.Is(observed, want) || w.Body.String() != "ok" {
				t.Fatalf("observed = %v, body = %q", observed, w.Body.String())
			}
		})
	}
}
