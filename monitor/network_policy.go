package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ayushanandhere/GoProbe/config"
)

type targetDialer struct {
	target config.Target
	dialer net.Dialer
}

func newTargetDialer(target config.Target) targetDialer {
	return targetDialer{
		target: target,
		dialer: net.Dialer{
			Timeout: target.Timeout,
		},
	}
}

func (d targetDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.target.Trusted {
		return d.dialer.DialContext(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := d.resolveAllowedIPs(ctx, host)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ip := range ips {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("no allowed IP addresses resolved")
	}
	return nil, lastErr
}

func (d targetDialer) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after too many redirects")
	}
	if d.target.Trusted {
		return nil
	}
	return validateRedirectTarget(req.URL)
}

func (d targetDialer) resolveAllowedIPs(ctx context.Context, host string) ([]net.IP, error) {
	normalizedHost := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if strings.EqualFold(normalizedHost, "localhost") {
		return nil, errors.New("localhost targets are not allowed for runtime-created targets")
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if !allowedRuntimeIP(ip) {
			return nil, errors.New("private, loopback, link-local, and special-use IPs are not allowed for runtime-created targets")
		}
		return []net.IP{ip}, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, d.target.Timeout)
	defer cancel()

	resolved, err := net.DefaultResolver.LookupIPAddr(lookupCtx, normalizedHost)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(resolved) == 0 {
		return nil, errors.New("resolve host: no addresses found")
	}

	allowed := make([]net.IP, 0, len(resolved))
	for _, resolvedAddr := range resolved {
		if !allowedRuntimeIP(resolvedAddr.IP) {
			return nil, errors.New("private, loopback, link-local, and special-use IPs are not allowed for runtime-created targets")
		}
		allowed = append(allowed, resolvedAddr.IP)
	}
	return allowed, nil
}

func validateRedirectTarget(targetURL *url.URL) error {
	if targetURL == nil {
		return errors.New("redirect target is missing")
	}
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		return errors.New("redirect target must use http or https")
	}
	if targetURL.Host == "" || targetURL.Hostname() == "" {
		return errors.New("redirect target must include a host")
	}
	if targetURL.User != nil {
		return errors.New("redirect target must not include user info")
	}
	if targetURL.Fragment != "" {
		return errors.New("redirect target must not include a fragment")
	}
	return validateUntrustedRedirectHost(targetURL.Hostname())
}

func validateUntrustedRedirectHost(host string) error {
	normalizedHost := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if strings.EqualFold(normalizedHost, "localhost") {
		return errors.New("localhost targets are not allowed for runtime-created targets")
	}
	if ip := net.ParseIP(normalizedHost); ip != nil && !allowedRuntimeIP(ip) {
		return errors.New("private, loopback, link-local, and special-use IPs are not allowed for runtime-created targets")
	}
	return nil
}

func allowedRuntimeIP(ip net.IP) bool {
	for _, network := range blockedRuntimeNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedRuntimeNetworks = mustParseRuntimeCIDRs(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"fe80::/10",
	"fc00::/7",
	"ff00::/8",
	"2001:db8::/32",
)

func mustParseRuntimeCIDRs(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}
