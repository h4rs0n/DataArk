package discovery

import (
	"DataArk/config"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"time"
)

type SourceSchedulePolicy struct {
	Clock              Clock
	ActiveFeedInterval time.Duration
	ObservingInterval  time.Duration
	DormantInterval    time.Duration
	BackoffBase        time.Duration
	BackoffMax         time.Duration
	MinimumInterval    time.Duration
}

type SourceScheduleDecision struct {
	Basis       string
	Base        time.Duration
	Chosen      time.Duration
	NextDueAt   time.Time
	Explanation string
}

func ConfiguredSourceSchedulePolicy(clock Clock) SourceSchedulePolicy {
	if clock == nil {
		clock = SystemClock{}
	}
	return SourceSchedulePolicy{
		Clock:              clock,
		ActiveFeedInterval: configuredDuration(config.DISCOVERYACTIVEFEEDINTERVAL, 24*time.Hour),
		ObservingInterval:  configuredDuration(config.DISCOVERYOBSERVINGINTERVAL, 7*24*time.Hour),
		DormantInterval:    configuredDuration(config.DISCOVERYDORMANTINTERVAL, 30*24*time.Hour),
		BackoffBase:        configuredDuration(config.DISCOVERYBACKOFFBASE, 5*time.Minute),
		BackoffMax:         configuredDuration(config.DISCOVERYBACKOFFMAX, 24*time.Hour),
		MinimumInterval:    configuredDuration(config.DISCOVERYSCHEDULEMININTERVAL, time.Hour),
	}
}

func (policy SourceSchedulePolicy) NextSuccess(source DiscoverySource, siteStatus string, changed bool) time.Time {
	return policy.DecideNextSuccess(source, siteStatus, changed, DiscoverySiteOperationalStats{}).NextDueAt
}

func (policy SourceSchedulePolicy) DecideNextSuccess(source DiscoverySource, siteStatus string, changed bool, stats DiscoverySiteOperationalStats) SourceScheduleDecision {
	base := policy.ObservingInterval
	if source.EndpointType == "feed" || source.EndpointType == "rsshub" || source.Type == DiscoverySourceTypeFeed || source.Type == DiscoverySourceTypeRSSHub {
		base = policy.ActiveFeedInterval
	}
	if siteStatus == "dormant" {
		base = policy.DormantInterval
	}
	if base <= 0 {
		base = 24 * time.Hour
	}
	interval := base
	reasons := make([]string, 0, 4)
	if changed && source.LastSuccessAt != nil {
		observed := policy.Clock.Now().Sub(*source.LastSuccessAt)
		if observed < 15*time.Minute {
			observed = 15 * time.Minute
		}
		if observed < interval {
			interval = observed
			reasons = append(reasons, "recent_update")
		}
	}
	bonusFactor := 1.0
	if stats.IndependentInboundSites >= 2 {
		bonusFactor *= 0.8
		reasons = append(reasons, "multiple_inbound_sites")
	}
	if stats.EligibleCandidateCount > 0 {
		bonusFactor *= 0.75
		reasons = append(reasons, "eligible_output")
	}
	if stats.PositiveFeedbackArticles > 0 {
		bonusFactor *= 0.75
		reasons = append(reasons, "explicit_positive_articles")
	}
	bonusInterval := time.Duration(float64(base) * bonusFactor)
	if bonusInterval < interval {
		interval = bonusInterval
	}
	minimum := policy.MinimumInterval
	if minimum <= 0 {
		minimum = time.Hour
	}
	if interval < minimum {
		interval = minimum
	}
	basis := "base_floor"
	explanation := "maximum_idle_floor"
	if interval < base {
		basis = "extra_budget"
		explanation = strings.Join(reasons, ",")
	}
	return SourceScheduleDecision{Basis: basis, Base: base, Chosen: interval, NextDueAt: policy.Clock.Now().Add(interval), Explanation: explanation}
}

func (policy SourceSchedulePolicy) NextFailure(sourceID uint, failureCount int) time.Time {
	return policy.DecideNextFailure(sourceID, failureCount, "").NextDueAt
}

func (policy SourceSchedulePolicy) DecideNextFailure(sourceID uint, failureCount int, category string) SourceScheduleDecision {
	base, maximum := policy.failureBackoffProfile(category)
	delay := stableFailureDelay(sourceID, failureCount, category, base, maximum)
	explanation := strings.TrimSpace(category)
	if explanation == "" {
		explanation = "generic"
	}
	return SourceScheduleDecision{
		Basis: "failure_backoff", Base: base, Chosen: delay,
		NextDueAt: policy.Clock.Now().Add(delay), Explanation: explanation,
	}
}

func (policy SourceSchedulePolicy) FailureDelay(sourceID uint, failureCount int, category string) time.Duration {
	base, maximum := policy.failureBackoffProfile(category)
	return stableFailureDelay(sourceID, failureCount, category, base, maximum)
}

func (policy SourceSchedulePolicy) failureBackoffProfile(category string) (time.Duration, time.Duration) {
	base := policy.BackoffBase
	if base <= 0 {
		base = 5 * time.Minute
	}
	maximum := policy.BackoffMax
	if maximum < base {
		maximum = 24 * time.Hour
	}
	switch strings.TrimSpace(category) {
	case "timeout":
		base = maximumDuration(base, 30*time.Minute)
		maximum = maximumDuration(maximum, 24*time.Hour)
	case "network":
		base = maximumDuration(base, 2*time.Hour)
		maximum = maximumDuration(maximum, 7*24*time.Hour)
	case "dns":
		base = maximumDuration(base, 6*time.Hour)
		maximum = maximumDuration(maximum, 14*24*time.Hour)
	case "tls":
		base = maximumDuration(base, 12*time.Hour)
		maximum = maximumDuration(maximum, 14*24*time.Hour)
	case "http_status":
		base = maximumDuration(base, 6*time.Hour)
		maximum = maximumDuration(maximum, 7*24*time.Hour)
	case "content_type", "feed_parse", "processing", "handler", "body_too_large", "unsafe_url", "too_many_redirects":
		base = maximumDuration(base, 24*time.Hour)
		maximum = maximumDuration(maximum, 30*24*time.Hour)
	}
	if maximum < base {
		maximum = base
	}
	return base, maximum
}

func stableFailureDelay(sourceID uint, failureCount int, category string, base time.Duration, maximum time.Duration) time.Duration {
	if failureCount < 1 {
		failureCount = 1
	}
	exponent := math.Min(float64(failureCount-1), 16)
	delay := time.Duration(float64(base) * math.Pow(2, exponent))
	if delay > maximum || delay < 0 {
		delay = maximum
	}
	// Stable jitter prevents synchronized retries while keeping tests repeatable.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strconv.FormatUint(uint64(sourceID), 10) + ":" + strconv.Itoa(failureCount) + ":" + strings.TrimSpace(category)))
	jitterFraction := (float64(hash.Sum32()%2001)/10000.0 - 0.1)
	delay += time.Duration(float64(delay) * jitterFraction)
	if delay > maximum {
		delay = maximum
	}
	return delay
}

func maximumDuration(first time.Duration, second time.Duration) time.Duration {
	if first >= second {
		return first
	}
	return second
}
