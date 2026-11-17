//go:build !windows

package httpx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServeFileRejectsFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/pipe", nil)
		rec := httptest.NewRecorder()
		done <- ServeFile(rec, req, fifo)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ServeFile(fifo) = nil, want 404 NotFoundError")
		}
		_, status, _ := ParseError(err)
		if status != http.StatusNotFound {
			t.Fatalf("ServeFile(fifo) status = %d (err=%v), want 404", status, err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeFile blocked opening FIFO")
	}
}
