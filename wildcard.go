package httpx

import "strings"

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
	param = wildcardParamName(path)
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

func wildcardParamName(path string) string {
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
