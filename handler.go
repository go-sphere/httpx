package httpx

// ErrorHandler renders err into the response using the framework-neutral
// Context. Adapters accept it via their WithHTTPXErrorHandler / WithErrorHandler
// options so applications can share one error-rendering implementation across
// all supported frameworks.
type ErrorHandler func(Context, error)

// DefaultErrorHandler writes the standard {success, code, message} error body
// produced by RenderError. It is the fallback used by adapters when no custom
// handler is configured.
func DefaultErrorHandler(ctx Context, err error) {
	status, body := RenderError(err)
	_ = ctx.JSON(status, body)
}
