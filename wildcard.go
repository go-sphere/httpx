package httpx

import (
	"fmt"
	"strings"
)

// ValidateWildcardPath reports whether path uses wildcards in the only form
// every adapter supports: at most one wildcard, **named**, starting its own
// path segment ("/files/*name"), as the final segment. Other shapes
// ("/a/*x/b/*y", "/foo*bar") panic at registration on gin/hertz but would
// silently register a semantically different route on fiber; every adapter
// calls this from every registration entry point, so all five fail loudly and
// identically — with this error, not a framework's — at registration time.
//
// The anonymous wildcard ("/files/*") is rejected for the same reason: gin and
// hertz panic on it natively while echo, fiber and stdx register it, and among
// those three the parameter is keyed "*" on echo and fiber but "" on stdx. The
// named form is the one for which Param, Params and BindURI agree on all five.
func ValidateWildcardPath(path string) error {
	star := strings.IndexByte(path, '*')
	if star == -1 {
		return nil
	}
	if star == 0 || path[star-1] != '/' {
		return fmt.Errorf("httpx: wildcard must start its own path segment in %q", path)
	}
	rest := path[star+1:]
	if i := strings.IndexByte(rest, '/'); i != -1 {
		return fmt.Errorf("httpx: wildcard must be the final path segment in %q", path)
	}
	if strings.IndexByte(rest, '*') != -1 {
		return fmt.Errorf("httpx: only one wildcard is allowed in %q", path)
	}
	if rest == "" {
		return fmt.Errorf("httpx: wildcard must be named in %q: write %q", path, path+"name")
	}
	return nil
}

// FixWildcardPathIfNeed normalizes wildcard path syntax based on router capability.
//
// It is adapter-internal in practice — every adapter applies it inside its own
// registration path, so a caller hands Router.Handle the named form
// ("/files/*name") and reads Param("name") on all five. Do not register the
// result: for a router without named wildcards it is the anonymous form, which
// ValidateWildcardPath rejects. It stays exported for third-party adapters.
//
// It returns the path to register and the wildcard param key to read from
// Context.Param: both unchanged when path has no wildcard (param "") or the
// router supports named wildcards, otherwise the path rewritten to the
// anonymous form with param "*".
func FixWildcardPathIfNeed(r RouterFeatureProvider, path string) (fixedPath string, param string) {
	param = WildcardParamName(path)
	if param == "" {
		return path, ""
	}

	if r.SupportsRouterFeature(RouterFeatureNamedWildcard) {
		return path, param
	}

	return toAnonymousWildcardPath(path), "*"
}

func toAnonymousWildcardPath(path string) string {
	if path == "" {
		return path
	}

	var b strings.Builder
	b.Grow(len(path))

	for i := 0; i < len(path); {
		if path[i] != '*' {
			b.WriteByte(path[i])
			i++
			continue
		}

		b.WriteByte('*')
		i++
		for i < len(path) && path[i] != '/' {
			i++
		}
	}

	return b.String()
}

// WildcardParamName returns the name of the first wildcard parameter in path,
// "*" for an anonymous wildcard, or "" when path contains no wildcard.
// Adapters use it to remember the original name when rewriting named
// wildcards for routers that only support the anonymous form.
func WildcardParamName(path string) string {
	for i := 0; i < len(path); i++ {
		if path[i] != '*' {
			continue
		}
		start := i + 1
		end := start
		for end < len(path) && path[end] != '/' {
			end++
		}
		if start == end {
			return "*"
		}
		return path[start:end]
	}
	return ""
}
