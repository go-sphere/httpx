package httpxmock

import (
	"sync"

	"github.com/go-sphere/httpx"
)

// Handler is a recording terminal httpx.Handler: the bottom of a chain, which
// a middleware test needs in order to answer the one question every such test
// asks first — did the chain get past the layer, or did the layer stop it?
//
// The zero value is ready to use and succeeds without doing anything:
//
//	next := &httpxmock.Handler{}
//	err := httpxmock.Run(ctx, next.Handle, mw)
//	if next.Called() {
//		t.Error("the layer should have rejected this request")
//	}
//
// Set Fn for a terminus that does something — writes a response, fails, reads
// what the layer above stored on the Context. Handler is safe for concurrent
// use, for the same reason the mock Context is.
type Handler struct {
	// Fn runs on every call and its error is what Handle returns. A nil Fn
	// means a handler that records the call and succeeds, which is what a
	// test asserting only on whether the chain continued wants.
	//
	// It is a plain field rather than a constructor argument because the
	// common case is not setting it at all, and a constructor taking one
	// argument that is nil at almost every call site is worse than a struct
	// literal that omits it.
	Fn httpx.Handler

	mu    sync.Mutex
	calls int
}

// Handle implements httpx.Handler. Pass the method value where a chain wants
// its terminus: mw(next.Handle)(ctx), or Run(ctx, next.Handle, mw).
func (h *Handler) Handle(ctx httpx.Context) error {
	h.mu.Lock()
	h.calls++
	fn := h.Fn
	h.mu.Unlock()

	// Fn runs outside the lock: it is caller code, it will touch the Context,
	// and it may well call back into this Handler.
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// Called reports whether the chain reached the handler at least once.
func (h *Handler) Called() bool {
	return h.Calls() > 0
}

// Calls reports how many times the chain reached the handler.
func (h *Handler) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// Run composes mw around next and drives it with ctx, returning the error the
// outermost layer returned.
//
// A nil next is a terminus that records nothing and succeeds, for a test
// interested only in the error or in what the layer wrote to the response. Nil
// layers are skipped, matching httpx.ComposeMiddleware, so a table that leaves
// one out does not have to special-case it.
//
// This exists because the three lines it replaces had been written four times
// in sphere — twice as a concrete func over that package's fake context, and
// twice as a generic one over a nextRecorder interface whose only purpose was
// to let a plain-bool and an atomic-bool variant of the same fake share the
// driver. A Context and a Handler that are both race-safe delete the interface,
// the type parameter and the duplicate fakes together.
//
// Layers run outermost-first, the order httpx.ComposeMiddleware gives them:
//
//	Run(ctx, next.Handle, logging, auth) // logging wraps auth wraps next
func Run(ctx httpx.Context, next httpx.Handler, mw ...httpx.Middleware) error {
	if next == nil {
		next = func(httpx.Context) error { return nil }
	}
	// The nil layers are dropped here rather than left to
	// httpx.ComposeMiddleware, which skips them too: a variadic argument is
	// the one place a nil layer arrives from outside the library, and
	// filtering at the boundary is what lets static analysis see that the
	// slice handed on holds no nils.
	layers := make([]httpx.Middleware, 0, len(mw))
	for _, m := range mw {
		if m != nil {
			layers = append(layers, m)
		}
	}
	return httpx.ComposeMiddleware(next, layers)(ctx)
}
