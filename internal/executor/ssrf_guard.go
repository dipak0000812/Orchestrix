package executor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// ErrBlockedDestination is wrapped into a PermanentError whenever a
// webhook target resolves to a disallowed destination -- retrying the
// same URL will never succeed, so it must not consume retry attempts.
type ErrBlockedDestination struct {
	Host string
	IP   net.IP
}

func (e *ErrBlockedDestination) Error() string {
	if e.IP != nil {
		return fmt.Sprintf("destination %s (resolved to %s) is not allowed", e.Host, e.IP)
	}
	return fmt.Sprintf("destination %s is not allowed", e.Host)
}

// allowedWebhookSchemes restricts outbound webhook requests to plain
// HTTP/HTTPS. Without this, a payload could specify file://, gopher://,
// or other schemes some HTTP client stacks still special-case.
var allowedWebhookSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// validateWebhookURL performs the scheme/structural checks that can be
// done on the URL string alone, before any network activity. This is NOT
// sufficient on its own to prevent SSRF (see safeDialContext for the part
// that actually matters) -- it only rejects obviously-wrong input early.
func validateWebhookURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !allowedWebhookSchemes[u.Scheme] {
		return nil, fmt.Errorf("scheme %q is not allowed (only http/https)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	return u, nil
}

// isBlockedIP reports whether ip falls in a range that must never be
// reachable from a webhook job: loopback, RFC1918/ULA private ranges,
// link-local (this is what covers cloud metadata endpoints like
// 169.254.169.254), and unspecified/multicast addresses. Go's net.IP
// range-check methods correctly handle IPv4-mapped IPv6 addresses
// (e.g. ::ffff:127.0.0.1), so this isn't bypassable by that encoding.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// safeDialContext is installed as the Transport's DialContext for the
// webhook executor's HTTP client. It resolves the target hostname itself,
// rejects any resolved address that is a blocked destination, and then
// dials the *specific validated IP* rather than the original hostname --
// dialing the hostname again here would re-resolve it, reopening exactly
// the DNS-rebinding gap this exists to close (validate one IP, then
// silently connect to a different one the second lookup returns).
//
// This function is applied to every connection the transport makes,
// including ones opened to follow a redirect, so a 30x response pointing
// at an internal address is caught the same way as a direct request to
// one.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}

	var chosen net.IP
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, &ErrBlockedDestination{Host: host, IP: ip}
		}
		if chosen == nil {
			chosen = ip
		}
	}

	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
}

// newSSRFSafeTransport returns an http.Transport whose DialContext routes
// through safeDialContext. Callers should build the webhook executor's
// http.Client around a transport from this function rather than
// http.DefaultTransport.
func newSSRFSafeTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeDialContext
	return transport
}
