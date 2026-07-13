package discovery

import (
	"context"
	"sync"
	"time"
)

type DelayFunc func(context.Context, time.Duration) error

type hostLimitState struct {
	semaphore chan struct{}
	next      time.Time
}

type HostLimiter struct {
	mu            sync.Mutex
	states        map[string]*hostLimitState
	maxConcurrent int
	minInterval   time.Duration
	clock         Clock
	delay         DelayFunc
}

func NewHostLimiter(maxConcurrent int, minInterval time.Duration, clock Clock, delay DelayFunc) *HostLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if minInterval < 0 {
		minInterval = 0
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if delay == nil {
		delay = waitContext
	}
	return &HostLimiter{
		states:        make(map[string]*hostLimitState),
		maxConcurrent: maxConcurrent,
		minInterval:   minInterval,
		clock:         clock,
		delay:         delay,
	}
}

func (limiter *HostLimiter) Acquire(ctx context.Context, host string) (func(), error) {
	limiter.mu.Lock()
	state := limiter.states[host]
	if state == nil {
		state = &hostLimitState{semaphore: make(chan struct{}, limiter.maxConcurrent)}
		limiter.states[host] = state
	}
	limiter.mu.Unlock()

	select {
	case state.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	limiter.mu.Lock()
	now := limiter.clock.Now()
	start := now
	if state.next.After(start) {
		start = state.next
	}
	state.next = start.Add(limiter.minInterval)
	limiter.mu.Unlock()
	if delay := start.Sub(now); delay > 0 {
		if err := limiter.delay(ctx, delay); err != nil {
			<-state.semaphore
			return nil, err
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() { <-state.semaphore })
	}, nil
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
