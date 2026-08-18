package discovery

import (
	"context"
	"net/http"
	neturl "net/url"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

type robotsCacheEntry struct {
	inspection RobotsInspection
	expiresAt  time.Time
}

// RobotsInspection 只记录 robots.txt 是否可解析。不再消费 Sitemap 提示；
// Allow、Disallow 与 Crawl-delay 从不拦截 owner 触发的抓取。
type RobotsInspection struct {
	Status   string
	Sitemaps []string
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

func (cache *RobotsCache) Inspect(ctx context.Context, rawURL string) RobotsInspection {
	target, err := neturl.Parse(rawURL)
	if err != nil {
		return RobotsInspection{Status: "invalid"}
	}
	origin := target.Scheme + "://" + target.Host
	cache.mu.Lock()
	entry, found := cache.items[origin]
	if found && cache.clock.Now().Before(entry.expiresAt) {
		cache.mu.Unlock()
		return cloneRobotsInspection(entry.inspection)
	}
	cache.mu.Unlock()

	inspection := RobotsInspection{Status: "unavailable"}
	if cache.loader == nil {
		return cache.store(origin, inspection)
	}
	result, err := cache.loader.Fetch(ctx, FetchRequest{URL: origin + "/robots.txt", Kind: FetchKindRobots, MaxBytes: 512 << 10})
	if err != nil {
		return cache.store(origin, inspection)
	}
	switch result.StatusCode {
	case http.StatusOK:
		_, parseErr := robotstxt.FromBytes(result.Body)
		inspection.Status = "available"
		if parseErr != nil {
			inspection.Status = "invalid"
		}
	case http.StatusNotFound, http.StatusGone:
		inspection.Status = "missing"
	default:
		inspection.Status = "unavailable"
	}
	return cache.store(origin, inspection)
}

func (cache *RobotsCache) store(origin string, inspection RobotsInspection) RobotsInspection {
	entry := robotsCacheEntry{inspection: cloneRobotsInspection(inspection), expiresAt: cache.clock.Now().Add(cache.ttl)}
	cache.mu.Lock()
	cache.items[origin] = entry
	cache.mu.Unlock()
	return cloneRobotsInspection(inspection)
}

func cloneRobotsInspection(inspection RobotsInspection) RobotsInspection {
	inspection.Sitemaps = append([]string(nil), inspection.Sitemaps...)
	return inspection
}
