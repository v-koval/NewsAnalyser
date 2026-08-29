package images

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// allowedURL permits only http/https URLs. Image URLs come from the agent
// response, so every other scheme (file:, data:, ftp:, ...) is rejected.
func allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed", u.Scheme)
	}
	return nil
}

// isPublicIP reports whether ip is a routable public address. Loopback,
// private, link-local, multicast and unspecified addresses are rejected to
// prevent SSRF via agent-supplied URLs.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return true
}

// newSafeClient returns an http.Client that resolves every host itself and
// refuses to connect to non-public addresses. The check runs per connection,
// so redirects are covered too.
func newSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !isPublicIP(ip.IP) {
					return nil, fmt.Errorf("blocked non-public address %s for host %s", ip.IP, host)
				}
			}
			// Dial the address we just validated, not the hostname, so a
			// second DNS resolution cannot return a different IP.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}
