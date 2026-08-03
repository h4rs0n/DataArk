package discovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"testing"
	"time"
)

func TestNormalizeDiscoveryBlacklistDomainAndMatchBoundaries(t *testing.T) {
	normalized, err := NormalizeDiscoveryBlacklistDomain(" BÜCHER.Example. ")
	if err != nil || normalized != "xn--bcher-kva.example" {
		t.Fatalf("normalized = %q, %v", normalized, err)
	}
	for _, invalid := range []string{"", "https://example.com", "*.example.com", "localhost", "127.0.0.1", "example"} {
		if _, err := NormalizeDiscoveryBlacklistDomain(invalid); !errors.Is(err, ErrInvalidDiscoveryBlacklistDomain) {
			t.Fatalf("%q error = %v", invalid, err)
		}
	}
	if !domainRuleMatchesHost("example.com", "blog.example.com") || !domainRuleMatchesHost("blog.example.com", "blog.example.com") {
		t.Fatal("expected exact and child hosts to match")
	}
	if domainRuleMatchesHost("example.com", "notexample.com") || domainRuleMatchesHost("blog.example.com", "example.com") {
		t.Fatal("host matching crossed a DNS label boundary")
	}
}

func TestDomainBlacklistReversiblyBlocksPendingCandidates(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	source := DiscoverySource{Name: "CSDN", URL: "https://feed.example.com/rss", CrawlHost: "feed.example.com", Type: DiscoverySourceTypeFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://blog.csdn.net/post/1", CrawlHost: "blog.csdn.net", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now}
	ready := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://blog.csdn.net/post/ready", CrawlHost: "blog.csdn.net", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady, EligibilityState: DiscoveryEligibilityEligible, LastSeenAt: now}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ready).Error; err != nil {
		t.Fatal(err)
	}
	broad, err := CreateDiscoveryDomainBlacklist("CSDN.NET", "fixture")
	if err != nil || broad.AffectedCandidates != 2 {
		t.Fatalf("broad mutation = %#v, %v", broad, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingDomainBlocked || candidate.ProcessingErrorType != processingErrorDomainBlacklist || candidate.NextProcessingAt != nil {
		t.Fatalf("blocked candidate = %#v", candidate)
	}
	if err := db.First(&ready, ready.ID).Error; err != nil || ready.ProcessingState != DiscoveryProcessingReady {
		t.Fatalf("ready candidate changed = %#v, %v", ready, err)
	}
	visible, err := ListDiscoveryCandidatesForUser(1, DiscoveryCandidateStatusNew, 10)
	if err != nil || len(visible) != 0 {
		t.Fatalf("blacklisted retained candidates remain visible: %#v, %v", visible, err)
	}
	narrow, err := CreateDiscoveryDomainBlacklist("blog.csdn.net", "overlap")
	if err != nil || narrow.AffectedCandidates != 0 {
		t.Fatalf("overlapping mutation = %#v, %v", narrow, err)
	}
	if _, err := DeleteDiscoveryDomainBlacklist(broad.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil || candidate.ProcessingState != DiscoveryProcessingDomainBlocked {
		t.Fatalf("overlapping rule did not retain block = %#v, %v", candidate, err)
	}
	removed, err := DeleteDiscoveryDomainBlacklist(narrow.Entry.ID)
	if err != nil || removed.AffectedCandidates != 2 {
		t.Fatalf("remove mutation = %#v, %v", removed, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingFetchPending || candidate.NextProcessingAt == nil || candidate.ProcessingErrorType != "" {
		t.Fatalf("resumed candidate = %#v", candidate)
	}
	visible, err = ListDiscoveryCandidatesForUser(1, DiscoveryCandidateStatusNew, 10)
	if err != nil || len(visible) != 2 {
		t.Fatalf("retained candidates were not restored: %#v, %v", visible, err)
	}
}

func TestHTTPFetcherBlocksBeforeValidationAndBeforeRedirect(t *testing.T) {
	blockedErr := errors.New("blocked fixture host")
	validatorCalls := 0
	validator := func(_ context.Context, rawURL string) (*neturl.URL, error) {
		validatorCalls++
		return neturl.Parse(rawURL)
	}
	blocker := func(_ context.Context, rawURL string) error {
		parsed, _ := neturl.Parse(rawURL)
		if parsed.Host == "blocked.example" || parsed.Path == "/blocked" {
			return blockedErr
		}
		return nil
	}
	fetcher := HTTPClientFetcher{Validator: validator, BlockURL: blocker}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: "https://blocked.example/article", Kind: FetchKindArticle}); !errors.Is(err, blockedErr) {
		t.Fatalf("initial block error = %v", err)
	}
	if validatorCalls != 0 {
		t.Fatalf("validator called %d times before initial block", validatorCalls)
	}

	var blockedRequests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/blocked", http.StatusFound)
			return
		}
		blockedRequests++
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("unexpected"))
	}))
	t.Cleanup(server.Close)
	fetcher = HTTPClientFetcher{Client: server.Client(), Validator: allowTestURL, BlockURL: blocker}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/start", Kind: FetchKindHTML}); !errors.Is(err, blockedErr) {
		t.Fatalf("redirect block error = %v", err)
	}
	if blockedRequests != 0 {
		t.Fatalf("redirect target received %d requests", blockedRequests)
	}
}

func TestBlacklistedSourceCannotBeCreated(t *testing.T) {
	setupSQLiteDB(t)
	if _, err := CreateDiscoveryDomainBlacklist("example.com", "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDiscoverySource("Blocked", "https://feed.example.com/rss", DiscoverySourceTypeFeed, true); !errors.Is(err, ErrDiscoveryDomainBlacklisted) {
		t.Fatalf("create source error = %v", err)
	}
}
