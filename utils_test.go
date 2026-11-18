package httpx

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestNewReadCloser(t *testing.T) {
	t.Parallel()
	rc := NewReadCloser(bytes.NewReader([]byte("hello")), nil)
	if _, ok := rc.(io.WriterTo); !ok {
		t.Fatalf("NewReadCloser(..., nil) lost io.WriterTo on *bytes.Reader (%T)", rc)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, rc); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello" {
		t.Fatalf("read %q, want %q", out.String(), "hello")
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}

	wantErr := errors.New("close failed")
	rc2 := NewReadCloser(bytes.NewReader(nil), func() error { return wantErr })
	if err := rc2.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close = %v, want %v", err, wantErr)
	}
}
