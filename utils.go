package httpx

import "io"

type readCloser struct {
	io.Reader
	closeFn func() error
}

func (rc readCloser) Close() error {
	if rc.closeFn == nil {
		return nil
	}
	return rc.closeFn()
}

// NewReadCloser returns an io.ReadCloser that reads from r and whose Close
// calls closeFn and returns its error. A nil closeFn makes Close a no-op that
// returns nil. Close does not guard against repeated calls; closeFn runs each
// time.
func NewReadCloser(r io.Reader, closeFn func() error) io.ReadCloser {
	return readCloser{r, closeFn}
}
