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
// apply. A streamed response (Stream, ServerSentEvents, DataFromReader with a
// body stream) is not replayed: it keeps its stream, the headers the
// middleware set before calling next, except Content-Length and
// Content-Encoding, are added where the handler did not set the same key, and
// whatever the middleware writes after next returns is dropped. If the
// middleware responds without calling the next handler, the chain is
// short-circuited. A downstream error that left the response uncommitted is
// not replayed: only the headers the middleware staged are applied, and the
// error is returned to be rendered as on the other adapters.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(next httpx.Handler) httpx.Handler { return next }
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
			if !ok {
				return errors.New("AdaptStdMiddleware: fiber context type error")
			}
			req, err := adaptor.ConvertRequest(fc, true)
			if err != nil {
				return err
			}
			// ConvertRequest builds a fresh request, so without this the
			// middleware — and the chain below, via ctx.SetContext(r.Context())
			// — would see context.Background() instead of the context the
			// layers above set, which is the discontinuity the other four
			// adapters do not have.
			req = req.WithContext(fc.Context())
			var nextErr error
			base := &fiberResponseWriter{fc: fc}
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx.SetContext(r.Context())
				nextErr = next(ctx)
				if nextErr != nil && !ctx.Committed() {
					return
				}
				if fc.Response().IsBodyStream() {
					// Body would read the stream to its end inside the
					// handler, so the client would get nothing until it ends.
					base.passStream()
					return
				}
				replayFiberResponse(fc, w)
			})
			middleware(inner).ServeHTTP(base, req)
			if nextErr != nil && !base.wroteHeader {
				// Replaying would commit an empty 200 and the error would
				// never be rendered.
				base.applyHeader()
			} else {
				base.finish()
			}
			return nextErr
		}
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
	// streaming is set once the downstream response is a body stream: any
	// write would replace that stream, so later writes are discarded.
	streaming bool
}

// passStream commits w over a streamed downstream response without touching
// its body: the staged headers are added for the keys the handler left unset.
func (w *fiberResponseWriter) passStream() {
	w.wroteHeader = true
	w.streaming = true
	markResponseCommitted(w.fc)
	resp := w.fc.Response()
	for key, values := range w.header {
		// The stream bypasses the middleware's writer, so a length or encoding
		// it staged would not describe the body.
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") || len(resp.Header.Peek(key)) > 0 {
			continue
		}
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
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
	markResponseCommitted(w.fc)
	w.applyHeader()
	w.fc.Response().SetStatusCode(code)
}

// applyHeader copies the staged headers into fiber's response without
// committing it.
func (w *fiberResponseWriter) applyHeader() {
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
}

func (w *fiberResponseWriter) Write(p []byte) (int, error) {
	if w.streaming {
		return len(p), nil
	}
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
