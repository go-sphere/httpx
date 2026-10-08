package httpx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
)

// CheckServeFile reports whether Responder.File may serve path, so every
// adapter refuses the same paths without writing anything. It returns nil for
// a regular file (symlinks followed); a 404 Error when path does not exist or
// names anything else, a directory included (a directory is never listed,
// redirected or served through an index file); a 403 Error when it cannot be
// stat'ed for lack of permission; and a 500 Error for any other stat failure.
// The returned Error carries no user message, so a rendered error body never
// reveals path.
func CheckServeFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return statError(err)
	}
	return regularFileError(info)
}

// ServeFile implements Responder.File for adapters that write through a
// net/http ResponseWriter. It serves the regular file at path with
// http.ServeContent, so Range, conditional requests and Content-Type
// detection work, but unlike http.ServeFile the request URL plays no part: a
// URL ending in /index.html is not redirected and one containing ".." is not
// rejected. A path CheckServeFile refuses, or one that cannot be opened,
// writes nothing and returns the Error CheckServeFile would report.
func ServeFile(w http.ResponseWriter, r *http.Request, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return statError(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return statError(err)
	}
	if err := regularFileError(info); err != nil {
		return err
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return nil
}

func statError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return NotFoundError(err)
	case errors.Is(err, fs.ErrPermission):
		return ForbiddenError(err)
	default:
		return InternalServerError(err)
	}
}

func regularFileError(info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return NotFoundError(errors.New("httpx: File: not a regular file"))
	}
	return nil
}
