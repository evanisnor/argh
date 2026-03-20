package api

import (
	"context"
	"log/slog"
	"time"
)

// Fetcher fetches PR data from GitHub and writes results into the persistence layer.
type Fetcher interface {
	Fetch(ctx context.Context) error
}

// RateLimitReader reads the current API rate limit state.
type RateLimitReader interface {
	CurrentState() RateLimitState
}

// Ticker is an injectable abstraction over time.Ticker, allowing tests to
// control tick delivery without sleeping.
type Ticker interface {
	C() <-chan time.Time
	Reset(d time.Duration)
	Stop()
}

// NewTickerFunc constructs a Ticker for the given duration.
type NewTickerFunc func(d time.Duration) Ticker

// RealTicker wraps time.Ticker to implement Ticker.
type RealTicker struct {
	t *time.Ticker
}

// NewRealTicker returns a production Ticker backed by time.NewTicker.
func NewRealTicker(d time.Duration) Ticker {
	return &RealTicker{t: time.NewTicker(d)}
}

func (r *RealTicker) C() <-chan time.Time    { return r.t.C }
func (r *RealTicker) Reset(d time.Duration) { r.t.Reset(d) }
func (r *RealTicker) Stop()                  { r.t.Stop() }

// Poller orchestrates periodic GitHub data fetches with adaptive back-off
// based on the API rate limit. It supports forced immediate polls via the
// channel returned by ForcePollCh.
type Poller struct {
	myPRs        Fetcher
	reviewQueue  Fetcher
	rateLimits   RateLimitReader
	baseInterval time.Duration
	newTicker    NewTickerFunc
	forcePoll    chan struct{}
	sleepChecker SleepScheduleChecker
}

// NewPoller constructs a Poller.
func NewPoller(
	myPRs Fetcher,
	reviewQueue Fetcher,
	rateLimits RateLimitReader,
	baseInterval time.Duration,
	newTicker NewTickerFunc,
) *Poller {
	return &Poller{
		myPRs:        myPRs,
		reviewQueue:  reviewQueue,
		rateLimits:   rateLimits,
		baseInterval: baseInterval,
		newTicker:    newTicker,
		forcePoll:    make(chan struct{}, 1),
	}
}

// ForcePollCh returns the send-only force-poll channel. Sending to it triggers
// an immediate fetch regardless of the tick interval or rate-limit state.
func (p *Poller) ForcePollCh() chan<- struct{} {
	return p.forcePoll
}

// ForcePoll triggers an immediate poll. It is safe to call from any goroutine.
// If the force-poll channel is already full, the call is a no-op (the pending
// force-poll will still fire).
func (p *Poller) ForcePoll() {
	select {
	case p.forcePoll <- struct{}{}:
	default:
	}
}

// SetSleepSchedule configures an optional sleep schedule. Must be called
// before Start.
func (p *Poller) SetSleepSchedule(checker SleepScheduleChecker) {
	p.sleepChecker = checker
}

// Wake immediately resumes normal polling, overriding any active sleep window.
func (p *Poller) Wake() {
	if p.sleepChecker != nil {
		p.sleepChecker.Wake()
	}
}

// Start launches the polling goroutine. The returned channel is closed when
// the goroutine exits (i.e. when ctx is cancelled).
func (p *Poller) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.run(ctx)
	}()
	return done
}

// run is the main polling loop; it exits when ctx is cancelled.
func (p *Poller) run(ctx context.Context) {
	interval, _ := p.pollState()
	ticker := p.newTicker(interval)
	defer ticker.Stop()

	slog.Debug("poller: starting", "interval", interval)

	_ = p.fetch(ctx)
	slog.Debug("poller: initial fetch complete")

	for {
		select {
		case <-ctx.Done():
			slog.Debug("poller: stopped")
			return
		case <-p.forcePoll:
			slog.Debug("poller: force poll")
			_ = p.fetch(ctx)
		case <-ticker.C():
			slog.Debug("poller: tick fetch")
			newInterval, doFetch := p.pollState()
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
			}
			if doFetch {
				_ = p.fetch(ctx)
			}
		}
	}
}

// pollState computes the effective tick interval and whether to call the fetchers.
// Sleep schedule takes priority over rate-limit back-off. When rate-limit quota
// is critically low (multiplier == 0), polling is paused but the ticker
// continues at baseInterval so the loop can re-evaluate once quota recovers.
func (p *Poller) pollState() (interval time.Duration, fetch bool) {
	if p.sleepChecker != nil && p.sleepChecker.IsInSleepWindow() {
		return p.sleepChecker.SleepInterval(), true
	}
	multiplier := IntervalMultiplier(p.rateLimits.CurrentState().Remaining)
	if multiplier <= 0 {
		return p.baseInterval, false
	}
	return p.baseInterval * time.Duration(multiplier), true
}

// fetch calls both fetchers sequentially. The poller continues running
// regardless of errors; callers are responsible for observability.
func (p *Poller) fetch(ctx context.Context) error {
	if err := p.myPRs.Fetch(ctx); err != nil {
		return err
	}
	return p.reviewQueue.Fetch(ctx)
}
