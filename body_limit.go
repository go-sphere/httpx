package httpx

import "net/http"

// LimitRequestBody applies a request body limit of n bytes to r, which is how
// the net/http based adapters implement their WithMaxBodySize option before a
// route's middleware runs. n <= 0 means no limit and leaves r untouched.
//
// A declared Content-Length above n returns a *http.MaxBytesError at once, so
// the request can be refused without reading anything. Otherwise r.Body is
// replaced by http.MaxBytesReader(w, r.Body, n): a body of unknown length
// fails the read that passes n bytes with a *http.MaxBytesError. Either way
// the error classifies as 413 (see WrapBindError and ParseError).
func LimitRequestBody(w http.ResponseWriter, r *http.Request, n int64) error {
	if n <= 0 {
		return nil
	}
	if r.ContentLength > n {
		return &http.MaxBytesError{Limit: n}
	}
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, r.Body, n)
	}
	return nil
}
