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
// Response shape is checked against the golden contracts embedded in this
// package, so the suite states what a response must look like instead of
// asserting that the adapters agree with each other. Set HTTPX_UPDATE_GOLDEN=1
// to rewrite them from a source checkout, then run the suite for every adapter
// to confirm they all still match. The package is deliberately importable from
// outside this repository, so a third-party adapter can certify itself against
// the same contract as the official four.
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
	// framework-neutral error handler (the adapter's WithErrorHandler option).
	ErrorHandler httpx.ErrorHandler
}

// Caps declares the optional behavior an adapter supports. Cases depending on
// a capability skip with a reason when it is absent, so a missing feature stays
// visible in test output instead of being silently untested.
type Caps struct {
	// NamedWildcard reports httpx.RouterFeatureNamedWildcard.
	NamedWildcard bool
	// Flusher reports that Context implements httpx.Flusher.
	Flusher bool
	// InProcessUnknownLengthBody reports that the engine's httpx.TestRequester
	// can deliver a request whose body length is not known in advance. This
	// describes the in-process requester, not the serving path: fiber's app.Test
	// serializes a negative ContentLength as a literal "Content-Length: -1"
	// header, which fasthttp rejects, while fiber serves chunked requests over a
	// socket normally. False means the property cannot be *verified* in-process,
	// not that it is unsupported.
	InProcessUnknownLengthBody bool
	// ForcedStopCutsConnections reports that when Engine.Stop's context
	// expires, the adapter also cuts the connections still being served,
	// instead of only closing the listener and leaving them to finish. Closing
	// the listener is the contract and every adapter does it; the frameworks
	// differ on the rest. net/http has Server.Close, so ginx, echox and stdx cut
	// in-flight connections; fasthttp has no equivalent (and fiber hands out no
	// listener to close behind its back), and hertzx's Engine.Close is Shutdown
	// with an already-expired context, which never touches an active connection.
	// Unlike the rest of Caps this is **not** checked by the Caps group — it
	// needs a real connection to hold open — so the conformance module verifies
	// it against actual behavior instead.
	ForcedStopCutsConnections bool
}

// Suite is what an adapter provides to run the shared cases.
type Suite struct {
	// Name identifies the adapter in failure messages.
	Name string
	// Caps declares optional behavior; see Caps.
	Caps Caps
	// NewEngine builds a fresh engine for one case. The engine must support
	// httpx.TestRequester so the suite can serve requests in-process; register
	// nothing on it, each case registers what it needs.
	NewEngine func(tb testing.TB, opts Options) httpx.Engine

	// Dispatch builds an engine with register applied and returns a function
	// that serves req through the framework's **own** dispatcher, reusing
	// whatever buffers that framework needs. It exists because the portable
	// path (httpx.TestRequester) allocates an *http.Response and reads its
	// body, which on an empty request dominates the measurement; without this
	// hook the benchmarks measure the harness. See the suites in
	// conformance/httpxtest_suite_test.go.
	Dispatch func(tb testing.TB, register func(httpx.Router), req *http.Request) func()

	// StdMiddleware wraps a plain net/http middleware as an httpx.Middleware,
	// which is the adapter's AdaptStdMiddleware. It is a hook rather than part
	// of the Router interface, so the cases that mount net/http middleware skip
	// when it is nil.
	StdMiddleware func(func(http.Handler) http.Handler) httpx.Middleware

	// NativeMiddleware registers the framework's own middleware on scope — an
	// httpx.Router or httpx.Engine — through the adapter's UseNative. The layer
	// must record mark("native-pre"), continue the chain the *native* way (gin's
	// c.Next, echo's next(c), fiber's c.Next, hertz's rc.Next) and then record
	// mark("native-post"). It is a hook because UseNative takes the framework's
	// own middleware type, which the shared interface cannot name and no
	// httpx.Middleware can stand in for, since every httpx layer shares one
	// native handler slot. stdx leaves it nil and the cases mixing native and
	// httpx layers skip there.
	NativeMiddleware func(scope any, mark func(string))
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

func (r runner) serve(t *testing.T, register func(httpx.Router), req *http.Request) response {
	t.Helper()
	return r.serveWith(t, Options{}, register, req)
}

func (r runner) serveWith(t *testing.T, opts Options, register func(httpx.Router), req *http.Request) response {
	t.Helper()
	return r.serveEngine(t, opts, func(engine httpx.Engine) {
		register(engine.Group(""))
	}, req)
}

// serveEngine is serveWith one level out: register is handed the Engine itself,
// for cases whose subject is Engine.Group or an engine-scope registration
// rather than anything a Router reaches.
func (r runner) serveEngine(t *testing.T, opts Options, register func(httpx.Engine), req *http.Request) response {
	t.Helper()
	engine := r.suite.NewEngine(t, opts)
	if engine == nil {
		t.Fatalf("%s: NewEngine returned nil", r.suite.Name)
	}
	register(engine)
	return r.serveOn(t, engine, req)
}

// serveOn dispatches one request against an engine that is already built and
// registered, for the cases whose subject is what a second request sees.
func (r runner) serveOn(t *testing.T, engine httpx.Engine, req *http.Request) response {
	t.Helper()
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

func (r runner) assertGolden(t *testing.T, register func(httpx.Router), req *http.Request) {
	t.Helper()
	r.assertGoldenWith(t, Options{}, register, req)
}

func (r runner) assertGoldenWith(t *testing.T, opts Options, register func(httpx.Router), req *http.Request) {
	t.Helper()
	got := r.serveWith(t, opts, register, req)
	r.compareGolden(t, got)
}
