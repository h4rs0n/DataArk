package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"strconv"
	"strings"
)

var (
	ErrUnsafeURLScheme = errors.New("only http and https urls are supported")
	ErrUnsafeURLHost   = errors.New("url host is required")
	ErrUnsafeURLPort   = errors.New("url port is not allowed")
	ErrUnsafeIPAddress = errors.New("url resolves to an unsafe address")
)

var allowedFetchPorts = map[string]struct{}{
	"":     {},
	"80":   {},
	"443":  {},
	"8080": {},
	"8443": {},
}

func ValidateFetchURL(ctx context.Context, rawURL string) (*neturl.URL, error) {
	parsedURL, err := parseFetchURL(rawURL)
	if err != nil {
		return nil, err
	}
	ips, err := resolveHost(ctx, parsedURL.Hostname())
	if err != nil {
		return nil, err
	}
	if err := ValidateResolvedFetchURL(parsedURL, ips); err != nil {
		return nil, err
	}
	return parsedURL, nil
}

func ValidateResolvedFetchURL(parsedURL *neturl.URL, ips []net.IP) error {
	if parsedURL == nil {
		return ErrUnsafeURLHost
	}
	if err := validateFetchURLParts(parsedURL); err != nil {
		return err
	}
	if len(ips) == 0 {
		return ErrUnsafeIPAddress
	}
	for _, ip := range ips {
		if isUnsafeIP(ip) {
			return fmt.Errorf("%w: %s", ErrUnsafeIPAddress, ip.String())
		}
	}
	return nil
}

func parseFetchURL(rawURL string) (*neturl.URL, error) {
	parsedURL, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if err := validateFetchURLParts(parsedURL); err != nil {
		return nil, err
	}
	return parsedURL, nil
}

func validateFetchURLParts(parsedURL *neturl.URL) error {
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return ErrUnsafeURLScheme
	}
	if strings.TrimSpace(parsedURL.Hostname()) == "" {
		return ErrUnsafeURLHost
	}
	if _, err := strconv.Atoi(parsedURL.Port()); parsedURL.Port() != "" && err != nil {
		return ErrUnsafeURLPort
	}
	if _, ok := allowedFetchPorts[parsedURL.Port()]; !ok {
		return ErrUnsafeURLPort
	}
	return nil
}

func resolveHost(ctx context.Context, host string) ([]net.IP, error) {
	if parsedIP := net.ParseIP(host); parsedIP != nil {
		return []net.IP{parsedIP}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

func isUnsafeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}
