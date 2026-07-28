// Package httpegress gates lab outbound HTTP (Pub/Sub push, Eventarc, Tasks, Scheduler).
// Default deny: only the lab HTTP catcher on loopback :4588, or other loopback :4588
// lab-local URLs. Open-internet delivery requires NOCTAXRIS_GCP_HTTP_EGRESS=1 plus an
// exact URL allowlist; private/metadata/loopback hosts never pass the allowlist path.
package httpegress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// EnvHTTPEgress enables honor of NOCTAXRIS_GCP_HTTP_ALLOWLIST for non-lab-local URLs.
const EnvHTTPEgress = "NOCTAXRIS_GCP_HTTP_EGRESS"

// EnvHTTPAllowlist is comma-separated exact HTTP(S) URLs allowed when egress is on.
const EnvHTTPAllowlist = "NOCTAXRIS_GCP_HTTP_ALLOWLIST"

// LabHTTPCatcherPath is the in-process catcher used by unit tests and local hooks.
const LabHTTPCatcherPath = "/_noctaxris-gcp/http-catcher"

// LabListenPort is the default API port used for lab-local delivery checks.
const LabListenPort = "4588"

// ErrNotAllowed is returned when a destination fails the egress gate.
var ErrNotAllowed = errors.New("http egress: destination not allowed")

// Validate checks endpoint before create/update or outbound delivery.
func Validate(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: invalid HTTP endpoint", ErrNotAllowed)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: protocol must be http or https", ErrNotAllowed)
	}
	if IsLabCatcher(u, scheme) || IsLabLocal(u, scheme) {
		return nil
	}
	if !egressEnabled() || !allowlisted(endpoint) {
		return ErrNotAllowed
	}
	if err := rejectUnsafeHost(u.Hostname()); err != nil {
		return err
	}
	return nil
}

// IsLabCatcher reports whether u is the loopback catcher on the lab API port.
func IsLabCatcher(u *url.URL, scheme string) bool {
	if !isLoopbackHost(u.Hostname()) {
		return false
	}
	if portOrDefault(u, scheme) != LabListenPort {
		return false
	}
	path := u.Path
	return path == LabHTTPCatcherPath || strings.HasPrefix(path, LabHTTPCatcherPath+"/")
}

// IsLabLocal reports loopback delivery to the lab API port (self-invoke theatre).
func IsLabLocal(u *url.URL, scheme string) bool {
	if !isLoopbackHost(u.Hostname()) {
		return false
	}
	return portOrDefault(u, scheme) == LabListenPort
}

func egressEnabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvHTTPEgress))
	return v == "1" || strings.EqualFold(v, "true")
}

func allowlisted(endpoint string) bool {
	raw := strings.TrimSpace(os.Getenv(EnvHTTPAllowlist))
	if raw == "" {
		return false
	}
	want := strings.TrimSpace(endpoint)
	for _, entry := range strings.Split(raw, ",") {
		if strings.TrimSpace(entry) == want {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	switch lower {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func portOrDefault(u *url.URL, scheme string) string {
	port := u.Port()
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func rejectUnsafeHost(host string) error {
	host = strings.TrimSpace(host)
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return ErrNotAllowed
	}
	if lower == "metadata.google.internal" || strings.Contains(lower, "metadata") {
		return ErrNotAllowed
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		if ip := net.ParseIP(host); ip != nil {
			return rejectUnsafeIP(ip)
		}
		return fmt.Errorf("%w: resolve HTTP endpoint host: %v", ErrNotAllowed, err)
	}
	if len(ips) == 0 {
		return ErrNotAllowed
	}
	for _, ip := range ips {
		if err := rejectUnsafeIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func rejectUnsafeIP(ip net.IP) error {
	if ip == nil {
		return ErrNotAllowed
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return ErrNotAllowed
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return ErrNotAllowed
	}
	return nil
}

// Client returns an HTTP client that denies redirects and dials only after IP safety checks.
func Client(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	var transport *http.Transport
	if ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.DialContext = PinnedDialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("http egress: redirects are not allowed")
		},
	}
}

// PinnedDialContext resolves addr, rejects unsafe IPs, and dials a validated address.
func PinnedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("http egress: dial addr: %w", err)
	}
	if isLoopbackHost(host) && port == LabListenPort {
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve dial host: %v", ErrNotAllowed, err)
	}
	if len(ips) == 0 {
		return nil, ErrNotAllowed
	}
	var last error
	var d net.Dialer
	for _, ip := range ips {
		if err := rejectUnsafeIP(ip); err != nil {
			last = err
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = ErrNotAllowed
	}
	return nil, last
}
