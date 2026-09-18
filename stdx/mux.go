package stdx

import (
	"strings"

	"github.com/go-sphere/httpx"
)

// The route tree.
//
// Only what the httpx contract defines is supported: static segments, a
// ":name" parameter that never spans a "/", and a trailing "*name" wildcard
// that takes the rest of the path. At every level matching prefers static over
// parameter over wildcard, and it backtracks, so /a/:x/c and /a/*rest coexist.
//
// Deliberately absent, because net/http's ServeMux has them and they would
// make this adapter disagree with gin/echo/fiber/hertz: trailing-slash
// redirects, path cleaning and case-insensitive fixups. A request matches
// exactly as received, or it does not match.
//
// Matching allocates nothing: the path is consumed in place and captured
// values are appended to a slice the caller owns.
type node struct {
	static map[string]*node
	// param matches one segment and binds it to paramName.
	param     *node
	paramName string
	// wildcard terminates the path: it binds the remainder to wildcardName.
	wildcard     *node
	wildcardName string
	// routes are the handlers registered on this node, by method.
	routes map[string]*route
}

type route struct {
	// pattern is the path as registered, so FullPath reads the same here as on
	// the adapters with a native router.
	pattern string
	// params names the captured values in match order.
	params  []string
	chain   []httpx.Middleware
	handler httpx.Handler
}

// add registers pattern for method. Conflicting registrations panic, like
// every other adapter: it is a programming error, and startup is when it
// should be found.
func (n *node) add(method, pattern string, r *route) {
	current := n
	rest := pattern
	for rest != "" {
		seg, next := cutSegment(rest)
		rest = next
		switch {
		case strings.HasPrefix(seg, "*"):
			// ValidateWildcardPath has already rejected everything but a
			// single, final wildcard, so this ends the pattern.
			name := seg[1:]
			if current.wildcard == nil {
				current.wildcard = &node{}
				current.wildcardName = name
			} else if current.wildcardName != name {
				panic("stdx: wildcard *" + name + " conflicts with *" + current.wildcardName + " on " + pattern)
			}
			r.params = append(r.params, name)
			current = current.wildcard
		case strings.HasPrefix(seg, ":"):
			name := seg[1:]
			if current.param == nil {
				current.param = &node{}
				current.paramName = name
			} else if current.paramName != name {
				panic("stdx: parameter :" + name + " conflicts with :" + current.paramName + " on " + pattern)
			}
			r.params = append(r.params, name)
			current = current.param
		default:
			if current.static == nil {
				current.static = make(map[string]*node, 4)
			}
			child, ok := current.static[seg]
			if !ok {
				child = &node{}
				current.static[seg] = child
			}
			current = child
		}
	}
	if current.routes == nil {
		current.routes = make(map[string]*route, 2)
	}
	if _, exists := current.routes[method]; exists {
		panic("stdx: duplicate route " + method + " " + pattern)
	}
	current.routes[method] = r
}

// match resolves path for method.
//
// A nil route with a non-empty allow means the path exists under other methods
// — 405 rather than 404. That distinction is why matching does not stop at the
// first node without a handler for this method.
func (n *node) match(method, path string, values *[]string) (*route, []string) {
	return n.walk(method, path, values)
}

func (n *node) walk(method, rest string, values *[]string) (*route, []string) {
	if rest == "" {
		if r, ok := n.routes[method]; ok {
			return r, nil
		}
		// A wildcard is deliberately not tried here. It would make
		// /assets/*filepath answer a bare /assets, and then a table holding
		// both /:p0 and /v1/*rest would answer /v1 from the wildcard while
		// every framework answers it from /:p0. /assets/ still reaches the
		// wildcard: a trailing slash is an empty segment, not an exhausted
		// path, so it goes through the branch below with an empty remainder.
		if len(n.routes) > 0 {
			return nil, methodsOf(n.routes)
		}
		return nil, nil
	}

	seg, next := cutSegment(rest)
	var allow []string

	if child, ok := n.static[seg]; ok {
		if r, a := child.walk(method, next, values); r != nil {
			return r, nil
		} else {
			allow = mergeMethods(allow, a)
		}
	}
	if n.param != nil && seg != "" {
		mark := len(*values)
		*values = append(*values, seg)
		if r, a := n.param.walk(method, next, values); r != nil {
			return r, nil
		} else {
			allow = mergeMethods(allow, a)
			*values = (*values)[:mark]
		}
	}
	if n.wildcard != nil {
		if r, ok := n.wildcard.routes[method]; ok {
			*values = append(*values, strings.TrimPrefix(rest, "/"))
			return r, nil
		}
		allow = mergeMethods(allow, methodsOf(n.wildcard.routes))
	}
	return nil, allow
}

// cutSegment splits "/a/b" into "a" and "/b". A path that has been consumed is
// "", and "/" is a single empty segment — which is what makes /files/ and
// /files different routes, as they are on every other adapter.
func cutSegment(rest string) (seg, next string) {
	rest = strings.TrimPrefix(rest, "/")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i], rest[i:]
	}
	return rest, ""
}

func methodsOf(routes map[string]*route) []string {
	if len(routes) == 0 {
		return nil
	}
	out := make([]string, 0, len(routes))
	for method := range routes {
		out = append(out, method)
	}
	return out
}

func mergeMethods(dst, src []string) []string {
	for _, m := range src {
		if !containsString(dst, m) {
			dst = append(dst, m)
		}
	}
	return dst
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
