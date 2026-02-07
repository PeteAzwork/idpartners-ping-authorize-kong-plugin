package main

import (
	"fmt"
	"sync"
	"time"
)

const defaultRetryAfterSec = 30

// CircuitBreakerTrigger identifies what caused the circuit to open.
type CircuitBreakerTrigger int

const (
	TriggerNone    CircuitBreakerTrigger = iota
	Trigger429                           // Rate limited by PingAuthorize
	Trigger5xx                           // Server error from PingAuthorize
	TriggerTimeout                       // Connection/read/write timeout
)

// CircuitBreakerOpenError is returned when the circuit breaker is open and rejecting traffic.
type CircuitBreakerOpenError struct {
	Trigger       CircuitBreakerTrigger
	RetryAfterSec int
	RemainingMs   int64 // milliseconds until circuit closes
}

func (e *CircuitBreakerOpenError) Error() string {
	return fmt.Sprintf("circuit breaker open (trigger=%d), retry after %d seconds", e.Trigger, e.RetryAfterSec)
}

// CircuitBreaker implements a per-instance circuit breaker with mutex protection.
type CircuitBreaker struct {
	mu            sync.Mutex
	enabled       bool
	closed        bool // true = circuit is closed (allowing traffic)
	openedAt      time.Time
	retryAfterSec int
	triggerType   CircuitBreakerTrigger
}

// NewCircuitBreaker creates a new circuit breaker. Initial state is closed (traffic flows).
func NewCircuitBreaker(enabled bool) *CircuitBreaker {
	return &CircuitBreaker{
		enabled: enabled,
		closed:  true,
	}
}

// Allow checks if a request can proceed. Returns true if allowed.
// If the circuit is open but the retry timer has expired, it transitions to closed.
func (cb *CircuitBreaker) Allow() (bool, *CircuitBreakerOpenError) {
	if !cb.enabled {
		return true, nil
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.closed {
		return true, nil
	}

	// Check if retry timer has expired
	elapsed := time.Since(cb.openedAt)
	retryDuration := time.Duration(cb.retryAfterSec) * time.Second
	if elapsed >= retryDuration {
		cb.closed = true
		cb.triggerType = TriggerNone
		return true, nil
	}

	remaining := retryDuration - elapsed
	return false, &CircuitBreakerOpenError{
		Trigger:       cb.triggerType,
		RetryAfterSec: cb.retryAfterSec,
		RemainingMs:   remaining.Milliseconds(),
	}
}

// Trip opens the circuit breaker with the given trigger and retry-after duration.
func (cb *CircuitBreaker) Trip(trigger CircuitBreakerTrigger, retryAfterSec int) {
	if !cb.enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.closed = false
	cb.openedAt = time.Now()
	cb.triggerType = trigger
	if retryAfterSec > 0 {
		cb.retryAfterSec = retryAfterSec
	} else {
		cb.retryAfterSec = defaultRetryAfterSec
	}
}

// Reset closes the circuit breaker (allows traffic again).
func (cb *CircuitBreaker) Reset() {
	if !cb.enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.closed = true
	cb.triggerType = TriggerNone
}

// IsClosed returns true if the circuit is closed (allowing traffic).
func (cb *CircuitBreaker) IsClosed() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.closed
}
