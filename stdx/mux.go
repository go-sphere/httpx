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
// Matching allocates nothing and hashes nothing on the hot path: a segment is
// compared against a short slice (a map only appears on wide nodes) and the
// method indexes a fixed array, so a request costs string compares instead of
// map lookups.
type node struct {
	// Parallel slices instead of a map: a node usually has a handful of
	// children, and comparing a few short strings beats hashing one.
	staticKeys  []string
	staticNodes []*node
	// staticMap takes over above staticSliceMax children, where the linear
	// scan would start to lose.
	staticMap map[string]*node
	// param matches one segment and binds it to paramName.
	param     *node
	paramName string
	// wildcard terminates the path: it binds the remainder to wildcardName.
	wildcard     *node
	wildcardName string
	// routes stays nil on inner nodes, which is most of the tree.
	routes *methodRoutes
}

// staticSliceMax is where a linear scan stops being cheaper than a map.
const staticSliceMax = 8

type route struct {
	// pattern is the path as registered, so FullPath reads the same here as on
	// the adapters with a native router.
	pattern string
	// params names the captured values in match order.
	params  []string
	chain   []httpx.Middleware
	handler httpx.Handler
}

// methodRoutes keeps the nine standard methods in a fixed array so dispatch is
// an index rather than a map lookup. Anything else (a WebDAV verb, say) still
// works, through the rare map.
type methodRoutes struct {
	common [len(methodNames)]*route
	rare   map[string]*route
}

var methodNames = [...]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE"}

// methodIndex maps a canonical method name to its slot, or -1 for the rest.
func methodIndex(method string) int {
	switch method {
	case "GET":
		return 0
	case "POST":
		return 1
	case "PUT":
		return 2
	case "PATCH":
		return 3
	case "DELETE":
		return 4
	case "HEAD":
		return 5
	case "OPTIONS":
		return 6
	case "CONNECT":
		return 7
	case "TRACE":
		return 8
	default:
		return -1
	}
}

func (m *methodRoutes) get(method string) *route {
	if i := methodIndex(method); i >= 0 {
		return m.common[i]
	}
	return m.rare[method]
}

// set reports whether the method was already registered here.
func (m *methodRoutes) set(method string, r *route) bool {
	if i := methodIndex(method); i >= 0 {
		if m.common[i] != nil {
			return true
		}
		m.common[i] = r
		return false
	}
	if m.rare == nil {
		m.rare = make(map[string]*route, 1)
	}
	if _, exists := m.rare[method]; exists {
		return true
	}
	m.rare[method] = r
	return false
}

func (m *methodRoutes) methods() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(methodNames))
	for i, r := range m.common {
		if r != nil {
			out = append(out, methodNames[i])
		}
	}
	for method := range m.rare {
		out = append(out, method)
	}
	return out
}

func (n *node) child(seg string) *node {
	if n.staticMap != nil {
		return n.staticMap[seg]
	}
	for i, key := range n.staticKeys {
		if key == seg {
			return n.staticNodes[i]
		}
	}
	return nil
}

func (n *node) childOrCreate(seg string) *node {
	if child := n.child(seg); child != nil {
		return child
	}
	child := &node{}
	if n.staticMap != nil {
		n.staticMap[seg] = child
		return child
	}
	n.staticKeys = append(n.staticKeys, seg)
	n.staticNodes = append(n.staticNodes, child)
	if len(n.staticKeys) > staticSliceMax {
		n.staticMap = make(map[string]*node, len(n.staticKeys)*2)
		for i, key := range n.staticKeys {
			n.staticMap[key] = n.staticNodes[i]
		}
		n.staticKeys, n.staticNodes = nil, nil
	}
	return child
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
			current = current.childOrCreate(seg)
		}
	}
	if current.routes == nil {
		current.routes = &methodRoutes{}
	}
	if current.routes.set(method, r) {
		panic("stdx: duplicate route " + method + " " + pattern)
	}
}

// match resolves path for method.
//
// A nil route with a non-empty allow means the path exists under other methods
// — 405 rather than 404. That distinction is why matching does not stop at the
// first node without a handler for this method.
func (n *node) match(method, path string, values *[]string) (*route, []string) {
	return n.walk(method, path, values)
}

// walk descends one level at a time, iteratively while the tree leaves no
// choice and recursively (through walkChoice) where it does.
//
// The split matters because an ordinary route table is unambiguous: at
// /api/v1/users/:id every level offers exactly one way forward, so nothing has
// to be remembered in case of a retreat. Recursing anyway costs a frame and a
// two-value return per segment.
func (n *node) walk(method, rest string, values *[]string) (*route, []string) {
	for rest != "" {
		seg, next := cutSegment(rest)
		child := n.child(seg)
		switch {
		case child != nil && n.param == nil && n.wildcard == nil:
			n, rest = child, next
		case child == nil && n.param != nil && n.wildcard == nil && seg != "":
			*values = append(*values, seg)
			n, rest = n.param, next
		case child == nil && n.param == nil:
			if n.wildcard == nil || n.wildcard.routes == nil {
				return nil, nil
			}
			if r := n.wildcard.routes.get(method); r != nil {
				*values = append(*values, trimLeadingSlash(rest))
				return r, nil
			}
			return nil, n.wildcard.routes.methods()
		default:
			// More than one branch could match from here: the walk has to be
			// able to come back, so hand this level to the recursive form.
			return n.walkChoice(method, rest, values)
		}
	}
	return n.walkTerminal(method)
}

func (n *node) walkTerminal(method string) (*route, []string) {
	if n.routes == nil {
		return nil, nil
	}
	if r := n.routes.get(method); r != nil {
		return r, nil
	}
	// A wildcard is deliberately not tried here; see walkChoice.
	return nil, n.routes.methods()
}

func (n *node) walkChoice(method, rest string, values *[]string) (*route, []string) {
	if rest == "" {
		if n.routes == nil {
			return nil, nil
		}
		if r := n.routes.get(method); r != nil {
			return r, nil
		}
		// A wildcard is deliberately not tried here. It would make
		// /assets/*filepath answer a bare /assets, and then a table holding
		// both /:p0 and /v1/*rest would answer /v1 from the wildcard while
		// every framework answers it from /:p0. /assets/ still reaches the
		// wildcard: a trailing slash is an empty segment, not an exhausted
		// path, so it goes through the branch below with an empty remainder.
		return nil, n.routes.methods()
	}

	seg, next := cutSegment(rest)
	var allow []string

	if child := n.child(seg); child != nil {
		mark := len(*values)
		r, a := child.walk(method, next, values)
		if r != nil {
			return r, nil
		}
		*values = (*values)[:mark]
		allow = a
	}
	if n.param != nil && seg != "" {
		mark := len(*values)
		*values = append(*values, seg)
		r, a := n.param.walk(method, next, values)
		if r != nil {
			return r, nil
		}
		allow = mergeMethods(allow, a)
		*values = (*values)[:mark]
	}
	if n.wildcard != nil && n.wildcard.routes != nil {
		if r := n.wildcard.routes.get(method); r != nil {
			*values = append(*values, trimLeadingSlash(rest))
			return r, nil
		}
		allow = mergeMethods(allow, n.wildcard.routes.methods())
	}
	return nil, allow
}

// cutSegment splits "/a/b" into "a" and "/b". A path that has been consumed is
// "", and "/" is a single empty segment — which is what makes /files/ and
// /files different routes, as they are on every other adapter.
func cutSegment(rest string) (seg, next string) {
	rest = trimLeadingSlash(rest)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i], rest[i:]
	}
	return rest, ""
}

func trimLeadingSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
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
