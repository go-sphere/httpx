// Package httpxtest is the shared conformance suite for httpx adapters.
//
// An adapter provides a Suite — a name, a capability declaration, and a
// function that builds an Engine — and calls Run from its own module's tests:
//
//	func TestConformance(t *testing.T) {
//		httpxtest.Run(t, httpxtest.Suite{
//			Name: "ginx",
//			Caps: httpxtest.Caps{NamedWildcard: true, Flusher: true},
//			NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
//				...
//			},
//		})
//	}
//
// Response shape is checked against the golden contracts in this package's
// golden directory, which every adapter compares against, so the suite states
// what a response must look like instead of asserting that the adapters agree
// with each other. Set HTTPX_UPDATE_GOLDEN=1 to rewrite them from a source
// checkout; after rewriting, run the suite for every adapter without the
// variable to confirm they all still match the recorded contract.
//
// The suite is deliberately usable from outside this repository: a
// third-party adapter can import it and certify itself against the same
// contract as the official four.
package httpxtest

import (
	"io"
	"net/http"
	"testing"

	"github.com/go-sphere/httpx"
)

// Options describe the engine one case needs.
type Options struct {
	// ErrorHandler, when non-nil, must be installed as the engine's
	// framework-neutral error handler (the adapter's WithHTTPXErrorHandler /
	// WithErrorHandler option).
	ErrorHandler httpx.ErrorHandler
}

// Caps declares the optional behavior an adapter supports. Cases that depend
// on a capability skip with a reason when it is absent, so a missing feature
// stays visible in test output instead of being silently untested.
type Caps struct {
	// NamedWildcard reports httpx.RouterFeatureNamedWildcard.
	NamedWildcard bool
	// Flusher reports that Context implements httpx.Flusher.
	Flusher bool
	// RendersErrorAtFailingLayer reports that a middleware error is written to
	// the response where it happens, rather than being handed back to the
	// framework's own error handler after the chain unwinds.
	RendersErrorAtFailingLayer bool
	// ComposesInterceptors reports that the router implements
	// httpx.InterceptorScope natively instead of falling back to
	// httpx.AsMiddleware.
	ComposesInterceptors bool
	// InProcessUnknownLengthBody reports that the engine's httpx.TestRequester
	// can deliver a request whose body length is not known in advance.
	//
	// This describes the in-process requester, not the serving path: fiber's
	// app.Test serializes a negative ContentLength as a literal
	// "Content-Length: -1" header, which fasthttp then rejects, while fiber
	// itself serves chunked requests over a socket normally. An adapter that
	// leaves this false is stating that the property cannot be *verified*
	// in-process, not that it is unsupported.
	InProcessUnknownLengthBody bool
}

// Suite is what an adapter provides to run the shared cases.
type Suite struct {
	// Name identifies the adapter in failure messages.
	Name string
	// Caps declares optional behavior; see Caps.
	Caps Caps
	// NewEngine builds a fresh engine for one case. The engine must support
	// httpx.TestRequester so the suite can serve requests in-process. Register
	// nothing on it: each case registers what it needs.
	NewEngine func(tb testing.TB, opts Options) httpx.Engine

	// Dispatch builds an engine with register applied and returns a function
	// that serves req through the framework's **own** dispatcher, reusing
	// whatever buffers that framework needs. RunBenchmarks calls the returned
	// function once per iteration.
	//
	// It exists because the portable path (httpx.TestRequester) allocates an
	// *http.Response and reads its body, which costs 20+ allocations and a few
	// microseconds — on an empty request that is 98% of the measurement.
	// Without this hook the benchmarks still run, they just measure the
	// harness. About twenty lines per adapter; see the suites in
	// conformance/httpxtest_suite_test.go.
	Dispatch func(tb testing.TB, register func(httpx.Router), req *http.Request) func()

	// StdMiddleware wraps a plain net/http middleware as an httpx.Middleware,
	// which is the adapter's AdaptStdMiddleware. It is a hook rather than part
	// of the Router interface, so the suite has to be handed it; the cases that
	// mount net/http middleware skip when it is nil.
	StdMiddleware func(func(http.Handler) http.Handler) httpx.Middleware

	// NativeMiddleware wraps the adapter's own framework middleware as an
	// httpx.Middleware: it must record mark("native-pre"), continue the chain
	// the *native* way (gin's c.Next, echo's next(c), fiber's c.Next, hertz's
	// rc.Next) and then record mark("native-post"). The cases that mix native
	// and httpx middleware skip when it is nil, which is the right answer for
	// an adapter that has no native bridge.
	NativeMiddleware func(mark func(string)) httpx.Middleware
}

// Run executes every shared case against s.
func Run(t *testing.T, s Suite) {
	t.Helper()
	if s.Name == "" {
		t.Fatal("httpxtest: Suite.Name is required")
	}
	if s.NewEngine == nil {
		t.Fatal("httpxtest: Suite.NewEngine is required")
	}
	root := t.Name()
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			g.run(t, runner{suite: s, root: root})
		})
	}
}

// caseGroup is one named collection of cases, registered by the cases_*.go
// files in this package.
type caseGroup struct {
	name string
	run  func(t *testing.T, r runner)
}

var groups []caseGroup

func register(name string, run func(t *testing.T, r runner)) {
	groups = append(groups, caseGroup{name: name, run: run})
}

// runner gives the cases access to the adapter under test.
type runner struct {
	suite Suite
	root  string
}

// response is the part of an HTTP response the cases inspect.
type response struct {
	Status  int
	Body    string
	Headers http.Header
}

// serve builds an engine, lets register add routes, and dispatches req
// in-process.
func (r runner) serve(t *testing.T, register func(httpx.Router), req *http.Request) response {
	t.Helper()
	return r.serveWith(t, Options{}, register, req)
}

func (r runner) serveWith(t *testing.T, opts Options, register func(httpx.Router), req *http.Request) response {
	t.Helper()
	engine := r.suite.NewEngine(t, opts)
	if engine == nil {
		t.Fatalf("%s: NewEngine returned nil", r.suite.Name)
	}
	register(engine.Group(""))

	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatalf("%s: engine does not support httpx.TestRequester", r.suite.Name)
	}
	resp, err := requester.Do(req)
	if err != nil {
		t.Fatalf("%s: serve %s %s: %v", r.suite.Name, req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s: read response body: %v", r.suite.Name, err)
	}
	headers := make(http.Header, len(resp.Header))
	for key, values := range resp.Header {
		headers[key] = append([]string(nil), values...)
	}
	return response{Status: resp.StatusCode, Body: string(body), Headers: headers}
}

// assertGolden serves the request and compares the response contract with the
// golden file recorded for this case.
func (r runner) assertGolden(t *testing.T, register func(httpx.Router), req *http.Request) {
	t.Helper()
	r.assertGoldenWith(t, Options{}, register, req)
}

func (r runner) assertGoldenWith(t *testing.T, opts Options, register func(httpx.Router), req *http.Request) {
	t.Helper()
	got := r.serveWith(t, opts, register, req)
	r.compareGolden(t, got)
}
