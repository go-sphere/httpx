package httpx

import "testing"

type mockRouterFeatureProvider struct {
	namedWildcard bool
}

func (m mockRouterFeatureProvider) SupportsRouterFeature(feature RouterFeature) bool {
	return feature == RouterFeatureNamedWildcard && m.namedWildcard
}

func TestFixWildcardPathIfNeed(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		supports  bool
		wantPath  string
		wantParam string
	}{
		{
			name:      "no wildcard path keeps original",
			path:      "/users/:id",
			supports:  false,
			wantPath:  "/users/:id",
			wantParam: "",
		},
		{
			name:      "named wildcard with support keeps original and returns name",
			path:      "/files/*filepath",
			supports:  true,
			wantPath:  "/files/*filepath",
			wantParam: "filepath",
		},
		{
			name:      "named wildcard without support converts to anonymous",
			path:      "/files/*filepath",
			supports:  false,
			wantPath:  "/files/*",
			wantParam: "*",
		},
		{
			name:      "anonymous wildcard remains anonymous",
			path:      "/files/*",
			supports:  false,
			wantPath:  "/files/*",
			wantParam: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotParam := FixWildcardPathIfNeed(
				mockRouterFeatureProvider{namedWildcard: tt.supports},
				tt.path,
			)

			if gotPath != tt.wantPath {
				t.Fatalf("path mismatch: got %q, want %q", gotPath, tt.wantPath)
			}
			if gotParam != tt.wantParam {
				t.Fatalf("param mismatch: got %q, want %q", gotParam, tt.wantParam)
			}
		})
	}
}

func TestToAnonymousWildcardPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty path", in: "", want: ""},
		{name: "no wildcard", in: "/health", want: "/health"},
		{name: "named wildcard", in: "/files/*filepath", want: "/files/*"},
		{name: "anonymous wildcard", in: "/files/*", want: "/files/*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toAnonymousWildcardPath(tt.in)
			if got != tt.want {
				t.Fatalf("toAnonymousWildcardPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWildcardParamName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no wildcard", in: "/users/:id", want: ""},
		{name: "named wildcard", in: "/files/*filepath", want: "filepath"},
		{name: "anonymous wildcard", in: "/files/*", want: "*"},
		// Current implementation only extracts the first wildcard when multiple are present.
		// This does not mean multiple wildcard params are supported by routers.
		{name: "multiple wildcards uses first one", in: "/a/*x/b/*y", want: "x"},
		{name: "wildcard in middle segment", in: "/a/*name/detail", want: "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wildcardParamName(tt.in)
			if got != tt.want {
				t.Fatalf("wildcardParamName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
