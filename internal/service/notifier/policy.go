package notifier

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Policy struct {
	AllowedHosts []string
	Development  bool
}

func (p Policy) allows(host string) bool {
	for _, allowed := range p.AllowedHosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
	}
	return false
}
func (p Policy) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !p.allows(u.Hostname()) {
		return fmt.Errorf("webhook destination is not allowed")
	}
	if p.Development {
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("invalid scheme")
		}
	} else if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("production webhook requires HTTPS port 443")
	}
	return nil
}

var deniedPrefixes = []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range deniedPrefixes {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
func NewWithPolicy(timeout time.Duration, policy Policy, secrets SecretResolver) *WebhookExecutor {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, MaxIdleConns: 32, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !policy.allows(host) {
			return nil, fmt.Errorf("blocked webhook host")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("webhook DNS lookup failed")
		}
		for _, ip := range ips {
			if !policy.Development && !publicAddress(ip) {
				return nil, fmt.Errorf("blocked webhook address")
			}
		}
		// Pin actual socket destinations to the checked addresses, preventing DNS rebinding.
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &WebhookExecutor{client: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, policy: policy, secrets: secrets}
}

// Explicit test/Compose exception; never selected by the production constructor.
func NewDevelopmentWebhookExecutor(timeout time.Duration, hosts ...string) *WebhookExecutor {
	return NewWithPolicy(timeout, Policy{AllowedHosts: hosts, Development: true}, nil)
}

type SecretResolver interface {
	Resolve(context.Context, uint64, string) ([]byte, error)
}
type DirectorySecrets struct{ Root string }

var secretRef = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (d DirectorySecrets) Resolve(ctx context.Context, tenantID uint64, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == 0 || !secretRef.MatchString(ref) || d.Root == "" {
		return nil, fmt.Errorf("invalid secret reference")
	}
	root, err := filepath.EvalSymlinks(d.Root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	tenantRoot := filepath.Join(root, strconv.FormatUint(tenantID, 10))
	resolved, err := filepath.EvalSymlinks(tenantRoot)
	if err != nil || resolved != tenantRoot {
		return nil, fmt.Errorf("tenant directory cannot be a symlink")
	}
	// Root confines symlink resolution and file opening in one operation (no check/open race).
	confined, err := os.OpenRoot(tenantRoot)
	if err != nil {
		return nil, err
	}
	defer confined.Close()
	file, err := confined.Open(ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid secret file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) < 32 || len(data) > 4096 {
		return nil, fmt.Errorf("invalid secret file")
	}
	return data, nil
}
