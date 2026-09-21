package httpxmock

import "errors"

// ErrBindUnsupported is returned by every Bind method on a mock Context.
//
// Binding is the one part of the Context surface this package will not fake,
// because there is no single correct answer to fake. Each adapter decodes
// through its framework's binder — gin's binding package, echo's, fiber's,
// hertz's, and go-playground/form in stdx — and they disagree on real
// details: trailing data after the first JSON value, repeated query keys,
// what an absent field leaves behind, whether a type mismatch is an error.
// A sixth decoder written here would match none of them, and a test that
// passed against it would be testing this package rather than the adapter
// the caller ships on.
//
// The root module also cannot borrow the closest match. stdx decodes query,
// form, URI and header parameters with github.com/go-playground/form, and
// github.com/go-sphere/httpx has no third-party dependencies and keeps it
// that way; pulling that decoder up here to serve a test helper would put it
// in the dependency graph of every consumer of every adapter.
//
// BindJSON returns this too, though encoding/json alone would cover it. The
// alternative — one binder that works and four that do not — is worse than
// five that behave alike: a test suite that binds JSON here and query
// parameters against a real engine has two different notions of what binding
// means, and the first surprise is somebody adding a `query` tag to a DTO
// whose test silently stops covering it.
//
// A test that needs real binding should drive a real engine. stdx is the
// cheapest: it needs no framework, its Engine is an http.Handler, and
// httpx.AsTestRequester serves a request in process.
//
// It is deliberately not wrapped by httpx.WrapBindError and so classifies as
// 500, not 400. A handler under test that renders it would otherwise report a
// plausible 400 and hide the fact that the test was never decoding anything.
var ErrBindUnsupported = errors.New("httpxmock: Bind is not implemented; drive a real adapter (stdx) for binding coverage")

// BindJSON reports ErrBindUnsupported. See ErrBindUnsupported.
func (c *Context) BindJSON(dst any) error { return c.bindUnsupported("BindJSON") }

// BindQuery reports ErrBindUnsupported. See ErrBindUnsupported.
func (c *Context) BindQuery(dst any) error { return c.bindUnsupported("BindQuery") }

// BindForm reports ErrBindUnsupported. See ErrBindUnsupported.
func (c *Context) BindForm(dst any) error { return c.bindUnsupported("BindForm") }

// BindURI reports ErrBindUnsupported. See ErrBindUnsupported.
func (c *Context) BindURI(dst any) error { return c.bindUnsupported("BindURI") }

// BindHeader reports ErrBindUnsupported. See ErrBindUnsupported.
func (c *Context) BindHeader(dst any) error { return c.bindUnsupported("BindHeader") }

func (c *Context) bindUnsupported(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note(name)
	return ErrBindUnsupported
}
