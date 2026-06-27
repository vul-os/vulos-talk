package bots

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// EnvAllowPrivateWebhooks, when truthy ("1"/"true"/"yes"/"on"), disables the
// SSRF guard on bot event_url validation and dispatch. It exists for self-host
// deployments that legitimately target a bot service on a private/loopback
// address (same LAN / same host). Default OFF — the guard is on by default.
const EnvAllowPrivateWebhooks = "VULOS_TALK_ALLOW_PRIVATE_WEBHOOKS"

// allowPrivateWebhooks reports whether the operator opted out of the SSRF guard.
func allowPrivateWebhooks() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAllowPrivateWebhooks))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ValidateEventURL guards a bot's event_url against blind SSRF. It enforces an
// http/https scheme allowlist and rejects URLs whose host is (or resolves to) a
// private, loopback, link-local, unspecified, multicast, or cloud-metadata
// address (RFC1918, 127/8, ::1, 169.254/16 incl. 169.254.169.254, fc00::/7,
// fe80::/10, …).
//
// An empty url is allowed — event_url is optional. The guard is bypassed when
// VULOS_TALK_ALLOW_PRIVATE_WEBHOOKS is set (self-host opt-out).
func ValidateEventURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil // optional field
	}
	if allowPrivateWebhooks() {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid event_url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("event_url scheme %q not allowed (use http or https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("event_url has no host")
	}

	// Literal IP → check directly. Hostname → resolve and reject if ANY resolved
	// address is disallowed (defends against DNS that returns a private address).
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, rerr := net.LookupIP(host)
		if rerr != nil {
			return fmt.Errorf("event_url host resolution failed: %w", rerr)
		}
		ips = resolved
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("event_url host resolves to a disallowed address (%s)", ip)
		}
	}
	return nil
}

// isDisallowedIP reports whether ip falls in a range we must never let a bot
// event_url target: loopback, link-local (uni/multicast), generic multicast,
// the unspecified address, or any private (RFC1918 / fc00::/7) range. The
// canonical IMDS address 169.254.169.254 is already link-local but is also
// guarded explicitly for clarity.
func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	return false
}
