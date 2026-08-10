package discovery

import (
	"testing"
	"time"
)

func TestSourceFailureBackoffProfilesAreStableAndCategoryAware(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)}
	policy := SourceSchedulePolicy{Clock: clock, BackoffBase: 5 * time.Minute, BackoffMax: 24 * time.Hour}
	timeout := policy.FailureDelay(17, 1, "timeout")
	network := policy.FailureDelay(17, 1, "network")
	dns := policy.FailureDelay(17, 1, "dns")
	structural := policy.FailureDelay(17, 1, "feed_parse")
	if !(timeout < network && network < dns && dns < structural) {
		t.Fatalf("failure delays timeout=%s network=%s dns=%s structural=%s", timeout, network, dns, structural)
	}
	if timeout < 27*time.Minute || timeout > 33*time.Minute {
		t.Fatalf("timeout delay = %s", timeout)
	}
	if structural < 21*time.Hour || structural > 27*time.Hour {
		t.Fatalf("structural delay = %s", structural)
	}
	if repeat := policy.FailureDelay(17, 1, "feed_parse"); repeat != structural {
		t.Fatalf("stable delay changed from %s to %s", structural, repeat)
	}
	if later := policy.FailureDelay(17, 2, "feed_parse"); later <= structural {
		t.Fatalf("second structural delay = %s, want greater than %s", later, structural)
	}
	decision := policy.DecideNextFailure(17, 1, "dns")
	if decision.Basis != "failure_backoff" || decision.Explanation != "dns" || decision.Base != 6*time.Hour || !decision.NextDueAt.Equal(clock.Now().Add(decision.Chosen)) {
		t.Fatalf("DNS decision = %#v", decision)
	}
}
