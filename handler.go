package httpx

// ErrorHandler renders err into the response using the framework-neutral
// Context. Every adapter accepts it under the same name, WithErrorHandler, so
// applications can share one error-rendering implementation across all
// supported frameworks. Adapters whose framework has an error handler of its
// own also offer WithNativeErrorHandler for that shape.
type ErrorHandler func(Context, error)

// DefaultErrorHandler writes the standard {success, code, message} error body
// produced by RenderError. It is the fallback used by adapters when no custom
// handler is configured.
func DefaultErrorHandler(ctx Context, err error) {
	status, body := RenderError(err)
	_ = ctx.JSON(status, body)
}
