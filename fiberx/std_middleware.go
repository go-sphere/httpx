package fiberx

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

// AdaptStdMiddleware wraps a plain net/http middleware
// (func(http.Handler) http.Handler) as httpx.Middleware, giving access to the
// otel/chi/gzip ecosystem. Because fiber buffers the response, the downstream
// chain runs first into fiber's buffer, which is then replayed through the
// middleware's (possibly wrapped) writer, so body/header transformations
// apply. Streaming responses (DataFromReader with unknown size) are not
// replayed through the wrapper. If the middleware responds without calling
// the next handler, the chain is short-circuited.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			return errors.New("AdaptStdMiddleware: fiber context type error")
		}
		req, err := adaptor.ConvertRequest(fc, true)
		if err != nil {
			return err
		}
		var nextErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx.SetContext(r.Context())
			nextErr = ctx.Next()
			replayFiberResponse(fc, w)
		})
		w := &fiberResponseWriter{fc: fc}
		middleware(inner).ServeHTTP(w, req)
		w.finish()
		return nextErr
	}
}

// replayFiberResponse moves the buffered downstream response out of the
// fiber context and replays it through w (the middleware's view of the
// response), so wrapping middleware observes and may transform it.
func replayFiberResponse(fc fiber.Ctx, w http.ResponseWriter) {
	resp := fc.Response()
	status := resp.StatusCode()
	header := w.Header()
	for k, v := range resp.Header.All() {
		header.Add(string(k), string(v))
	}
	body := bytes.Clone(resp.Body())
	resp.Reset()
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

// fiberResponseWriter writes into fiber's buffered response, staging headers
// until the first write like net/http does.
type fiberResponseWriter struct {
	fc          fiber.Ctx
	header      http.Header
	wroteHeader bool
}

func (w *fiberResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *fiberResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	resp := w.fc.Response()
	for key, values := range w.header {
		if strings.EqualFold(key, "Content-Length") {
			if len(values) > 0 {
				if n, err := strconv.Atoi(values[0]); err == nil {
					resp.Header.SetContentLength(n)
				}
			}
			continue
		}
		resp.Header.Del(key)
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
	resp.SetStatusCode(code)
}

func (w *fiberResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.fc.Response().AppendBody(p)
	return len(p), nil
}

func (w *fiberResponseWriter) finish() {
	if !w.wroteHeader {
		w.WriteHeader(w.fc.Response().StatusCode())
	}
}
