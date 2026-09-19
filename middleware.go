package httpx

import "sync/atomic"

// Middleware is a layer around the rest of the chain: it receives the handler
// that follows it and returns the handler that runs in its place.
//
//	func RequestID(next httpx.Handler) httpx.Handler {
//		return func(ctx httpx.Context) error {
//			ctx.SetContext(withRequestID(ctx.Context()))
//			return next(ctx)
//		}
//	}
//
// What follows from the shape:
//
//   - Returning without calling next stops the chain. There is no Abort, no
//     layer index and no second-Next hazard.
//   - An error from an inner layer is rendered where the chain was composed —
//     at the route — not at the layer that produced it. A layer that logs or
//     measures the outcome should use the error it gets back from next, not
//     only Context.StatusCode.
//   - Middleware always runs inside anything registered with the adapter's
//     UseNative on the same scope, whatever order the registration calls were
//     made in.
//
// A chain covers every registered route, including Static, StaticFS and
// HandleStd mounts, which go through Handle like any other route. An
// engine-scope chain also covers the paths no route matched: the adapter's
// 404/405 fallback composes the engine's own layers in front of the handler
// that renders the error, so an access log, a panic recovery layer or a CORS
// layer sees a request for /nope and may answer it. A group's layers do not and
// must not — a 404 belongs to no group, and there is no scope to pick.
type Middleware func(next Handler) Handler

// MiddlewareScope attaches middleware to the current scope. Both Router and
// Engine include it, so a caller holding either interface calls Use directly;
// it is still a named interface of its own for code that accepts any scope.
type MiddlewareScope interface {
	Use(m ...Middleware)
}

// MiddlewareChain is one scope's registered middleware plus a reference to the
// scope it was created from. Adapters keep one per Engine and one per Router.
//
// Rule 1 — a route's chain is resolved when the route is registered, not when
// its scope was created. A group keeps a reference to its parent's chain rather
// than a copy, so a late Use on the parent still reaches the routes the group
// registers afterwards:
//
//	e.Use(A)
//	g := e.Group("/api")
//	e.Use(B)                // still reaches the routes g registers below
//	g.GET("/x", h)          // chain: A, B
//	e.Use(C)
//	g.GET("/y", h)          // chain: A, B, C; /x keeps A, B
//
// Routes already registered are deliberately left on the chain they were
// registered with. That is the rule gin applies to its own Use, and it is what
// makes a registered route immutable.
//
// Rule 2 — Compose walks to the root, so an outer scope's layers end up outside
// an inner scope's whatever order the calls came in, while ComposeOwn stops at
// this scope. An unmatched-path fallback must use ComposeOwn on the engine's
// chain: a 404 belongs to no group.
//
// Concurrent Use is not supported — registration happens before serving — but a
// Use concurrent with a request is race-free rather than merely unlikely: the
// layer list is replaced, never mutated in place.
type MiddlewareChain struct {
	parent *MiddlewareChain
	// own holds an immutable snapshot, replaced on every Use. Readers load the
	// pointer, so a request that overlaps a late registration sees either the
	// old list or the new one and never a torn slice.
	own atomic.Pointer[[]Middleware]
}

// NewMiddlewareChain returns the root chain, which is what an Engine holds.
func NewMiddlewareChain() *MiddlewareChain {
	return &MiddlewareChain{}
}

// Sub returns the chain for a scope created from this one.
func (c *MiddlewareChain) Sub() *MiddlewareChain {
	return &MiddlewareChain{parent: c}
}

// Use appends middleware to this scope, in registration order.
func (c *MiddlewareChain) Use(m ...Middleware) {
	if len(m) == 0 {
		return
	}
	cur := c.Own()
	next := make([]Middleware, 0, len(cur)+len(m))
	next = append(next, cur...)
	next = append(next, m...)
	c.own.Store(&next)
}

// noMiddleware is what a scope with no registrations reports. Non-nil and
// shared: the callers only range over it, and a nil return here reads to
// nilaway as a nil slice being indexed inside ComposeMiddleware.
var noMiddleware = []Middleware{}

// Own returns this scope's own layers, without its parents'. The slice must not
// be modified.
func (c *MiddlewareChain) Own() []Middleware {
	p := c.own.Load()
	if p == nil {
		return noMiddleware
	}
	return *p
}

// Compose wraps h in every layer from the root scope inward, which is what an
// adapter calls once per route at registration.
func (c *MiddlewareChain) Compose(h Handler) Handler {
	// Innermost scope first: each ComposeMiddleware call puts its layers
	// outside what it was handed, so walking up to the root leaves the root's
	// layers outermost.
	for s := c; s != nil; s = s.parent {
		h = ComposeMiddleware(h, s.Own())
	}
	return h
}

// ComposeOwn wraps h in this scope's own layers only.
func (c *MiddlewareChain) ComposeOwn(h Handler) Handler {
	return ComposeMiddleware(h, c.Own())
}

// MiddlewareFallback is an engine chain composed around one fixed leaf that is
// not a registered route — the handler an adapter runs for a path no route
// matched.
//
// It exists because a fallback has no registration moment to compose at. The
// adapter installs it while the Engine is being constructed, before any Use
// call, so a chain snapshotted there would always be empty; composing per
// request instead would allocate one closure per layer on the cheapest request
// there is to send. The composition is therefore built on the first unmatched
// request after each registration and reused until the next one.
//
// leaf is fixed for the life of the fallback — adapters pass a closure over the
// 404 or the 405 error, built once — because the cache is keyed on the chain's
// layer list alone.
type MiddlewareFallback struct {
	chain *MiddlewareChain
	leaf  Handler
	cur   atomic.Pointer[composedFallback]
}

// composedFallback remembers which layer list a composition was built from, so
// a later Use invalidates it by replacing that list.
type composedFallback struct {
	own *[]Middleware
	h   Handler
}

// NewMiddlewareFallback returns the fallback for chain and leaf. chain must be
// the engine's own chain: a group's middleware must not reach an unmatched
// path.
func NewMiddlewareFallback(chain *MiddlewareChain, leaf Handler) *MiddlewareFallback {
	return &MiddlewareFallback{chain: chain, leaf: leaf}
}

// Handler returns the leaf wrapped in the engine scope's middleware, composing
// it only when the chain has changed since the last call.
func (f *MiddlewareFallback) Handler() Handler {
	own := f.chain.own.Load()
	if cur := f.cur.Load(); cur != nil && cur.own == own {
		return cur.h
	}
	h := f.chain.ComposeOwn(f.leaf)
	f.cur.Store(&composedFallback{own: own, h: h})
	return h
}

// ComposeMiddleware builds the handler that runs ms in front of h. ms[0] is the
// outermost layer, so the order matches registration order.
func ComposeMiddleware(h Handler, ms []Middleware) Handler {
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
