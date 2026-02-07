package main

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(true)
	ok, err := cb.Allow()
	if !ok || err != nil {
		t.Fatal("expected circuit to be closed initially")
	}
}

func TestCircuitBreaker_TripAndReject(t *testing.T) {
	cb := NewCircuitBreaker(true)
	cb.Trip(Trigger429, 10)

	ok, err := cb.Allow()
	if ok || err == nil {
		t.Fatal("expected circuit to be open after trip")
	}
	if err.Trigger != Trigger429 {
		t.Errorf("expected trigger 429, got %d", err.Trigger)
	}
	if err.RetryAfterSec != 10 {
		t.Errorf("expected retry after 10s, got %d", err.RetryAfterSec)
	}
}

func TestCircuitBreaker_Trip5xx(t *testing.T) {
	cb := NewCircuitBreaker(true)
	cb.Trip(Trigger5xx, 30)

	ok, err := cb.Allow()
	if ok || err == nil {
		t.Fatal("expected circuit to be open")
	}
	if err.Trigger != Trigger5xx {
		t.Errorf("expected trigger 5xx, got %d", err.Trigger)
	}
}

func TestCircuitBreaker_TripTimeout(t *testing.T) {
	cb := NewCircuitBreaker(true)
	cb.Trip(TriggerTimeout, 30)

	ok, err := cb.Allow()
	if ok || err == nil {
		t.Fatal("expected circuit to be open")
	}
	if err.Trigger != TriggerTimeout {
		t.Errorf("expected trigger timeout, got %d", err.Trigger)
	}
}

func TestCircuitBreaker_TimerExpiry(t *testing.T) {
	cb := NewCircuitBreaker(true)

	// Trip with a very short timer
	cb.mu.Lock()
	cb.closed = false
	cb.openedAt = time.Now().Add(-2 * time.Second)
	cb.retryAfterSec = 1
	cb.triggerType = Trigger429
	cb.mu.Unlock()

	ok, err := cb.Allow()
	if !ok || err != nil {
		t.Fatal("expected circuit to auto-close after timer expiry")
	}

	// Should be closed now
	if !cb.IsClosed() {
		t.Fatal("expected circuit to be closed")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(true)
	cb.Trip(Trigger5xx, 30)

	ok, _ := cb.Allow()
	if ok {
		t.Fatal("expected circuit to be open")
	}

	cb.Reset()

	ok, err := cb.Allow()
	if !ok || err != nil {
		t.Fatal("expected circuit to be closed after reset")
	}
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	cb := NewCircuitBreaker(false)
	cb.Trip(Trigger429, 30)

	ok, err := cb.Allow()
	if !ok || err != nil {
		t.Fatal("expected disabled circuit breaker to always allow")
	}
}

func TestCircuitBreaker_DefaultRetryAfter(t *testing.T) {
	cb := NewCircuitBreaker(true)
	cb.Trip(Trigger5xx, 0)

	ok, err := cb.Allow()
	if ok {
		t.Fatal("expected circuit to be open")
	}
	if err.RetryAfterSec != defaultRetryAfterSec {
		t.Errorf("expected default retry after %d, got %d", defaultRetryAfterSec, err.RetryAfterSec)
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(true)

	var wg sync.WaitGroup
	// Run concurrent trips and allows
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cb.Trip(Trigger429, 5)
		}()
		go func() {
			defer wg.Done()
			cb.Allow()
		}()
	}
	wg.Wait()

	// Should not panic; state should be consistent
	cb.Reset()
	ok, _ := cb.Allow()
	if !ok {
		t.Fatal("expected circuit to be closed after reset")
	}
}
