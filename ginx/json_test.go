package ginx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/codec/json"
)

type testJSONCodec struct {
	json.Core
	err error
}

func (c testJSONCodec) Marshal(any) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return []byte(`{"codec":"custom"}`), nil
}

func TestJSONUsesConfiguredCodec(t *testing.T) {
	original := json.API
	t.Cleanup(func() { json.API = original })
	json.API = testJSONCodec{Core: original}
	for _, status := range []int{http.StatusCreated, http.StatusNoContent, http.StatusNotModified} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			gc, _ := gin.CreateTestContext(w)
			if err := FromGin(gc).JSON(status, struct{}{}); err != nil {
				t.Fatal(err)
			}
			if w.Code != status {
				t.Fatalf("status = %d, want %d", w.Code, status)
			}
			expected := ""
			if status == http.StatusCreated {
				expected = `{"codec":"custom"}`
			}
			if w.Body.String() != expected {
				t.Fatalf("body = %q, want %q", w.Body.String(), expected)
			}
			if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatal("missing JSON content type")
			}
		})
	}
	sentinel := errors.New("encoding failed")
	json.API = testJSONCodec{Core: original, err: sentinel}
	w := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(w)
	if err := FromGin(gc).JSON(201, struct{}{}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if gc.Writer.Written() || gc.Writer.Status() != 200 || w.Body.Len() != 0 {
		t.Fatal("encoding failure committed the response")
	}
	json.API = nil
	if err := FromGin(gc).JSON(200, struct{}{}); err == nil {
		t.Fatal("expected an error for a missing codec")
	}
}
