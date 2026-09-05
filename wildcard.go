package httpx

import (
	"fmt"
	"strings"
)

// ValidateWildcardPath reports whether path uses wildcards in the only form
// every adapter supports: at most one wildcard, starting its own path segment
// ("/files/*name"), as the final segment. Other shapes ("/a/*x/b/*y",
// "/foo*bar") panic at registration on gin/hertz but would silently register
// a semantically different route on fiber; adapters call this to fail loudly
// and identically at registration time.
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
	return nil
}

// FixWildcardPathIfNeed normalizes wildcard path syntax based on router capability.
//
// It returns:
//   - path: the path to register
//   - param: the wildcard param key to read from Context.Param
//
// Rules:
//   - If path has no wildcard, param is "" and path is returned unchanged.
//   - If router supports named wildcards, path is returned unchanged and param is wildcard name.
//   - If router does not support named wildcards, named wildcard segments are rewritten to "*"
//     and param is "*".
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
