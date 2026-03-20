package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

// startPoller is a test helper that creates a Poller with a FakeTicker, starts
// it, and returns the ticker and a done channel. The ticker factory sends the
// created FakeTicker back via tickerCh so the test can interact with it.
func startPoller(
	t *testing.T,
	myPRs Fetcher,
	reviewQueue Fetcher,
	rl *StubRateLimitReader,
	base time.Duration,
) (ctx context.Context, cancel context.CancelFunc, fakeTicker *FakeTicker, done <-chan struct{}) {
	t.Helper()

	tickerCh := make(chan *FakeTicker, 1)
	p := NewPoller(myPRs, reviewQueue, rl, base, func(d time.Duration) Ticker {
		ft := NewFakeTicker(d)
		tickerCh <- ft
		return ft
	})

	ctx, cancel = context.WithCancel(context.Background())
	done = p.Start(ctx)

	select {
	case fakeTicker = <-tickerCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker was not created in time")
	}

	return ctx, cancel, fakeTicker, done
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestPoller_InitialFetch_CalledOnStart(t *testing.T) {
	calls := make(chan string, 10)

	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000)

	_, cancel, _, done := startPoller(t, myPRs, reviewQueue, rl, time.Second)
	defer func() { cancel(); <-done }()

	// Both fetchers should be called by the initial fetch before any tick.
	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("initial fetch called: %v; expected both myPRs and reviewQueue", received)
		}
	}
}

func TestPoller_TickerFires_BothFetchersCalled(t *testing.T) {
	calls := make(chan string, 10)

	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000) // multiplier = 1

	_, cancel, fakeTicker, done := startPoller(t, myPRs, reviewQueue, rl, time.Second)
	defer func() { cancel(); <-done }()

	fakeTicker.Tick()

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called: %v; expected both myPRs and reviewQueue", received)
		}
	}
}

func TestPoller_ForcePoll_ImmediateFetch(t *testing.T) {
	calls := make(chan string, 10)

	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000)

	tickerCh := make(chan *FakeTicker, 1)
	p := NewPoller(myPRs, reviewQueue, rl, time.Second, func(d time.Duration) Ticker {
		ft := NewFakeTicker(d)
		tickerCh <- ft
		return ft
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	// Wait for goroutine to start (ticker created).
	select {
	case <-tickerCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}

	// Send force-poll — no tick needed.
	p.ForcePollCh() <- struct{}{}

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called: %v; expected both after force-poll", received)
		}
	}
}

func TestPoller_Shutdown_GoroutineExits(t *testing.T) {
	rl := NewStubRateLimitReader(5000)

	_, cancel, _, done := startPoller(t, NewStubFetcher(), NewStubFetcher(), rl, time.Second)

	cancel()

	select {
	case <-done:
		// goroutine exited cleanly
	case <-time.After(200 * time.Millisecond):
		t.Fatal("goroutine did not exit after context cancellation")
	}
}

func TestPoller_IntervalAdjusted_OnRateLimitChange(t *testing.T) {
	rl := NewStubRateLimitReader(5000) // multiplier = 1

	_, cancel, fakeTicker, done := startPoller(t, NewStubFetcher(), NewStubFetcher(), rl, 10*time.Millisecond)
	defer func() { cancel(); <-done }()

	// Change rate limit to multiplier = 2 (500–999 remaining).
	rl.SetRemaining(700)

	// Trigger a tick — run() evaluates pollState and should reset the ticker.
	fakeTicker.Tick()

	select {
	case d := <-fakeTicker.ResetCh:
		if d != 20*time.Millisecond {
			t.Errorf("expected ticker reset to 20ms (2×base), got %v", d)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ticker was not reset after rate-limit change")
	}
}

func TestPoller_PausedWhenRateLimitCritical_FetchNotCalled(t *testing.T) {
	fetchCalled := make(chan struct{}, 10)
	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		fetchCalled <- struct{}{}
		return nil
	}}

	rl := NewStubRateLimitReader(50) // multiplier = 0 → paused

	_, cancel, fakeTicker, done := startPoller(t, myPRs, NewStubFetcher(), rl, time.Second)
	defer func() { cancel(); <-done }()

	// Drain the initial fetch call (unconditional, ignores rate limit).
	select {
	case <-fetchCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("initial fetch did not call fetcher")
	}

	fakeTicker.Tick()

	// Tick-triggered fetch must NOT be called when rate limit is paused.
	select {
	case <-fetchCalled:
		t.Error("fetch was called on tick when rate limit was paused (remaining < 100)")
	case <-time.After(50 * time.Millisecond):
		// correct: no fetch on tick
	}
}

func TestPoller_ForcePoll_AlwaysFetches_EvenWhenPaused(t *testing.T) {
	calls := make(chan string, 10)
	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(50) // paused

	tickerCh := make(chan *FakeTicker, 1)
	p := NewPoller(myPRs, reviewQueue, rl, time.Second, func(d time.Duration) Ticker {
		ft := NewFakeTicker(d)
		tickerCh <- ft
		return ft
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	select {
	case <-tickerCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created")
	}

	// Force-poll bypasses rate-limit guard.
	p.ForcePollCh() <- struct{}{}

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called: %v; expected both even when paused", received)
		}
	}
}

func TestPoller_FetchError_PollerContinues(t *testing.T) {
	// If the first fetcher returns an error, the second is not called for that
	// fetch cycle, but the poller keeps running and fetches again on the next tick.
	// The initial fetch (before the loop) consumes the firstCall=true error case.
	firstCall := true
	calls := make(chan string, 10)

	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		if firstCall {
			firstCall = false
			return errors.New("transient error")
		}
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000)

	_, cancel, fakeTicker, done := startPoller(t, myPRs, reviewQueue, rl, time.Second)
	defer func() { cancel(); <-done }()

	// Initial fetch errors on myPRs; reviewQueue should NOT be called.
	time.Sleep(20 * time.Millisecond)
	select {
	case name := <-calls:
		t.Errorf("unexpected call to %s during initial (error) fetch", name)
	default:
	}

	// First tick — both succeed since firstCall is now false.
	fakeTicker.Tick()

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called on first tick: %v; expected both", received)
		}
	}
}

func TestPoller_InitialTickerInterval_ReflectsBaseInterval(t *testing.T) {
	rl := NewStubRateLimitReader(5000) // multiplier = 1

	base := 42 * time.Millisecond
	capturedDur := make(chan time.Duration, 1)

	p := NewPoller(NewStubFetcher(), NewStubFetcher(), rl, base, func(d time.Duration) Ticker {
		capturedDur <- d
		return NewFakeTicker(d)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	select {
	case d := <-capturedDur:
		if d != base {
			t.Errorf("expected initial ticker interval %v, got %v", base, d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}
}

func TestFakeTicker_StopAndDuration(t *testing.T) {
	ft := NewFakeTicker(5 * time.Second)

	if got := ft.Duration(); got != 5*time.Second {
		t.Errorf("Duration() = %v, want 5s", got)
	}

	ft.Reset(10 * time.Second)
	if got := ft.Duration(); got != 10*time.Second {
		t.Errorf("Duration() after Reset = %v, want 10s", got)
	}

	ft.Stop() // must not panic; coverage for the method body
}

func TestRealTicker_Methods(t *testing.T) {
	rt := NewRealTicker(time.Hour) // long interval so it never fires naturally
	if rt.C() == nil {
		t.Error("C() returned nil channel")
	}
	rt.Reset(time.Hour)
	rt.Stop()
}

// ── Sleep schedule integration ────────────────────────────────────────────────

func TestPoller_SleepSchedule_InitialInterval_WhenActive(t *testing.T) {
	sleepChecker := NewStubSleepScheduleChecker()
	sleepChecker.SetInSleepWindow(true)
	sleepInterval := 100 * time.Millisecond
	sleepChecker.SetSleepInterval(sleepInterval)

	rl := NewStubRateLimitReader(5000)
	base := 10 * time.Millisecond

	capturedDur := make(chan time.Duration, 1)
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), rl, base, func(d time.Duration) Ticker {
		capturedDur <- d
		return NewFakeTicker(d)
	})
	p.SetSleepSchedule(sleepChecker)

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	select {
	case d := <-capturedDur:
		if d != sleepInterval {
			t.Errorf("initial ticker interval = %v, want sleep interval %v", d, sleepInterval)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}
}

func TestPoller_SleepSchedule_InitialInterval_WhenInactive(t *testing.T) {
	sleepChecker := NewStubSleepScheduleChecker()
	sleepChecker.SetInSleepWindow(false)

	rl := NewStubRateLimitReader(5000) // multiplier = 1
	base := 10 * time.Millisecond

	capturedDur := make(chan time.Duration, 1)
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), rl, base, func(d time.Duration) Ticker {
		capturedDur <- d
		return NewFakeTicker(d)
	})
	p.SetSleepSchedule(sleepChecker)

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	select {
	case d := <-capturedDur:
		if d != base {
			t.Errorf("initial ticker interval = %v, want base interval %v", d, base)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}
}

func TestPoller_SleepSchedule_TickerReset_WhenWindowActivates(t *testing.T) {
	sleepChecker := NewStubSleepScheduleChecker()
	sleepChecker.SetInSleepWindow(false)
	sleepInterval := 200 * time.Millisecond
	sleepChecker.SetSleepInterval(sleepInterval)

	rl := NewStubRateLimitReader(5000)

	_, cancel, fakeTicker, done := startPoller(t, NewStubFetcher(), NewStubFetcher(), rl, 10*time.Millisecond)
	// Note: startPoller doesn't set a sleep checker, so we can't use it here.
	// Use the inline construction instead.
	cancel()
	<-done

	// Rebuild with sleep checker.
	tickerCh := make(chan *FakeTicker, 1)
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), rl, 10*time.Millisecond, func(d time.Duration) Ticker {
		ft := NewFakeTicker(d)
		tickerCh <- ft
		return ft
	})
	p.SetSleepSchedule(sleepChecker)

	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := p.Start(ctx2)
	defer func() { cancel2(); <-done2 }()

	select {
	case fakeTicker = <-tickerCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}

	// Activate sleep window and fire a tick.
	sleepChecker.SetInSleepWindow(true)
	fakeTicker.Tick()

	select {
	case d := <-fakeTicker.ResetCh:
		if d != sleepInterval {
			t.Errorf("ticker reset to %v, want sleep interval %v", d, sleepInterval)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ticker was not reset after sleep window activated")
	}
}

func TestPoller_SleepSchedule_FetchStillCalled_WhenInSleepWindow(t *testing.T) {
	sleepChecker := NewStubSleepScheduleChecker()
	sleepChecker.SetInSleepWindow(true)
	sleepChecker.SetSleepInterval(time.Second)

	calls := make(chan string, 10)
	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		calls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000)
	tickerCh := make(chan *FakeTicker, 1)
	p := NewPoller(myPRs, reviewQueue, rl, 10*time.Millisecond, func(d time.Duration) Ticker {
		ft := NewFakeTicker(d)
		tickerCh <- ft
		return ft
	})
	p.SetSleepSchedule(sleepChecker)

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)
	defer func() { cancel(); <-done }()

	var fakeTicker *FakeTicker
	select {
	case fakeTicker = <-tickerCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ticker not created in time")
	}

	fakeTicker.Tick()

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-calls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called: %v; expected both even during sleep window", received)
		}
	}
}

func TestPoller_ForcePoll_DoesNotBlock(t *testing.T) {
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), NewStubRateLimitReader(5000), time.Second,
		func(d time.Duration) Ticker { return NewFakeTicker(d) })

	// ForcePoll twice without a consumer on the channel — must not block.
	p.ForcePoll()
	p.ForcePoll()
}

func TestPoller_Wake_NilSleepChecker(t *testing.T) {
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), NewStubRateLimitReader(5000), time.Second,
		func(d time.Duration) Ticker { return NewFakeTicker(d) })
	p.Wake() // must not panic with nil sleepChecker
}

func TestPoller_Wake_NonNilSleepChecker(t *testing.T) {
	stub := NewStubSleepScheduleChecker()
	p := NewPoller(NewStubFetcher(), NewStubFetcher(), NewStubRateLimitReader(5000), time.Second,
		func(d time.Duration) Ticker { return NewFakeTicker(d) })
	p.SetSleepSchedule(stub)
	p.Wake()
	if !stub.WasWakeCalled() {
		t.Error("expected Wake to be called on sleep checker")
	}
}
