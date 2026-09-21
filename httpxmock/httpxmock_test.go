package httpxmock_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

// TestSatisfiesContextWithoutEmbedding is the guarantee the package exists
// for: every method of httpx.Context is really implemented, so calling one
// the test never thought about returns a zero value rather than panicking on
// the nil interface an embedding fake would have left behind.
func TestSatisfiesContextWithoutEmbedding(t *testing.T) {
	var ctx httpx.Context = httpxmock.New(nil)

	ifaceType := reflect.TypeOf((*httpx.Context)(nil)).Elem()
	mockType := reflect.TypeOf(ctx)
	for i := range ifaceType.NumMethod() {
		name := ifaceType.Method(i).Name
		if _, ok := mockType.MethodByName(name); !ok {
			t.Fatalf("*httpxmock.Context is missing %s", name)
		}
	}

	// Satisfying the interface is not enough on its own, because embedding
	// httpx.Context satisfies it too and is exactly the pattern this package
	// replaces. No field of the struct may be an embedded interface, so
	// every method above has a real body.
	structType := reflect.TypeOf((*httpxmock.Context)(nil)).Elem()
	for i := range structType.NumField() {
		f := structType.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Interface {
			t.Errorf("field %s embeds interface %s; methods must be implemented, not promoted", f.Name, f.Type)
		}
	}

	// Nothing is exercised here beyond the calls themselves: the assertion is
	// that a bare context survives its whole surface.
	ctx.Method()
	ctx.Path()
	ctx.FullPath()
	ctx.ClientIP()
	ctx.Param("missing")
	ctx.Params()
	ctx.Query("missing")
	ctx.Queries()
	ctx.RawQuery()
	ctx.Header("missing")
	ctx.Headers()
	_, _ = ctx.Cookie("missing")
	ctx.Cookies()
	ctx.FormValue("missing")
	_, _ = ctx.MultipartForm()
	_, _ = ctx.FormFile("missing")
	_, _ = ctx.BodyRaw()
	_ = ctx.BodyReader()
	ctx.Set("k", "v")
	_, _ = ctx.Get("k")
	ctx.Status(http.StatusTeapot)
	ctx.SetHeader("X-Test", "1")
	ctx.SetCookie(nil)
	ctx.StatusCode()
	ctx.SetContext(ctx.Context())
}

func TestCapabilityProbes(t *testing.T) {
	var ctx httpx.Context = httpxmock.New(nil)

	if _, ok := httpx.AsFlusher(ctx); !ok {
		t.Error("AsFlusher: want supported")
	}
	if _, ok := httpx.AsStreamer(ctx); !ok {
		t.Error("AsStreamer: want supported")
	}
	// There is no native context to hand out, so the probe must say so
	// rather than return a stand-in nobody can use.
	if _, ok := httpx.AsNativeContext[any](ctx); ok {
		t.Error("AsNativeContext: want unsupported")
	}
}

func TestBindMethodsReportUnsupported(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodPost, "/?a=1", nil)
	var dst struct {
		A string `json:"a" query:"a" form:"a" uri:"a" header:"a"`
	}
	binds := map[string]func(any) error{
		"BindJSON":   ctx.BindJSON,
		"BindQuery":  ctx.BindQuery,
		"BindForm":   ctx.BindForm,
		"BindURI":    ctx.BindURI,
		"BindHeader": ctx.BindHeader,
	}
	for name, bind := range binds {
		if err := bind(&dst); !errors.Is(err, httpxmock.ErrBindUnsupported) {
			t.Errorf("%s = %v, want ErrBindUnsupported", name, err)
		}
	}
	// It must not classify as a client error: a handler rendering it would
	// otherwise answer a plausible 400 and hide that nothing was decoded.
	if _, status, _ := httpx.ClassifyError(httpxmock.ErrBindUnsupported); status != http.StatusInternalServerError {
		t.Errorf("ClassifyError status = %d, want 500", status)
	}
}

func TestStateStoreNilIsAbsent(t *testing.T) {
	ctx := httpxmock.New(nil, httpxmock.WithState("seeded", "yes"))

	if got, ok := ctx.Get("seeded"); !ok || got != "yes" {
		t.Errorf("Get(seeded) = %v, %v; want yes, true", got, ok)
	}
	if _, ok := ctx.Get("never-set"); ok {
		t.Error("Get(never-set): want ok=false")
	}

	ctx.Set("explicit-nil", nil)
	if got, ok := ctx.Get("explicit-nil"); ok || got != nil {
		t.Errorf("Get(explicit-nil) = %v, %v; want nil, false", got, ok)
	}

	// A typed nil is a value, not an absence: only an untyped nil reads back
	// as missing, which is what the adapters do.
	var typedNil *int
	ctx.Set("typed-nil", typedNil)
	if _, ok := ctx.Get("typed-nil"); !ok {
		t.Error("Get(typed-nil): want ok=true")
	}
}

type ctxKey string

func TestStateStoreIsSeparateFromStandardContext(t *testing.T) {
	ctx := httpxmock.New(nil)

	ctx.Set("state-only", "visible-to-handlers")
	if got := ctx.Context().Value(ctxKey("state-only")); got != nil {
		t.Errorf("Context().Value = %v, want nil: Set must not reach context.Context", got)
	}
	if got := ctx.Context().Value("state-only"); got != nil {
		t.Errorf("Context().Value = %v, want nil", got)
	}

	ctx.SetContext(context.WithValue(ctx.Context(), ctxKey("carried"), 42))
	if got := ctx.Context().Value(ctxKey("carried")); got != 42 {
		t.Errorf("Context().Value(carried) = %v, want 42", got)
	}
	// And the reverse direction: a context value is not state-store state.
	if _, ok := ctx.Get("carried"); ok {
		t.Error("Get(carried): want ok=false, SetContext must not reach the state store")
	}
}

func TestSetContextIgnoresNil(t *testing.T) {
	ctx := httpxmock.New(nil)
	// Through a variable, because a literal nil here is what staticcheck
	// exists to stop and the case being pinned is the accident, not the
	// idiom: a middleware that derives a context from a call that failed
	// should not take the rest of the chain down with a nil.
	var missing context.Context
	ctx.SetContext(missing)
	if ctx.Context() == nil {
		t.Fatal("Context() = nil after SetContext(nil)")
	}
}

func TestCallCountsSupportShortCircuitAssertions(t *testing.T) {
	ctx := httpxmock.NewRequest(http.MethodGet, "/v1/users", nil,
		httpxmock.WithFullPath("/v1/users"))

	// The shape of a matcher that compares the method first and gives up
	// before it reads the route pattern.
	if ctx.Method() == http.MethodPost {
		_ = ctx.FullPath()
	}
	if got := ctx.CallCount("Method"); got != 1 {
		t.Errorf("CallCount(Method) = %d, want 1", got)
	}
	if got := ctx.CallCount("FullPath"); got != 0 {
		t.Errorf("CallCount(FullPath) = %d, want 0", got)
	}
	if got := ctx.Calls()["Method"]; got != 1 {
		t.Errorf("Calls()[Method] = %d, want 1", got)
	}
	// Reading the response back must not look like handler activity.
	ctx.StatusCode()
	_ = ctx.Body()
	_ = ctx.Committed()
	if got := ctx.CallCount("StatusCode"); got != 1 {
		t.Errorf("CallCount(StatusCode) = %d, want 1", got)
	}
}

func TestConcurrentUseIsRaceFree(t *testing.T) {
	// Not a behavioral claim about the adapters, whose contexts are
	// single-goroutine: this is the property that lets a downstream stress
	// test share one mock instead of writing a second atomic copy of it.
	ctx := httpxmock.NewRequest(http.MethodGet, "/stress?a=1", nil,
		httpxmock.WithFullPath("/stress"), httpxmock.WithParam("id", "7"))

	const workers = 8
	done := make(chan struct{}, workers)
	for i := range workers {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			ctx.Set("k", i)
			_, _ = ctx.Get("k")
			_ = ctx.Method()
			_ = ctx.FullPath()
			_ = ctx.Param("id")
			_ = ctx.Query("a")
			ctx.SetHeader("X-Worker", "1")
			_ = ctx.JSON(http.StatusOK, map[string]int{"i": i})
			_ = ctx.StatusCode()
			_ = ctx.Body()
			_ = ctx.Committed()
		}(i)
	}
	for range workers {
		<-done
	}
	if ctx.StatusCode() != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", ctx.StatusCode())
	}
}
