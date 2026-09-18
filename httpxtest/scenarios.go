package httpxtest

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Scenarios", casesScenarios)
}

// Scenario is one registration plus one request, used twice: the Scenarios
// case group records a golden contract for it, and RunBenchmarks measures it.
// Adding a scenario here therefore adds both a contract and a benchmark.
//
// The request is described rather than built so a benchmark can reuse one
// request object and rewind its body without allocating per iteration.
type Scenario struct {
	// Name identifies the scenario in test names, golden file names and
	// benchmark names.
	Name string
	// Register adds the routes and middleware the scenario needs.
	Register func(httpx.Router)
	// Method defaults to GET when empty.
	Method string
	// Target is the request path, including any query string.
	Target string
	// Header is applied to the request; Content-Type for a body goes here.
	Header map[string]string
	// Body is the request body, or nil.
	Body []byte
	// BenchmarkOnly marks a scenario whose response is too large to record as
	// a golden contract; it is still measured.
	BenchmarkOnly bool
}

// NewRequest builds a fresh request for the scenario.
func (s Scenario) NewRequest() *http.Request {
	method := s.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if s.Body != nil {
		body = bytes.NewReader(s.Body)
	}
	req := httptest.NewRequest(method, "http://example.com"+s.Target, body)
	for k, v := range s.Header {
		req.Header.Set(k, v)
	}
	return req
}

// RewindableRequest returns a request whose body can be rewound without
// allocating, plus the rewind function. A benchmark calls rewind before each
// iteration: reusing the request keeps request construction out of the
// measurement, and rewinding keeps the handler from reading an exhausted body
// on every iteration after the first.
func (s Scenario) RewindableRequest() (*http.Request, func()) {
	req := s.NewRequest()
	if s.Body == nil {
		return req, func() {}
	}
	reader := bytes.NewReader(s.Body)
	body := rewindBody{reader}
	req.Body = body
	return req, func() {
		_, _ = reader.Seek(0, io.SeekStart)
		// Reinstall the body: BodyRaw replaces Request.Body with a reader over
		// its own copy (that is how ginx/echox keep the body re-readable), so
		// seeking alone would leave the next iteration reading a different
		// buffer than the first one did — and a different one than a native
		// handler reads, which makes the pair incomparable.
		req.Body = body
	}
}

type rewindBody struct{ *bytes.Reader }

func (rewindBody) Close() error { return nil }

// Scenarios returns the shared scenario table: the request and response shapes
// a service actually serves, at the depths it actually registers.
func Scenarios() []Scenario {
	pass := func(ctx httpx.Context) error { return ctx.Next() }
	passInterceptor := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}
	noContent := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }
	payload1K := map[string]any{"id": 42, "name": "benchmark", "data": strings.Repeat("x", 1024)}
	payload100K := map[string]any{"id": 42, "data": strings.Repeat("x", 100*1024)}

	scenarios := []Scenario{
		{
			Name:     "Empty",
			Register: func(r httpx.Router) { r.GET("/scenario", noContent) },
			Target:   "/scenario",
		},
		{
			Name: "JSON1K",
			Register: func(r httpx.Router) {
				r.GET("/scenario", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, payload1K)
				})
			},
			Target: "/scenario",
		},
		{
			Name: "JSON100K",
			Register: func(r httpx.Router) {
				r.GET("/scenario", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, payload100K)
				})
			},
			Target: "/scenario",
		},
		{
			Name: "State",
			Register: func(r httpx.Router) {
				r.GET("/scenario", func(ctx httpx.Context) error {
					ctx.Set("key", "value")
					v, _ := ctx.Get("key")
					return ctx.JSON(http.StatusOK, map[string]any{"value": v})
				})
			},
			Target: "/scenario",
		},
		{
			Name: "BindJSON",
			Register: func(r httpx.Router) {
				type in struct {
					Name string `json:"name"`
				}
				r.POST("/scenario", func(ctx httpx.Context) error {
					var v in
					if err := ctx.BindJSON(&v); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, map[string]any{"name": v.Name})
				})
			},
			Method: http.MethodPost,
			Target: "/scenario",
			Header: map[string]string{"Content-Type": "application/json"},
			Body:   []byte(`{"name":"alice"}`),
		},
		{
			// The most expensive request path a generated handler takes: body,
			// query, path and header all decoded into separate structs.
			Name: "BindFull",
			Register: func(r httpx.Router) {
				type body struct {
					Name string `json:"name"`
					Age  int    `json:"age"`
				}
				type query struct {
					Active bool `query:"active"`
				}
				type uri struct {
					ID string `uri:"id"`
				}
				type header struct {
					Token string `header:"X-Token"`
				}
				r.POST("/scenario/:id", func(ctx httpx.Context) error {
					var b body
					var q query
					var u uri
					var h header
					if err := ctx.BindJSON(&b); err != nil {
						return err
					}
					if err := ctx.BindQuery(&q); err != nil {
						return err
					}
					if err := ctx.BindURI(&u); err != nil {
						return err
					}
					if err := ctx.BindHeader(&h); err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, map[string]any{
						"name": b.Name, "age": b.Age, "active": q.Active, "id": u.ID, "token": h.Token,
					})
				})
			},
			Method: http.MethodPost,
			Target: "/scenario/7?active=true",
			Header: map[string]string{"Content-Type": "application/json", "X-Token": "token-1"},
			Body:   []byte(`{"name":"tom","age":11}`),
		},
		{
			Name: "LargeBody",
			Register: func(r httpx.Router) {
				r.POST("/scenario", func(ctx httpx.Context) error {
					raw, err := ctx.BodyRaw()
					if err != nil {
						return err
					}
					return ctx.JSON(http.StatusOK, map[string]any{"length": len(raw)})
				})
			},
			Method: http.MethodPost,
			Target: "/scenario",
			Body:   bytes.Repeat([]byte("abcdefgh"), 128*1024), // 1 MiB
		},
		{
			Name: "SSE",
			Register: func(r httpx.Router) {
				r.GET("/scenario", func(ctx httpx.Context) error {
					return httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
						for i := range 3 {
							if err := w.SendData("event-" + strconv.Itoa(i)); err != nil {
								return err
							}
						}
						return nil
					})
				})
			},
			Target: "/scenario",
		},
	}

	scenarios = append(scenarios, multipartScenario(), staticScenario())

	for _, layers := range []int{1, 5, 10} {
		n := layers
		scenarios = append(scenarios,
			Scenario{
				Name: "Middleware" + strconv.Itoa(n),
				Register: func(r httpx.Router) {
					for range n {
						r.Use(pass)
					}
					r.GET("/scenario", noContent)
				},
				Target: "/scenario",
			},
			Scenario{
				Name: "Interceptor" + strconv.Itoa(n),
				Register: func(r httpx.Router) {
					for range n {
						httpx.UseInterceptor(r, passInterceptor)
					}
					r.GET("/scenario", noContent)
				},
				Target: "/scenario",
			},
		)
	}

	// Both forms nested over groups, which is the shape a service ends up with.
	scenarios = append(scenarios, Scenario{
		Name: "MixedChain",
		Register: func(r httpx.Router) {
			r.Use(pass, pass)
			httpx.UseInterceptor(r, passInterceptor)
			mid := r.Group("/mid", pass)
			httpx.UseInterceptor(mid, passInterceptor)
			leaf := mid.Group("/leaf", pass)
			httpx.UseInterceptor(leaf, passInterceptor)
			leaf.GET("/scenario", noContent)
		},
		Target: "/mid/leaf/scenario",
	})

	return scenarios
}

func multipartScenario() Scenario {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	// Writing a multipart body into a bytes.Buffer cannot fail for reasons a
	// caller could handle; a failure here is a bug in this table.
	if err := writer.WriteField("title", "sample"); err != nil {
		panic("httpxtest: multipart WriteField: " + err.Error())
	}
	part, err := writer.CreateFormFile("file", "a.txt")
	if err != nil {
		panic("httpxtest: multipart CreateFormFile: " + err.Error())
	}
	if _, err := part.Write(bytes.Repeat([]byte("file-content"), 64)); err != nil {
		panic("httpxtest: multipart write file part: " + err.Error())
	}
	if err := writer.Close(); err != nil {
		panic("httpxtest: multipart Close: " + err.Error())
	}

	return Scenario{
		Name: "MultipartUpload",
		Register: func(r httpx.Router) {
			r.POST("/scenario", func(ctx httpx.Context) error {
				f, err := ctx.FormFile("file")
				if err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"filename": f.Filename,
					"size":     f.Size,
					"title":    ctx.FormValue("title"),
				})
			})
		},
		Method: http.MethodPost,
		Target: "/scenario",
		Header: map[string]string{"Content-Type": writer.FormDataContentType()},
		Body:   buf.Bytes(),
	}
}

// The static scenario serves from an in-memory filesystem: the point is the
// adapter's static route and the interceptor chain around it, not disk I/O,
// and it keeps the table free of temporary directories.
func staticScenario() Scenario {
	assets := fstest.MapFS{
		"asset.txt": &fstest.MapFile{Data: bytes.Repeat([]byte("static"), 64)},
	}
	return Scenario{
		Name:     "StaticFile",
		Register: func(r httpx.Router) { r.StaticFS("/assets", assets) },
		Target:   "/assets/asset.txt",
	}
}

func casesScenarios(t *testing.T, r runner) {
	for _, sc := range Scenarios() {
		t.Run(sc.Name, func(t *testing.T) {
			if sc.BenchmarkOnly {
				t.Skipf("%s is measured but not recorded: its response is too large for a contract", sc.Name)
			}
			r.assertGolden(t, sc.Register, sc.NewRequest())
		})
	}
}

// RunBenchmarks measures every shared scenario against s.
//
// With Suite.Dispatch the measurement goes through the framework's own
// dispatcher and is comparable with the adapter's other benchmarks. Without
// it the fallback goes through httpx.TestRequester, which builds an
// *http.Response and reads its body: that costs 20+ allocations and a few
// microseconds per request, so it measures the harness more than the adapter.
func RunBenchmarks(b *testing.B, s Suite) {
	b.Helper()
	if s.NewEngine == nil {
		b.Fatal("httpxtest: Suite.NewEngine is required")
	}
	for _, sc := range Scenarios() {
		b.Run("scenario="+sc.Name, func(b *testing.B) {
			req, rewind := sc.RewindableRequest()
			if s.Dispatch != nil {
				run := s.Dispatch(b, sc.Register, req)
				b.ReportAllocs()
				for b.Loop() {
					rewind()
					run()
				}
				return
			}

			b.Logf("%s: no Suite.Dispatch; measuring through TestRequester, which dominates the result", s.Name)
			engine := s.NewEngine(b, Options{})
			sc.Register(engine.Group(""))
			requester, ok := httpx.AsTestRequester(engine)
			if !ok {
				b.Fatalf("%s: engine does not support httpx.TestRequester", s.Name)
			}
			b.ReportAllocs()
			for b.Loop() {
				resp, err := requester.Do(sc.NewRequest())
				if err != nil {
					b.Fatalf("%s: serve %s: %v", s.Name, sc.Name, err)
				}
				_ = resp.Body.Close()
			}
		})
	}
}
