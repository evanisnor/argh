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
	fetchCalled := make(chan struct{}, 1)
	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		fetchCalled <- struct{}{}
		return nil
	}}

	rl := NewStubRateLimitReader(50) // multiplier = 0 → paused

	_, cancel, fakeTicker, done := startPoller(t, myPRs, NewStubFetcher(), rl, time.Second)
	defer func() { cancel(); <-done }()

	fakeTicker.Tick()

	// Fetch must NOT be called within a short window.
	select {
	case <-fetchCalled:
		t.Error("fetch was called when rate limit was paused (remaining < 100)")
	case <-time.After(50 * time.Millisecond):
		// correct: no fetch
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
	firstCall := true
	secondTickCalls := make(chan string, 10)

	myPRs := &StubFetcher{FetchFunc: func(_ context.Context) error {
		if firstCall {
			firstCall = false
			return errors.New("transient error")
		}
		secondTickCalls <- "myPRs"
		return nil
	}}
	reviewQueue := &StubFetcher{FetchFunc: func(_ context.Context) error {
		secondTickCalls <- "reviewQueue"
		return nil
	}}

	rl := NewStubRateLimitReader(5000)

	_, cancel, fakeTicker, done := startPoller(t, myPRs, reviewQueue, rl, time.Second)
	defer func() { cancel(); <-done }()

	// First tick — myPRs returns error; reviewQueue should not be called.
	fakeTicker.Tick()

	// Brief pause, then verify reviewQueue was NOT called.
	time.Sleep(20 * time.Millisecond)
	select {
	case name := <-secondTickCalls:
		t.Errorf("unexpected call to %s on first (error) tick", name)
	default:
	}

	// Second tick — both succeed.
	fakeTicker.Tick()

	received := map[string]bool{}
	timeout := time.After(200 * time.Millisecond)
	for len(received) < 2 {
		select {
		case name := <-secondTickCalls:
			received[name] = true
		case <-timeout:
			t.Fatalf("fetchers called on second tick: %v; expected both", received)
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
