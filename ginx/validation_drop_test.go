package ginx

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-sphere/httpx"
)

// ginx is the one adapter that still runs a validator: gin validates inside
// every binding.Binding through its process-wide binding.Validator, which ginx
// must not assign to. These cases pin the seam that keeps httpx.Binder's
// no-validation contract anyway — the verdict is dropped, the decode error is
// not — directly against gin's own bindings, so a gin release that changes the
// error shapes fails here rather than at a 400 nobody expected.

type dropItem struct {
	Name string `json:"name" binding:"required"`
}

func bindJSON(t *testing.T, body string, dst any) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "http://example.com/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return bind(dst, func() error { return binding.JSON.Bind(req, dst) })
}

func TestBindDropsGinValidationVerdict(t *testing.T) {
	t.Run("struct target: validator.ValidationErrors", func(t *testing.T) {
		var dst dropItem
		if err := bindJSON(t, `{}`, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dst.Name != "" {
			t.Fatalf("name = %q, want empty", dst.Name)
		}
	})

	t.Run("slice target: binding.SliceValidationError", func(t *testing.T) {
		var dst []*dropItem
		if err := bindJSON(t, `[{"name":"ok"},{}]`, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dst) != 2 || dst[0].Name != "ok" || dst[1].Name != "" {
			t.Fatalf("dst = %+v", dst)
		}
	})

	// gin's validator panics here (reflect.Value.Interface on the zero Value it
	// gets from dereferencing the nil element) rather than returning an error.
	t.Run("slice target with a null element: the validator panics", func(t *testing.T) {
		var dst []*dropItem
		if err := bindJSON(t, `[{"name":"ok"},null]`, &dst); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dst) != 2 || dst[0].Name != "ok" || dst[1] != nil {
			t.Fatalf("dst = %+v", dst)
		}
	})
}

func TestBindKeepsDecodeError(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		dst        func() any
	}{
		{"malformed json, struct target", `{`, func() any { return new(dropItem) }},
		{"malformed json, slice target", `[`, func() any { return new([]*dropItem) }},
		{"wrong type for a field", `{"name":1}`, func() any { return new(dropItem) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := bindJSON(t, tc.body, tc.dst())
			if err == nil {
				t.Fatal("decode failure was dropped along with the validation verdict")
			}
			if status, _ := httpx.RenderError(err); status != 400 {
				t.Fatalf("status = %d, want 400 (err = %v)", status, err)
			}
		})
	}
}

// A panic that is not the validator's must still be reported: swallowing every
// panic on the slice path would hide a real fault in gin's decode.
func TestBindRecoveringReportsOtherPanics(t *testing.T) {
	err := bindRecovering(func() error { panic(errors.New("boom")) })
	if err == nil {
		t.Fatal("a non-validator panic was swallowed")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want it to mention the panic", err)
	}
}
