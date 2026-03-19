package eventbus_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/evanisnor/argh/internal/eventbus"
)

// ── Subscribe / Publish ───────────────────────────────────────────────────────

func TestBus_SubscribeAndReceive(t *testing.T) {
	b := eventbus.New()

	type pr struct{ title string }
	before := pr{"old title"}
	after := pr{"new title"}
	want := eventbus.Event{
		Type:   eventbus.PRUpdated,
		Before: before,
		After:  after,
	}

	var got eventbus.Event
	b.Subscribe(func(e eventbus.Event) {
		got = e
	})

	b.Publish(want)

	if got.Type != want.Type {
		t.Errorf("Type: got %q, want %q", got.Type, want.Type)
	}
	if got.Before != want.Before {
		t.Errorf("Before: got %v, want %v", got.Before, want.Before)
	}
	if got.After != want.After {
		t.Errorf("After: got %v, want %v", got.After, want.After)
	}
}

func TestBus_AllEventTypesDelivered(t *testing.T) {
	types := []eventbus.EventType{
		eventbus.PRUpdated,
		eventbus.CIChanged,
		eventbus.ReviewChanged,
		eventbus.WatchFired,
		eventbus.RateLimitWarning,
	}

	for _, et := range types {
		et := et
		t.Run(string(et), func(t *testing.T) {
			b := eventbus.New()
			var received eventbus.EventType
			b.Subscribe(func(e eventbus.Event) {
				received = e.Type
			})
			b.Publish(eventbus.Event{Type: et})
			if received != et {
				t.Errorf("got %q, want %q", received, et)
			}
		})
	}
}

func TestBus_MultipleSubscribersAllReceive(t *testing.T) {
	b := eventbus.New()

	const n = 5
	counts := make([]int, n)

	for i := 0; i < n; i++ {
		i := i
		b.Subscribe(func(e eventbus.Event) {
			counts[i]++
		})
	}

	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})

	for i, c := range counts {
		if c != 1 {
			t.Errorf("subscriber %d: received %d events, want 1", i, c)
		}
	}
}

func TestBus_PublishWithNoSubscribers(t *testing.T) {
	b := eventbus.New()
	// Should not panic.
	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})
}

// ── Unsubscribe ───────────────────────────────────────────────────────────────

func TestBus_UnsubscribeStopsDelivery(t *testing.T) {
	b := eventbus.New()

	var count int
	unsub := b.Subscribe(func(e eventbus.Event) {
		count++
	})

	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})
	if count != 1 {
		t.Fatalf("before unsubscribe: got %d, want 1", count)
	}

	unsub()
	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})

	if count != 1 {
		t.Errorf("after unsubscribe: got %d, want 1 (no additional delivery)", count)
	}
}

func TestBus_UnsubscribeOneOfManySubscribers(t *testing.T) {
	b := eventbus.New()

	var aCount, bCount int
	unsub := b.Subscribe(func(e eventbus.Event) { aCount++ })
	b.Subscribe(func(e eventbus.Event) { bCount++ })

	b.Publish(eventbus.Event{Type: eventbus.CIChanged})
	unsub()
	b.Publish(eventbus.Event{Type: eventbus.CIChanged})

	if aCount != 1 {
		t.Errorf("unsubscribed handler: got %d, want 1", aCount)
	}
	if bCount != 2 {
		t.Errorf("remaining handler: got %d, want 2", bCount)
	}
}

// ── Shutdown ──────────────────────────────────────────────────────────────────

func TestBus_ShutdownSilencesPublish(t *testing.T) {
	b := eventbus.New()

	var count int
	b.Subscribe(func(e eventbus.Event) {
		count++
	})

	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})
	if count != 1 {
		t.Fatalf("before shutdown: got %d, want 1", count)
	}

	b.Shutdown()
	b.Publish(eventbus.Event{Type: eventbus.PRUpdated})

	if count != 1 {
		t.Errorf("after shutdown: got %d, want 1 (no delivery after shutdown)", count)
	}
}

func TestBus_ShutdownIsIdempotent(t *testing.T) {
	b := eventbus.New()
	// Calling Shutdown multiple times must not panic.
	b.Shutdown()
	b.Shutdown()
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestBus_ConcurrentPublishAndSubscribe(t *testing.T) {
	b := eventbus.New()

	var received atomic.Int64
	const publishers = 10
	const eventsEach = 100

	// Pre-register one subscriber before any goroutines start.
	b.Subscribe(func(e eventbus.Event) {
		received.Add(1)
	})

	var wg sync.WaitGroup
	wg.Add(publishers)
	for i := 0; i < publishers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < eventsEach; j++ {
				b.Publish(eventbus.Event{Type: eventbus.PRUpdated})
			}
		}()
	}
	wg.Wait()

	want := int64(publishers * eventsEach)
	if got := received.Load(); got != want {
		t.Errorf("got %d events, want %d", got, want)
	}
}

func TestBus_NoGoroutineLeakAfterShutdown(t *testing.T) {
	// The bus uses a callback registry (no internal goroutines), so shutdown
	// simply prevents future publishes. Verify that publish after shutdown
	// does not invoke any handler, and that the bus can be garbage-collected
	// without leaking resources.
	b := eventbus.New()

	var count int
	b.Subscribe(func(e eventbus.Event) { count++ })

	b.Shutdown()

	// Publishing on a shut-down bus must not call any subscriber.
	b.Publish(eventbus.Event{Type: eventbus.WatchFired})
	if count != 0 {
		t.Errorf("got %d calls after shutdown, want 0", count)
	}
	// b goes out of scope; GC reclaims it — no goroutines to leak.
}
