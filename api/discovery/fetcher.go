package discovery

import (
	"DataArk/config"
	"context"
	"errors"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

var ErrInvalidSOCKS5Proxy = errors.New("invalid discovery SOCKS5 proxy configuration")

type errorHTTPFetcher struct{ err error }

func (fetcher errorHTTPFetcher) Fetch(context.Context, FetchRequest) (FetchResult, error) {
	return FetchResult{}, fetcher.err
}

var configuredFetcher struct {
	sync.Once
	value HTTPFetcher
}

func ConfiguredHTTPFetcher() HTTPFetcher {
	configuredFetcher.Do(func() {
		clock := SystemClock{}
		timeout := configuredDuration(config.DISCOVERYREQUESTTIMEOUT, 12*time.Second)
		minimumInterval := configuredDuration(config.DISCOVERYMINREQUESTINTERVAL, time.Second)
		robotsTTL := configuredDuration(config.DISCOVERYROBOTSCACHETTL, 6*time.Hour)
		client, err := newDiscoveryHTTPClient(config.DISCOVERYSOCKS5PROXY, timeout)
		if err != nil {
			configuredFetcher.value = errorHTTPFetcher{err: err}
			return
		}
		limiter := NewHostLimiter(config.DISCOVERYHOSTCONCURRENCY, minimumInterval, clock, nil)
		raw := &HTTPClientFetcher{
			Client:       client,
			Clock:        clock,
			Limiter:      limiter,
			UserAgent:    config.DISCOVERYUSERAGENT,
			MaxRedirects: config.DISCOVERYMAXREDIRECTS,
			BlockURL:     ensureDiscoveryURLNotBlacklisted,
		}
		robots := NewRobotsCache(clock, robotsTTL, raw)
		configuredFetcher.value = &HTTPClientFetcher{
			Client:       client,
			Clock:        clock,
			Limiter:      limiter,
			Robots:       robots,
			UserAgent:    config.DISCOVERYUSERAGENT,
			MaxRedirects: config.DISCOVERYMAXREDIRECTS,
			BlockURL:     ensureDiscoveryURLNotBlacklisted,
		}
	})
	return configuredFetcher.value
}

func newDiscoveryHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, ErrInvalidSOCKS5Proxy
	}
	transport := base.Clone()
	transport.Proxy = nil

	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return &http.Client{Transport: transport, Timeout: timeout}, nil
	}
	parsed, err := neturl.Parse(proxyURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "socks5") || parsed.Hostname() == "" || parsed.Port() == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidSOCKS5Proxy
	}
	forward := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	dialer, err := xproxy.FromURL(parsed, forward)
	if err != nil {
		return nil, ErrInvalidSOCKS5Proxy
	}
	contextDialer, ok := dialer.(xproxy.ContextDialer)
	if !ok {
		return nil, ErrInvalidSOCKS5Proxy
	}
	transport.DialContext = contextDialer.DialContext
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func configuredDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
