package httpx

import (
	"fmt"
	"net"
)

// ParseCIDRs parses trusted-proxy specifications. Each entry may be a CIDR
// ("10.0.0.0/8") or a single IP address ("192.0.2.1"), which is treated as a
// single-host network. It is the shared parser behind the adapters'
// WithTrustedProxies options.
func ParseCIDRs(entries []string) ([]*net.IPNet, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		if _, ipNet, err := net.ParseCIDR(entry); err == nil {
			out = append(out, ipNet)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("httpx: invalid trusted proxy %q: not an IP or CIDR", entry)
		}
		bits := 8 * net.IPv6len
		if v4 := ip.To4(); v4 != nil {
			ip = v4
			bits = 8 * net.IPv4len
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}
