package discovery

import (
	"DataArk/config"
	"hash/fnv"
	"math"
	"strconv"
	"time"
)

type SourceSchedulePolicy struct {
	Clock              Clock
	ActiveFeedInterval time.Duration
	ObservingInterval  time.Duration
	DormantInterval    time.Duration
	BackoffBase        time.Duration
	BackoffMax         time.Duration
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
	}
}

func (policy SourceSchedulePolicy) NextSuccess(source DiscoverySource, siteStatus string, changed bool) time.Time {
	interval := policy.ObservingInterval
	if source.EndpointType == "feed" || source.EndpointType == "rsshub" || source.Type == DiscoverySourceTypeFeed || source.Type == DiscoverySourceTypeRSSHub {
		interval = policy.ActiveFeedInterval
	}
	if siteStatus == "dormant" {
		interval = policy.DormantInterval
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if changed && source.LastSuccessAt != nil {
		observed := policy.Clock.Now().Sub(*source.LastSuccessAt)
		if observed < 15*time.Minute {
			observed = 15 * time.Minute
		}
		if observed < interval {
			interval = observed
		}
	}
	return policy.Clock.Now().Add(interval)
}

func (policy SourceSchedulePolicy) NextFailure(sourceID uint, failureCount int) time.Time {
	if failureCount < 1 {
		failureCount = 1
	}
	base := policy.BackoffBase
	if base <= 0 {
		base = 5 * time.Minute
	}
	maximum := policy.BackoffMax
	if maximum < base {
		maximum = 24 * time.Hour
	}
	exponent := math.Min(float64(failureCount-1), 16)
	delay := time.Duration(float64(base) * math.Pow(2, exponent))
	if delay > maximum || delay < 0 {
		delay = maximum
	}
	// Stable jitter prevents synchronized retries while keeping tests repeatable.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strconv.FormatUint(uint64(sourceID), 10) + ":" + strconv.Itoa(failureCount)))
	jitterFraction := (float64(hash.Sum32()%2001)/10000.0 - 0.1)
	delay += time.Duration(float64(delay) * jitterFraction)
	if delay > maximum {
		delay = maximum
	}
	return policy.Clock.Now().Add(delay)
}
