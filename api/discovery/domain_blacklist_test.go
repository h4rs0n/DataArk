package discovery

import (
	"DataArk/material"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"reflect"
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
	if err != nil || broad.AffectedCandidates != 1 {
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
	if err != nil || len(visible) != 2 {
		t.Fatalf("crawl restriction hid retained candidates: %#v, %v", visible, err)
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
	if err != nil || removed.AffectedCandidates != 1 {
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

func TestBlacklistedSourceCanBeSavedButNotFetched(t *testing.T) {
	setupSQLiteDB(t)
	if _, err := CreateDiscoveryDomainBlacklist("example.com", "fixture"); err != nil {
		t.Fatal(err)
	}
	source, err := CreateDiscoverySource("Blocked", "https://feed.example.com/rss", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateDiscoverySource(source.ID, "Updated", "https://other.example.com/rss", DiscoverySourceTypeFeed, true); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&source, source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := FetchDiscoverySource(context.Background(), source); !errors.Is(err, ErrDiscoveryDomainBlacklisted) {
		t.Fatalf("fetch error = %v", err)
	}
}

func TestCrawlRestrictionPreservesContentStateOnInitialAndRedirectBlocks(t *testing.T) {
	for _, retained := range []bool{false, true} {
		for _, redirect := range []bool{false, true} {
			t.Run(fmt.Sprintf("retained=%t/redirect=%t", retained, redirect), func(t *testing.T) {
				setupSQLiteDB(t)
				candidate := DiscoveryCandidate{URL: "https://article.example/post", Title: "Article", ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown}
				if retained {
					candidate = newIndexedCandidate(t, "article", dedupeAutoBody())
					if err := UpdateCandidate(db, candidate.ID, map[string]interface{}{"dedupe_state": DiscoveryDedupeReady, "assessment_state": DiscoveryAssessmentReady, "eligibility_state": DiscoveryEligibilityEligible, "eligibility_reasons": "article_quality_passed"}).Error; err != nil {
						t.Fatal(err)
					}
				} else if err := db.Create(&candidate).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.First(&candidate, candidate.ID).Error; err != nil {
					t.Fatal(err)
				}
				var before material.ArticleState
				if retained {
					if err := db.First(&before, "material_id=?", candidate.MaterialID).Error; err != nil {
						t.Fatal(err)
					}
				}
				oldFetch := fetchDiscoveryRequest
				t.Cleanup(func() { fetchDiscoveryRequest = oldFetch })
				calls := 0
				fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
					calls++
					return FetchResult{}, fmt.Errorf("%w: redirect.example", ErrDiscoveryDomainBlacklisted)
				}
				if !redirect {
					if _, err := CreateDiscoveryDomainBlacklist("article.example", "fixture"); err != nil {
						t.Fatal(err)
					}
				}
				if err := ProcessCandidate(context.Background(), candidate.ID, fmt.Sprint(candidate.ContentVersion)); !errors.Is(err, ErrDiscoveryDomainBlacklisted) {
					t.Fatalf("process error = %v", err)
				}
				var after DiscoveryCandidate
				if err := db.First(&after, candidate.ID).Error; err != nil {
					t.Fatal(err)
				}
				wantState := DiscoveryProcessingDomainBlocked
				if retained {
					wantState = DiscoveryProcessingReady
				}
				if after.ProcessingState != wantState || after.EligibilityState != candidate.EligibilityState || after.EligibilityReasons != candidate.EligibilityReasons || after.NextProcessingAt != nil || after.ProcessingErrorType != processingErrorDomainBlacklist {
					t.Fatalf("restriction changed content: %+v", after)
				}
				if calls != 0 && !redirect || calls != 1 && redirect {
					t.Fatalf("fetch calls = %d", calls)
				}
				if retained {
					var state material.ArticleState
					if err := db.First(&state, "material_id=?", candidate.MaterialID).Error; err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(before, state) || after.BodyText != candidate.BodyText || after.ContentVersion != candidate.ContentVersion || after.DedupeState != candidate.DedupeState {
						t.Fatal("crawl restriction changed retained content or assessment")
					}
				}
			})
		}
	}
}

func TestDeletingRedirectDomainRulesResumesBlockedCandidate(t *testing.T) {
	setupSQLiteDB(t)
	c := DiscoveryCandidate{URL: "https://allowed.example/post", Title: "Article", ProcessingState: DiscoveryProcessingFetchPending}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	broad, err := CreateDiscoveryDomainBlacklist("redirect.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := CreateDiscoveryDomainBlacklist("child.redirect.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	oldFetch := fetchDiscoveryRequest
	t.Cleanup(func() { fetchDiscoveryRequest = oldFetch })
	fetchDiscoveryRequest = func(ctx context.Context, _ FetchRequest) (FetchResult, error) {
		return FetchResult{}, ensureDiscoveryURLNotBlacklisted(ctx, "https://child.redirect.example/post")
	}
	if err := ProcessCandidate(context.Background(), c.ID, "0"); !errors.Is(err, ErrDiscoveryDomainBlacklisted) {
		t.Fatalf("redirect error = %v", err)
	}
	removed, err := DeleteDiscoveryDomainBlacklist(broad.Entry.ID)
	if err != nil || removed.AffectedCandidates != 0 {
		t.Fatalf("overlap deletion = %+v %v", removed, err)
	}
	removed, err = DeleteDiscoveryDomainBlacklist(narrow.Entry.ID)
	if err != nil || removed.AffectedCandidates != 1 {
		t.Fatalf("redirect deletion = %+v %v", removed, err)
	}
	if err := db.First(&c, c.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c.ProcessingState != DiscoveryProcessingFetchPending || c.NextProcessingAt == nil {
		t.Fatalf("redirect candidate did not resume: %+v", c)
	}
}
