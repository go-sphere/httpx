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

func NewReadCloser(r io.Reader, closeFn func() error) io.ReadCloser {
	return readCloser{r, closeFn}
}
