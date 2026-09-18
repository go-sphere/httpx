package httpx

// EXPERIMENTAL. Interceptor is an additive alternative to Middleware, not a
// replacement, and its interleaving rules with Use are not final. See
// benchmarks/MIDDLEWARE_FUSION_REPORT.md for what it buys and what it costs.
//
// A Middleware continues the chain by calling ctx.Next(), so the adapter has
// to sit between every two layers: one call to reach Next, one to reach the
// middleware, plus a per-request object holding the layer index. An
// Interceptor receives the rest of the chain as a Handler and calls it
// directly, so the whole chain is composed once at registration into a tree of
// closures with no per-request state: one call per layer and nothing to
// allocate.
//
// The two forms behave the same in the ways that matter to a middleware
// author, with two differences to know about:
//
//   - An Interceptor that returns without calling next stops the chain. There
//     is no Abort to reason about, no layer index and no second-Next hazard.
//   - An error from an inner layer is rendered where the chain was composed —
//     at the route — not at the layer that produced it. A layer that logs or
//     measures the outcome should use the error it gets back from next, not
//     only Context.StatusCode.
//   - Interceptors cover every registered route, including Static, StaticFS
//     and HandleStd mounts, but not unmatched paths: a 404 never reaches a
//     route, so nothing composed into one runs.

// Interceptor takes the rest of the chain and returns the handler that runs in
// front of it.
//
//	func RequestID(next httpx.Handler) httpx.Handler {
//		return func(ctx httpx.Context) error {
//			ctx.SetContext(withRequestID(ctx.Context()))
//			return next(ctx)
//		}
//	}
type Interceptor func(next Handler) Handler

// InterceptorScope is an optional router capability: the scope can run
// composed middleware as a single native handler instead of adapting every
// layer separately.
type InterceptorScope interface {
	UseInterceptor(m ...Interceptor)
}

// UseInterceptor registers interceptors on r and reports whether r ran them in
// composed form. When the scope does not implement InterceptorScope, each one
// is adapted to the Next-driven form, which behaves the same and costs what Use
// costs.
func UseInterceptor(r MiddlewareScope, m ...Interceptor) bool {
	if len(m) == 0 {
		return false
	}
	if scope, ok := r.(InterceptorScope); ok {
		scope.UseInterceptor(m...)
		return true
	}
	adapted := make([]Middleware, 0, len(m))
	for _, w := range m {
		adapted = append(adapted, AsMiddleware(w))
	}
	r.Use(adapted...)
	return false
}

// ComposeInterceptors builds the handler that runs ms in front of h. ms[0] is the
// outermost layer, so the order matches registration order.
func ComposeInterceptors(h Handler, ms []Interceptor) Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i] == nil {
			continue
		}
		if next := ms[i](h); next != nil {
			h = next
		}
	}
	return h
}

// AsMiddleware adapts an Interceptor to the Next-driven form. The
// composition happens once, here, so the returned Middleware allocates
// nothing per request.
func AsMiddleware(m Interceptor) Middleware {
	driveNext := func(ctx Context) error { return ctx.Next() }
	if m == nil {
		return driveNext
	}
	h := m(driveNext)
	if h == nil {
		return driveNext
	}
	return Middleware(h)
}

// ctxBase names the embedded interface something other than Context, whose
// promoted Context() method would otherwise collide with the field name.
type ctxBase = Context

// nextContext gives a Next-driven middleware something for ctx.Next() to
// mean while it runs inside a composed chain.
type nextContext struct {
	ctxBase
	next   Handler
	called bool
}

func (c *nextContext) Next() error {
	return c.NextWithContext(c.ctxBase)
}

// NextWithContext lets an adapter isolate the continuation of asynchronous
// middleware while preserving the composed chain.
func (c *nextContext) NextWithContext(ctx Context) error {
	if c.called {
		// Matches the adapters: driving a finished chain again does nothing.
		return nil
	}
	c.called = true
	// The rest of the chain gets the adapter's own context, not this shim:
	// handing it the shim would make a downstream ctx.Next() re-enter Next
	// here and stop the chain instead of continuing it.
	return c.next(ctx)
}

// AsInterceptor adapts a Next-driven Middleware to an Interceptor, for mixing
// middleware that has not been converted. Unlike AsMiddleware, this allocates
// per-request continuation state and a wrapper for any optional capabilities
// exposed by the original context.
func AsInterceptor(m Middleware) Interceptor {
	return func(next Handler) Handler {
		if m == nil {
			return next
		}
		return func(ctx Context) error {
			c := &nextContext{ctxBase: ctx, next: next}
			return m(preserveCapabilities(c, ctx))
		}
	}
}

// Preserve precisely the optional capabilities of the wrapped context. In
// particular, a buffered adapter must not acquire Flusher merely by wrapping.
func preserveCapabilities(c *nextContext, ctx Context) Context {
	n, hasNative := ctx.(NativeContextProvider)
	f, hasFlush := ctx.(Flusher)
	s, hasStream := ctx.(Streamer)
	switch {
	case hasNative && hasFlush && hasStream:
		return &struct {
			*nextContext
			NativeContextProvider
			Flusher
			Streamer
		}{c, n, f, s}
	case hasNative && hasFlush:
		return &struct {
			*nextContext
			NativeContextProvider
			Flusher
		}{c, n, f}
	case hasNative && hasStream:
		return &struct {
			*nextContext
			NativeContextProvider
			Streamer
		}{c, n, s}
	case hasFlush && hasStream:
		return &struct {
			*nextContext
			Flusher
			Streamer
		}{c, f, s}
	case hasNative:
		return &struct {
			*nextContext
			NativeContextProvider
		}{c, n}
	case hasFlush:
		return &struct {
			*nextContext
			Flusher
		}{c, f}
	case hasStream:
		return &struct {
			*nextContext
			Streamer
		}{c, s}
	default:
		return c
	}
}
