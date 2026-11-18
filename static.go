package httpx

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// StaticFileHandler serves fsys as a plain net/http handler mounted at
// urlPrefix, which every adapter uses for Router.Static and Router.StaticFS.
//
// Serving through net/http's FileServer is what gives Range requests,
// If-Modified-Since (304) and content sniffing the same behavior on every
// adapter instead of one hand-rolled loop per framework. A directory — or the
// bare prefix — is 404 rather than a listing.
//
// urlPrefix must be the path the route is actually mounted on, including any
// router group prefix, because that is what gets stripped before the lookup.
func StaticFileHandler(urlPrefix string, fsys fs.FS) http.Handler {
	if fsys == nil {
		return http.NotFoundHandler()
	}
	prefix := path.Clean("/" + strings.Trim(urlPrefix, "/"))
	server := http.Handler(http.FileServer(indexOnlyFS{open: http.FS(fsys), fsys: fsys}))
	if prefix == "/" {
		return server
	}
	stripped := http.StripPrefix(prefix, server)
	mountPrefix := prefix + "/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, mountPrefix) {
			http.NotFound(w, r)
			return
		}
		stripped.ServeHTTP(w, r)
	})
}

// indexOnlyFS is what suppresses FileServer's directory listing: a directory
// with no readable regular index.html does not open at all, so FileServer
// answers 404 instead of rendering a listing.
//
// Deciding it here rather than in a handler that runs first is what keeps an
// ordinary file request to a single open.
type indexOnlyFS struct {
	open http.FileSystem
	fsys fs.FS
}

func (f indexOnlyFS) Open(name string) (http.File, error) {
	file, err := f.open.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		// A directory is served by its index.html, the way net/http does it — a
		// single-page app mounted on a prefix depends on that. FileServer asks
		// for the directory before it asks for the index, so refusing here
		// would take the index down with it; probe for a readable regular
		// index.html instead.
		//
		// FileServer has already cleaned name, so dropping the leading slash is
		// the whole of the translation to an fs.FS path.
		dir := strings.TrimPrefix(name, "/")
		if dir == "" {
			dir = "."
		}
		idx, err := f.fsys.Open(path.Join(dir, "index.html"))
		if err != nil {
			_ = file.Close()
			return nil, fs.ErrNotExist
		}
		idxInfo, statErr := idx.Stat()
		_ = idx.Close()
		if statErr != nil || idxInfo.IsDir() {
			_ = file.Close()
			return nil, fs.ErrNotExist
		}
	}
	return statedFile{File: file, info: info}, nil
}

// statedFile answers Stat from the lookup Open already paid for, so the check
// above costs nothing: FileServer stats every file it opens. Readdir always
// refuses so FileServer can never fall through to an HTML directory listing.
type statedFile struct {
	http.File
	info fs.FileInfo
}

func (f statedFile) Stat() (fs.FileInfo, error) { return f.info, nil }

func (f statedFile) Readdir(int) ([]fs.FileInfo, error) { return nil, fs.ErrPermission }

// StaticRoutePattern is the route pattern an adapter registers for a static
// mount: the prefix plus a named wildcard, which ValidateWildcardPath accepts
// and every adapter can normalize.
func StaticRoutePattern(prefix string) string {
	return path.Join("/", prefix, "/*filepath")
}
