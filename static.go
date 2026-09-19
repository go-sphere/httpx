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
	prefix := path.Clean("/" + strings.Trim(urlPrefix, "/"))
	server := http.Handler(http.FileServer(indexOnlyFS{open: http.FS(fsys), stat: fsys}))
	if prefix != "/" {
		server = http.StripPrefix(prefix, server)
	}
	return server
}

// indexOnlyFS is what suppresses FileServer's directory listing: a directory
// with no index.html does not open at all, so FileServer answers 404 instead of
// rendering one.
//
// Deciding it here rather than in a handler that runs first is what keeps an
// ordinary file request to a single open. The pre-flight fs.Stat this replaced
// looked the file up a second time — on an os.DirFS that was a whole extra
// stat syscall, about 30% of the cost of serving the file — and it reimplemented
// the prefix trimming and path cleaning that FileServer already does, which is
// the part of a static mount least worth having a second copy of.
type indexOnlyFS struct {
	open http.FileSystem
	// stat is the same tree as open, kept alongside it so the index probe
	// costs one stat rather than the open-and-close that reaching it through
	// http.FileSystem would.
	stat fs.FS
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
		// would take the index down with it; probe for one instead.
		//
		// FileServer has already cleaned name, so dropping the leading slash is
		// the whole of the translation to an fs.FS path.
		dir := strings.TrimPrefix(name, "/")
		if dir == "" {
			dir = "."
		}
		if _, err := fs.Stat(f.stat, path.Join(dir, "index.html")); err != nil {
			_ = file.Close()
			return nil, fs.ErrNotExist
		}
	}
	return statedFile{File: file, info: info}, nil
}

// statedFile answers Stat from the lookup Open already paid for, so the check
// above costs nothing: FileServer stats every file it opens.
type statedFile struct {
	http.File
	info fs.FileInfo
}

func (f statedFile) Stat() (fs.FileInfo, error) { return f.info, nil }

// StaticRoutePattern is the route pattern an adapter registers for a static
// mount: the prefix plus a named wildcard, which ValidateWildcardPath accepts
// and every adapter can normalize.
func StaticRoutePattern(prefix string) string {
	return path.Join("/", prefix, "/*filepath")
}
