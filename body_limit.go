package httpx

import "net/http"

// LimitRequestBody applies a request body limit of n bytes to r, which is how
// the net/http based adapters implement their WithMaxBodySize option before a
// route's middleware runs. n <= 0 or a nil request is a no-op and leaves r
// untouched.
//
// A request declaring Content-Length > n is refused: LimitRequestBody returns
// a *http.MaxBytesError without reading anything and leaves r.Body as
// received, so the caller must answer the error and not read the body. On
// HTTP/1 it also sets Connection: close on w, so net/http sends the answer
// before it looks at the unread body (a client that stalls mid-body cannot
// hold the answer back) and then closes the connection. When more than
// net/http is willing to discard (256 KiB) is unread, it half-closes the
// connection and waits before closing it, so a client still streaming the body
// reads the 413 instead of failing with a connection reset.
//
// Any other body is wrapped with http.MaxBytesReader(w, r.Body, n), so a read
// that passes n bytes (a body of unknown length) fails with a
// *http.MaxBytesError. Either way the error classifies as 413 (see
// WrapBindError and ParseError).
func LimitRequestBody(w http.ResponseWriter, r *http.Request, n int64) error {
	if n <= 0 || r == nil {
		return nil
	}
	if r.ContentLength > n {
		r.Close = true
		if w != nil && r.ProtoMajor < 2 {
			w.Header().Set("Connection", "close")
		}
		// Not wrapped: net/http chooses between closing at once and lingering by
		// inspecting the request's own body type, which a wrapper hides.
		return &http.MaxBytesError{Limit: n}
	}
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, n)
	}
	return nil
}
