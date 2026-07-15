package discovery

import (
	"errors"
	"net"
	neturl "net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

func domainKeyForURL(rawURL string) (string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" {
		return "", errors.New("missing site host")
	}
	hostname = strings.TrimPrefix(hostname, "www.")
	key := hostname
	if net.ParseIP(hostname) == nil && hostname != "localhost" {
		if registrable, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil {
			key = strings.ToLower(registrable)
		}
	}
	if port := parsed.Port(); port != "" && (net.ParseIP(hostname) != nil || hostname == "localhost") && !isDefaultWebPort(parsed.Scheme, port) {
		key += ":" + port
	}
	return key, nil
}

func isDefaultWebPort(scheme string, port string) bool {
	return (strings.EqualFold(scheme, "http") && port == "80") || (strings.EqualFold(scheme, "https") && port == "443")
}

func siteDomainKey(site DiscoverySite) string {
	if value := strings.TrimSpace(site.DomainKey); value != "" {
		return value
	}
	if value, err := domainKeyForURL(site.RootURL); err == nil {
		return value
	}
	return strings.TrimSpace(site.HostKey)
}
