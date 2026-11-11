package httpxtest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("RequestEdges", casesRequestEdges)
}

// Edges of the request surface the ordinary cases do not reach: the multi-value
// shape of Queries/Headers, a body with no declared length, an encoded path, and
// a body large enough to cross a buffer.
func casesRequestEdges(t *testing.T, r runner) {
	// Queries and Headers return map[string][]string, a shape that only means
	// something when a key repeats.
	t.Run("RepeatedQueryAndHeader", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/edges/repeated?tag=a&tag=b&single=x", nil)
		req.Header.Add("X-Multi", "one")
		req.Header.Add("X-Multi", "two")

		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/edges/repeated", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"queryFirst":  ctx.Query("tag"),
					"queries":     ctx.Queries()["tag"],
					"single":      ctx.Queries()["single"],
					"headerFirst": ctx.Header("X-Multi"),
					"headers":     ctx.Headers()["X-Multi"],
				})
			})
		}, req)
	})

	// A body with no Content-Length: the frameworks read bodies differently
	// (net/http reader, fasthttp buffer), so a length assumption shows up here.
	t.Run("BodyWithoutContentLength", func(t *testing.T) {
		if !r.suite.Caps.InProcessUnknownLengthBody {
			r.skipf(t, "%s: Caps.InProcessUnknownLengthBody is not declared; the in-process requester cannot express an unknown body length", r.suite.Name)
		}
		// An io.Reader body makes httptest declare no length, which is how an
		// unknown-length (chunked on the wire) request is expressed; setting
		// TransferEncoding by hand alongside a negative ContentLength produces an
		// illegal wire form some servers reject.
		req := httptest.NewRequest(http.MethodPost, "http://example.com/edges/chunked",
			io.NopCloser(strings.NewReader("chunked-body")))

		r.assertGolden(t, func(router httpx.Router) {
			router.POST("/edges/chunked", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{
					"body":   string(raw),
					"length": len(raw),
				})
			})
		}, req)
	})

	// Path is documented as always returning the decoded path.
	t.Run("EncodedPathSegment", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/edges/decode/hello%20world", nil)

		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/edges/decode/:seg", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"path":  ctx.Path(),
					"param": ctx.Param("seg"),
				})
			})
		}, req)
	})

	// A '+' in a path is itself, not a space: only form and query encoding give
	// it that meaning. Param, Params, BindURI and Path must all read it so.
	t.Run("PlusInPathParam", func(t *testing.T) {
		for _, tc := range []struct{ name, pattern, target, param, want, path string }{
			{"Param", "/edges/plus/:id", "/edges/plus/a+b%2Bc", "id", "a+b+c", "/edges/plus/a+b+c"},
			{"LeadingPlus", "/edges/plus/:id", "/edges/plus/+8613800", "id", "+8613800", "/edges/plus/+8613800"},
			{"Wildcard", "/edges/plusw/*rest", "/edges/plusw/x+y/z", "rest", "x+y/z", "/edges/plusw/x+y/z"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serve(t, func(router httpx.Router) {
					router.GET(tc.pattern, func(ctx httpx.Context) error {
						var in struct {
							ID   string `uri:"id"`
							Rest string `uri:"rest"`
						}
						if err := ctx.BindURI(&in); err != nil {
							return err
						}
						return ctx.Text(http.StatusOK, strings.Join([]string{
							ctx.Param(tc.param), ctx.Params()[tc.param], in.ID + in.Rest, ctx.Path(),
						}, "|"))
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))
				want := strings.Join([]string{tc.want, tc.want, tc.want, tc.path}, "|")
				if got.Status != http.StatusOK || got.Body != want {
					t.Fatalf("status=%d body=%q, want 200 %q (Param|Params|BindURI|Path)", got.Status, got.Body, want)
				}
			})
		}
	})

	// Routes match the decoded path, so a static segment a client
	// percent-encodes still reaches its route, unless Caps.RawPathRouting
	// declares otherwise.
	t.Run("EncodedStaticSegment", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/edges/caf\u00e9/menu", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "static")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/edges/caf%C3%A9/menu", nil))
		if r.suite.Caps.RawPathRouting {
			if got.Status != http.StatusNotFound {
				t.Fatalf("status=%d, want 404: Caps.RawPathRouting is declared", got.Status)
			}
			return
		}
		if got.Status != http.StatusOK || got.Body != "static" {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "static")
		}
	})

	t.Run("NonASCIIPathSegment", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/edges/decode/"+"%E4%B8%AD%E6%96%87", nil)

		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/edges/decode/:seg", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"path":  ctx.Path(),
					"param": ctx.Param("seg"),
				})
			})
		}, req)
	})

	// A body larger than the frameworks' internal read buffers arrives byte for
	// byte through both readers. Scenarios/LargeBody pins the length; this pins
	// the content, which a buffer reused or stitched at the wrong offset changes
	// without changing the length.
	t.Run("LargeBodyArrivesIntact", func(t *testing.T) {
		payload := strings.Repeat("0123456789abcdef", 64*1024) + "tail" // 1 MiB and a partial chunk
		for _, tc := range []struct {
			name string
			read func(httpx.Context) ([]byte, error)
		}{
			{"BodyRaw", func(ctx httpx.Context) ([]byte, error) { return ctx.BodyRaw() }},
			{"BodyReader", func(ctx httpx.Context) ([]byte, error) { return io.ReadAll(ctx.BodyReader()) }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serve(t, func(router httpx.Router) {
					router.POST("/edges/large", func(ctx httpx.Context) error {
						body, err := tc.read(ctx)
						if err != nil {
							return err
						}
						if string(body) != payload {
							return ctx.Text(http.StatusOK, fmt.Sprintf("corrupted: %d bytes, want %d", len(body), len(payload)))
						}
						return ctx.Text(http.StatusOK, "intact")
					})
				}, httptest.NewRequest(http.MethodPost, "http://example.com/edges/large", strings.NewReader(payload)))

				if got.Status != http.StatusOK || got.Body != "intact" {
					t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "intact")
				}
			})
		}
	})
}
