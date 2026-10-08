package fiberx

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// routeOrder keeps the routes this adapter registers on one fiber.App in the
// precedence order of the httpx route grammar: a static segment beats a
// parameter, and a parameter beats a wildcard, whatever order the routes were
// registered in. fiber itself tries routes in registration order, so
// /users/:id registered before /users/new would otherwise answer /users/new.
//
// A newly added route is moved up its method's stack to just before the first
// route of this adapter it must beat. The stack is fiber's own slice, so the
// order holds for every way the app is served: Start, Do, or a caller driving
// app.Handler() of an engine passed through WithEngine.
//
// It is shared by every Router of one Engine and, like route registration, is
// not safe for concurrent use.
type routeOrder struct {
	app     *fiber.App
	methods []string
	shapes  map[*fiber.Route][]routeSegment
}

func newRouteOrder(app *fiber.App) *routeOrder {
	return &routeOrder{
		app:     app,
		methods: app.Config().RequestMethods,
		shapes:  make(map[*fiber.Route][]routeSegment),
	}
}

type segmentKind uint8

// Lower kinds take precedence.
const (
	segmentStatic segmentKind = iota
	segmentParam
	segmentWildcard
)

type routeSegment struct {
	kind  segmentKind
	value string
}

func parseRouteShape(pattern string) []routeSegment {
	parts := strings.Split(strings.Trim(pattern, "/"), "/")
	shape := make([]routeSegment, 0, len(parts))
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, ":"):
			shape = append(shape, routeSegment{kind: segmentParam})
		case strings.HasPrefix(part, "*"):
			shape = append(shape, routeSegment{kind: segmentWildcard})
		default:
			shape = append(shape, routeSegment{kind: segmentStatic, value: part})
		}
	}
	return shape
}

// beats reports whether a route shaped a must be tried before one shaped b.
// The routes are compared segment by segment, and at the first segment where
// their kinds differ the more specific kind wins; two different static
// segments leave them unordered. Later segments are not compared, so routes no
// single path matches (/users/new/edit and /users/:id) can still be ordered:
// harmless for routing, and it keeps the relation a consistent order, which
// placing each new route before the first one it beats relies on.
func beats(a, b []routeSegment) bool {
	for i := range min(len(a), len(b)) {
		sa, sb := a[i], b[i]
		if sa.kind != sb.kind || sa.kind == segmentWildcard {
			return sa.kind < sb.kind
		}
		if sa.kind == segmentStatic && sa.value != sb.value {
			return false
		}
	}
	// A trailing wildcard can match the path its prefix names on its own.
	return len(a) < len(b) && b[len(a)].kind == segmentWildcard
}

// mayMatch reports whether an entry registered on the app directly with path
// could handle a request that a route shaped shape also matches. The entry's
// kind (middleware, mount, route) is not visible, so its path is treated as a
// prefix, and everything from its first non-static segment on as matching any
// rest; the answer can be a false positive but never a false negative.
func mayMatch(path string, shape []routeSegment, caseSensitive bool) bool {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return true
	}
	for i, part := range strings.Split(trimmed, "/") {
		if strings.ContainsAny(part, ":*+") {
			return true
		}
		if i >= len(shape) {
			return false
		}
		switch seg := shape[i]; seg.kind {
		case segmentWildcard:
			return true
		case segmentStatic:
			if caseSensitive && seg.value != part || !caseSensitive && !strings.EqualFold(seg.value, part) {
				return false
			}
		}
	}
	return true
}

// place records the route just registered for each of methods and moves it
// before the routes of this adapter it beats. It panics when that would move
// it across anything registered on the app directly, such as UseNative
// middleware, that could match a request the route matches, since that could
// change which middleware wraps it. Because beats also orders some routes no
// path shares, the panic can name a move that would not change routing.
func (o *routeOrder) place(methods []string) {
	stack := o.app.Stack()
	for i, method := range o.methods {
		if !slices.Contains(methods, method) || i >= len(stack) || len(stack[i]) == 0 {
			continue
		}
		routes := stack[i]
		added := routes[len(routes)-1]
		if _, known := o.shapes[added]; known || len(added.Handlers) != 1 {
			// fiber merged the handler into an identical route registered
			// just before, so nothing new entered the stack.
			continue
		}
		shape := parseRouteShape(added.Path)
		o.shapes[added] = shape
		target := -1
		for j, route := range routes[:len(routes)-1] {
			if other, ok := o.shapes[route]; ok && beats(shape, other) {
				target = j
				break
			}
		}
		if target < 0 {
			continue
		}
		for _, route := range routes[target : len(routes)-1] {
			if _, ok := o.shapes[route]; !ok && mayMatch(route.Path, shape, o.app.Config().CaseSensitive) {
				panic(fmt.Sprintf("fiberx: %s %s must take precedence over %s, but a handler registered on the fiber app directly (UseNative, a mount, a native route) sits between them; register those before the routes",
					method, added.Path, routes[target].Path))
			}
		}
		copy(routes[target+1:], routes[target:len(routes)-1])
		routes[target] = added
	}
}
