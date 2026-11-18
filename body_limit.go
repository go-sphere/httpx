package httpx

import "net/http"

// LimitRequestBody applies a request body limit of n bytes to r, which is how
// the net/http based adapters implement their WithMaxBodySize option before a
// route's middleware runs. n <= 0 or a nil request is a no-op and leaves r
// untouched.
//
// When r.Body is non-nil (and not http.NoBody), it is wrapped with
// http.MaxBytesReader(w, r.Body, n) so a body whose read passes n bytes fails
// with a *http.MaxBytesError. A request declaring Content-Length > n also
// returns a *http.MaxBytesError immediately so the handler can refuse it
// without reading, and marks HTTP/1 connections for closure. Either way the
// error classifies as 413 (see WrapBindError and ParseError).
func LimitRequestBody(w http.ResponseWriter, r *http.Request, n int64) error {
	if n <= 0 || r == nil {
		return nil
	}
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, n)
	}
	if r.ContentLength > n {
		r.Close = true
		if w != nil && r.ProtoMajor < 2 {
			w.Header().Set("Connection", "close")
		}
		return &http.MaxBytesError{Limit: n}
	}
	return nil
}
