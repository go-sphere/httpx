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
	server := http.Handler(http.FileServer(http.FS(fsys)))
	if prefix != "/" {
		server = http.StripPrefix(prefix, server)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(req.URL.Path, prefix)), "/")
		target := rel
		if target == "" {
			target = "."
		}
		info, err := fs.Stat(fsys, target)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		if info.IsDir() {
			// A directory is served by its index.html, the way net/http does
			// it — a single-page app mounted on a prefix depends on that. With
			// no index.html it is 404 rather than FileServer's directory
			// listing.
			if _, err := fs.Stat(fsys, path.Join(target, "index.html")); err != nil {
				http.NotFound(w, req)
				return
			}
		}
		server.ServeHTTP(w, req)
	})
}

// StaticRoutePattern is the route pattern an adapter registers for a static
// mount: the prefix plus a named wildcard, which ValidateWildcardPath accepts
// and every adapter can normalize.
func StaticRoutePattern(prefix string) string {
	return path.Join("/", prefix, "/*filepath")
}
