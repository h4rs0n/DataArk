package discovery

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

type robotsCacheEntry struct {
	robots    *robotstxt.RobotsData
	status    string
	expiresAt time.Time
}

type RobotsCache struct {
	mu     sync.Mutex
	items  map[string]robotsCacheEntry
	clock  Clock
	ttl    time.Duration
	loader HTTPFetcher
}

func NewRobotsCache(clock Clock, ttl time.Duration, loader HTTPFetcher) *RobotsCache {
	if clock == nil {
		clock = SystemClock{}
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &RobotsCache{items: make(map[string]robotsCacheEntry), clock: clock, ttl: ttl, loader: loader}
}

func (cache *RobotsCache) Allowed(ctx context.Context, rawURL string, userAgent string) (bool, string, error) {
	target, err := neturl.Parse(rawURL)
	if err != nil {
		return false, "invalid", err
	}
	origin := target.Scheme + "://" + target.Host
	cache.mu.Lock()
	entry, found := cache.items[origin]
	if found && cache.clock.Now().Before(entry.expiresAt) {
		cache.mu.Unlock()
		return robotsEntryAllows(entry, target, userAgent), entry.status, nil
	}
	cache.mu.Unlock()

	if cache.loader == nil {
		return false, "unavailable", ErrRobotsUnavailable
	}
	result, err := cache.loader.Fetch(ctx, FetchRequest{URL: origin + "/robots.txt", Kind: FetchKindRobots, MaxBytes: 512 << 10})
	if err != nil {
		return false, "unavailable", fmt.Errorf("%w: %v", ErrRobotsUnavailable, err)
	}
	entry = robotsCacheEntry{status: "allowed", expiresAt: cache.clock.Now().Add(cache.ttl)}
	switch result.StatusCode {
	case http.StatusOK:
		entry.robots, err = robotstxt.FromBytes(result.Body)
		if err != nil {
			return false, "invalid", fmt.Errorf("%w: invalid robots.txt: %v", ErrRobotsUnavailable, err)
		}
	case http.StatusNotFound, http.StatusGone:
		entry.status = "missing"
	default:
		return false, "unavailable", ErrRobotsUnavailable
	}
	cache.mu.Lock()
	cache.items[origin] = entry
	cache.mu.Unlock()
	return robotsEntryAllows(entry, target, userAgent), entry.status, nil
}

func robotsEntryAllows(entry robotsCacheEntry, target *neturl.URL, userAgent string) bool {
	if entry.robots == nil {
		return true
	}
	return entry.robots.FindGroup(userAgent).Test(target.RequestURI())
}
