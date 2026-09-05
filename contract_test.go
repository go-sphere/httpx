package httpx

import (
	"net"
	"testing"
)

func TestValidateWildcardPath(t *testing.T) {
	t.Parallel()
	valid := []string{"", "/", "/files", "/files/*filepath", "/files/*", "/a/b/*rest"}
	for _, path := range valid {
		if err := ValidateWildcardPath(path); err != nil {
			t.Fatalf("ValidateWildcardPath(%q) = %v, want nil", path, err)
		}
	}
	invalid := []string{"/a/*x/b/*y", "/foo*bar", "/a/*x/tail", "*root", "/a/*x*y"}
	for _, path := range invalid {
		if err := ValidateWildcardPath(path); err == nil {
			t.Fatalf("ValidateWildcardPath(%q) = nil, want error", path)
		}
	}
}

func TestParseCIDRs(t *testing.T) {
	t.Parallel()
	nets, err := ParseCIDRs([]string{"10.0.0.0/8", "192.0.2.1", "::1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 3 {
		t.Fatalf("got %d networks, want 3", len(nets))
	}
	cases := map[string]bool{
		"10.1.2.3":  true,
		"192.0.2.1": true,
		"192.0.2.2": false,
		"::1":       true,
	}
	for ipStr, want := range cases {
		contained := false
		for _, n := range nets {
			if n.Contains(mustParseIP(t, ipStr)) {
				contained = true
				break
			}
		}
		if contained != want {
			t.Fatalf("%s contained = %v, want %v", ipStr, contained, want)
		}
	}
	if _, err := ParseCIDRs([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error for invalid entry")
	}
	if nets, err := ParseCIDRs(nil); err != nil || nets != nil {
		t.Fatalf("nil input: got %v, %v", nets, err)
	}
}

func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("bad test IP %q", s)
	}
	return ip
}
