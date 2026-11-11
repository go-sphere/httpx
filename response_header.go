package httpx

// ResponseHeaderEditor is an optional Context capability for the response
// headers Responder.SetHeader cannot express: appending to a multi-valued
// header such as Vary or Link, and reading what downstream code has set.
// Every official adapter and httpxmock implement it; probe with
// AsResponseHeaderEditor.
//
// It follows the Responder post-commit rules: AddHeader on a committed
// response is dropped and reports nothing.
type ResponseHeaderEditor interface {
	// AddHeader appends value to the response header key, keeping the values
	// already set. Set-Cookie belongs to Responder.SetCookie, and a header the
	// protocol allows only once (Content-Type, Content-Length) may be replaced
	// rather than repeated on the fasthttp-based adapters.
	AddHeader(key, value string)

	// ResponseHeaderValues returns the values the response carries for header
	// key, in the order they were added, or nil when there are none. The key
	// is matched case-insensitively and the slice is the caller's. A header
	// nothing set is not reported, whatever default the framework keeps, and
	// neither is one dropped because the response was already committed.
	ResponseHeaderValues(key string) []string
}

// AsResponseHeaderEditor returns the response-header editing capability when
// supported.
func AsResponseHeaderEditor(ctx Context) (ResponseHeaderEditor, bool) {
	e, ok := ctx.(ResponseHeaderEditor)
	return e, ok
}
