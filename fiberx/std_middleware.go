package fiberx

import (
	"bytes"
	"errors"
	"maps"
	"net/http"
	"net/textproto"
	"reflect"
	"slices"
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
// apply. The middleware's header map is the response's, as in net/http: it
// sees the headers the chain above set, what it sets or deletes before calling
// next is what the handlers below see (Content-Length and Content-Encoding
// excepted, which describe the body it writes), and what they set is what it
// sees afterwards. A streamed response (Stream, ServerSentEvents,
// DataFromReader with a body stream) is not replayed: it keeps its stream, and
// whatever the middleware writes after next returns is dropped. If the
// middleware responds without calling the next handler, the chain is
// short-circuited. A downstream response that is not committed (an error, or
// a handler that wrote nothing) is not replayed, and neither the middleware
// nor the adapter commits one the middleware did not write to: only the
// headers the middleware staged are applied, and an error is returned to be
// rendered as on the other adapters, including one a layer above returns
// after the middleware stopped the chain without writing.
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
			base := &fiberResponseWriter{fc: fc, late: responseDecided(fc)}
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// net/http shares one header map down the chain: what the
				// middleware staged is what the handlers below see, and what
				// they set is what the middleware sees after next.
				base.sync(false)
				ctx.SetContext(r.Context())
				nextErr = next(ctx)
				base.reseed()
				if !ctx.Committed() {
					return
				}
				if fc.Response().IsBodyStream() {
					// Body would read the stream to its end inside the
					// handler, so the client would get nothing until it ends.
					base.passStream()
					return
				}
				replayFiberBody(fc, w, base)
			})
			middleware(inner).ServeHTTP(base, req)
			if !base.wroteHeader && !base.late {
				// Committing here would send an empty 200 that nothing wrote,
				// and an error from below or above would never be rendered.
				base.applyHeader()
			}
			return nextErr
		}
	}
}

// replayFiberBody moves the buffered downstream body out of the fiber context
// and replays it with its status through w (the middleware's view of the
// response), so wrapping middleware observes and may transform it. The
// headers are already in base's header map (see fiberResponseWriter.reseed);
// a wrapping writer that keeps a header map of its own gets them moved into
// that map, which is where the chain below would have written them on
// net/http.
func replayFiberBody(fc fiber.Ctx, w http.ResponseWriter, base *fiberResponseWriter) {
	if header := w.Header(); !sameHeaderMap(header, base.Header()) {
		maps.Copy(header, takeResponseHeader(fc))
		base.reseed()
	}
	resp := fc.Response()
	status := resp.StatusCode()
	body := bytes.Clone(resp.Body())
	resp.ResetBody()
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

// sameHeaderMap reports whether a and b are the same map, not merely equal.
func sameHeaderMap(a, b http.Header) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

// takeResponseHeader moves the response headers out of fiber's response into
// an http.Header, leaving the status in place. A Content-Type nothing set is
// not taken (see explicitContentType). The headers are deleted one by one
// rather than with Reset, which would also clear the flags the server set on
// the response for this request, such as its no-default-Content-Type one.
func takeResponseHeader(native fiber.Ctx) http.Header {
	resp := native.Response()
	taken := make(http.Header)
	explicitCT := len(explicitContentType(native)) > 0
	for k, v := range resp.Header.All() {
		key := string(k)
		if key == fiber.HeaderContentType && !explicitCT {
			continue
		}
		taken[key] = append(taken[key], string(v))
	}
	resp.Header.Del(fiber.HeaderContentType)
	for key := range taken {
		resp.Header.Del(key)
	}
	return taken
}

// fiberResponseWriter writes into fiber's buffered response. Its header map is
// the response's headers, as net/http's is: it starts as what the response
// already carries, and the changes made to it — values set, keys deleted —
// reach fiber's response before the chain below runs and when the response
// is written.
type fiberResponseWriter struct {
	fc     fiber.Ctx
	header http.Header
	// synced is the header map as last exchanged with fiber's response; a key
	// it holds that header no longer does was deleted.
	synced      http.Header
	wroteHeader bool
	late        bool
	// streaming is set once the downstream response is a body stream: any
	// write would replace that stream, so later writes are discarded.
	streaming bool
}

// passStream commits w over a streamed downstream response without touching
// its body. A length or encoding the middleware staged is dropped: the stream
// bypasses the middleware's writer, so neither would describe the body.
func (w *fiberResponseWriter) passStream() {
	w.wroteHeader = true
	w.streaming = true
	markResponseCommitted(w.fc)
}

func (w *fiberResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
		w.reseed()
	}
	return w.header
}

// reseed refills the header map, in place so a reference a middleware kept
// stays live, with the response's current headers. A Content-Type nothing set
// is left out (see explicitContentType).
func (w *fiberResponseWriter) reseed() {
	if w.header == nil {
		return
	}
	clear(w.header)
	explicitCT := len(explicitContentType(w.fc)) > 0
	for k, v := range w.fc.Response().Header.All() {
		key := textproto.CanonicalMIMEHeaderKey(string(k))
		if key == fiber.HeaderContentType && !explicitCT {
			continue
		}
		w.header[key] = append(w.header[key], string(v))
	}
	w.synced = w.header.Clone()
}

// isBodyHeader reports the headers that describe the body the middleware
// writes, which are only applied when it writes one.
func isBodyHeader(key string) bool {
	return strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding")
}

// sync applies the header map's changes since the last exchange to fiber's
// response: a deleted key is removed, a changed one replaced, an unchanged one
// left alone. With withBody false, Content-Length and Content-Encoding stay
// staged.
func (w *fiberResponseWriter) sync(withBody bool) {
	if w.header == nil || w.late {
		return
	}
	resp := w.fc.Response()
	for key := range w.synced {
		if _, ok := w.header[key]; !ok {
			resp.Header.Del(key)
		}
	}
	for key, values := range w.header {
		if slices.Equal(w.synced[key], values) {
			continue
		}
		if isBodyHeader(key) {
			if !withBody {
				continue
			}
			if strings.EqualFold(key, "Content-Length") {
				if len(values) > 0 {
					if n, err := strconv.Atoi(values[0]); err == nil {
						resp.Header.SetContentLength(n)
					}
				}
				continue
			}
		}
		resp.Header.Del(key)
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
	w.synced = w.header.Clone()
	if !withBody {
		for key := range w.synced {
			if isBodyHeader(key) {
				delete(w.synced, key)
			}
		}
	}
}

func (w *fiberResponseWriter) WriteHeader(code int) {
	if w.wroteHeader || w.late {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
	w.wroteHeader = true
	markResponseCommitted(w.fc)
	w.applyHeader()
	w.fc.Response().SetStatusCode(code)
}

// applyHeader applies the header map's changes to fiber's response without
// committing it.
func (w *fiberResponseWriter) applyHeader() { w.sync(true) }

func (w *fiberResponseWriter) Write(p []byte) (int, error) {
	if w.streaming {
		return len(p), nil
	}
	if !w.wroteHeader && !w.late {
		status := w.fc.Response().StatusCode()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}
	if bodyless(w.fc.Response().StatusCode()) {
		return len(p), nil
	}
	w.fc.Response().AppendBody(p)
	return len(p), nil
}
