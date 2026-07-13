package discovery

import (
	"DataArk/config"
	"net/http"
	"sync"
	"time"
)

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
		client := &http.Client{Timeout: timeout}
		limiter := NewHostLimiter(config.DISCOVERYHOSTCONCURRENCY, minimumInterval, clock, nil)
		raw := &HTTPClientFetcher{
			Client:       client,
			Clock:        clock,
			Limiter:      limiter,
			UserAgent:    config.DISCOVERYUSERAGENT,
			MaxRedirects: config.DISCOVERYMAXREDIRECTS,
		}
		robots := NewRobotsCache(clock, robotsTTL, raw)
		configuredFetcher.value = &HTTPClientFetcher{
			Client:       client,
			Clock:        clock,
			Limiter:      limiter,
			Robots:       robots,
			UserAgent:    config.DISCOVERYUSERAGENT,
			MaxRedirects: config.DISCOVERYMAXREDIRECTS,
		}
	})
	return configuredFetcher.value
}

func configuredDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
