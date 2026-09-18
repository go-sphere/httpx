package hertzx

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server/render"
	hjson "github.com/cloudwego/hertz/pkg/common/json"
)

func TestJSONUsesConfiguredMarshaler(t *testing.T) {
	// Hertz exposes a setter but no getter; the package starts with hjson.Marshal.
	t.Cleanup(func() { render.ResetJSONMarshal(hjson.Marshal) })
	render.ResetJSONMarshal(func(any) ([]byte, error) { return []byte(`{"codec":"custom"}`), nil })
	rc := app.NewContext(0)
	if err := FromHertz(t.Context(), rc).JSON(201, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if rc.Response.StatusCode() != 201 || string(rc.Response.Body()) != `{"codec":"custom"}` {
		t.Fatalf("unexpected response: status=%d body=%q", rc.Response.StatusCode(), rc.Response.Body())
	}
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		rc := app.NewContext(0)
		if err := FromHertz(t.Context(), rc).JSON(status, struct{}{}); err != nil {
			t.Fatal(err)
		}
		if rc.Response.StatusCode() != status || len(rc.Response.Body()) != 0 {
			t.Fatalf("status %d must have no body: %q", status, rc.Response.Body())
		}
	}
	sentinel := errors.New("encoding failed")
	render.ResetJSONMarshal(func(any) ([]byte, error) { return nil, sentinel })
	rc = app.NewContext(0)
	if err := FromHertz(t.Context(), rc).JSON(201, struct{}{}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if rc.Response.StatusCode() != 200 || len(rc.Response.Body()) != 0 {
		t.Fatal("encoding failure committed the response")
	}
}
