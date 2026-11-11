package echox

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// echo keeps one anonymous wildcard node per pattern, so GET /files/*a and
// POST /files/*b share "/files/*". Each method must still report the name and
// pattern it was registered with, as on the adapters with named wildcards.
func TestNamedWildcardIsPerMethod(t *testing.T) {
	report := func(name string) httpx.Handler {
		return func(ctx httpx.Context) error {
			var uri struct {
				A string `uri:"a"`
				B string `uri:"b"`
				C string `uri:"c"`
			}
			if err := ctx.BindURI(&uri); err != nil {
				return err
			}
			bound := map[string]string{"a": uri.A, "b": uri.B, "c": uri.C}
			return ctx.Text(http.StatusOK, strings.Join([]string{
				ctx.FullPath(), ctx.Param(name), ctx.Params()[name], bound[name],
			}, "|"))
		}
	}
	engine := New()
	router := engine.Group("")
	router.GET("/files/*a", report("a"))
	router.POST("/files/*b", report("b"))
	router.Any("/any/*c", report("c"))

	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		t.Fatal("echox engine is not a TestRequester")
	}
	for _, tc := range []struct {
		method, path, want string
	}{
		{http.MethodGet, "/files/x/y", "/files/*a|x/y|x/y|x/y"},
		{http.MethodPost, "/files/x/y", "/files/*b|x/y|x/y|x/y"},
		{http.MethodPut, "/any/z", "/any/*c|z|z|z"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			resp, err := requester.Do(httptest.NewRequest(tc.method, tc.path, nil))
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if resp.StatusCode != http.StatusOK || string(body) != tc.want {
				t.Fatalf("status=%d body=%q, want 200 %q", resp.StatusCode, body, tc.want)
			}
		})
	}
}
