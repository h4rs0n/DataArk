package discovery

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRequestSourceFetchEnqueuesWithoutNetworkAndMarksDue(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Now()}
	oldClock, oldFetcher := discoveryClock, fetchDiscoveryRequest
	oldQueue, _ := jobQueue()
	discoveryClock = clock
	calls := 0
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
		calls++
		return FetchResult{StatusCode: http.StatusOK, Body: []byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)}, nil
	}
	t.Cleanup(func() { discoveryClock = oldClock; fetchDiscoveryRequest = oldFetcher; SetJobQueue(oldQueue) })
	queue := &recoveryRecordingQueue{}
	SetJobQueue(queue)
	future := clock.Now().Add(time.Hour)
	source := DiscoverySource{Name: "Queued refresh", URL: "https://queued.example/feed", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true, NextDueAt: &future}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := RequestDiscoverySourceFetch(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(queue.fetches) != 1 || queue.fetches[0] != source.ID {
		t.Fatalf("calls=%d queued=%v", calls, queue.fetches)
	}
	if err := db.First(&source, source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if source.NextDueAt == nil || source.NextDueAt.After(clock.Now()) {
		t.Fatalf("due=%v", source.NextDueAt)
	}
	if err := RunFetchDiscoverySourceJob(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("worker network calls=%d", calls)
	}
}

func TestRequestSourceFetchRespectsDisabledBlacklistAndCooldown(t *testing.T) {
	setupSQLiteDB(t)
	oldQueue, _ := jobQueue()
	oldFetcher := fetchDiscoveryRequest
	t.Cleanup(func() { SetJobQueue(oldQueue); fetchDiscoveryRequest = oldFetcher })
	queue := &recoveryRecordingQueue{}
	SetJobQueue(queue)
	source := DiscoverySource{Name: "Restricted", URL: "https://restricted.example/feed", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&source).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := RequestDiscoverySourceFetch(context.Background(), source.ID); err == nil {
		t.Fatal("disabled source accepted")
	}
	if err := db.Model(&source).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	entry := DiscoveryDomainBlacklistEntry{Domain: "restricted.example"}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := RequestDiscoverySourceFetch(context.Background(), source.ID); err == nil {
		t.Fatal("blacklisted source accepted")
	}
	if len(queue.fetches) != 0 {
		t.Fatalf("restricted tasks=%v", queue.fetches)
	}
	if err := db.Delete(&entry).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Model(&source).Updates(map[string]interface{}{"failure_count": 2, "last_attempt_at": now, "backoff_reason": "network"}).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) { calls++; return FetchResult{}, nil }
	if err := RequestDiscoverySourceFetch(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if err := RunFetchDiscoverySourceJob(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("queued refresh bypassed failure cooldown")
	}
	SetJobQueue(nil)
	if err := RequestDiscoverySourceFetch(context.Background(), source.ID); err == nil {
		t.Fatal("offline queue accepted request")
	}
}
