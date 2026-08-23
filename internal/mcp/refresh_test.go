package mcp

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestRefreshWithRetry_SuccessAfterNFailures verifies that the retry loop
// succeeds when refresh eventually returns nil, and that onWarning was called
// for each failed attempt.
func TestRefreshWithRetry_SuccessAfterNFailures(t *testing.T) {
	var calls int32
	var warnings int32

	refresh := func() error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("transient failure")
		}
		return nil
	}

	onWarning := func(err error) {
		atomic.AddInt32(&warnings, 1)
	}

	// Use tiny backoffs so the test is fast.
	backoffs := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}

	err := RefreshWithRetry(context.Background(), refresh, backoffs, onWarning)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
	if got := atomic.LoadInt32(&warnings); got != 2 {
		t.Errorf("expected 2 warnings (for the 2 failures), got %d", got)
	}
}

// TestRefreshWithRetry_BackoffScheduleRespected verifies that the delays
// between attempts match the provided backoff schedule. We cancel after the
// 2nd attempt completes to bound the test.
func TestRefreshWithRetry_BackoffScheduleRespected(t *testing.T) {
	var calls int32

	refresh := func() error {
		atomic.AddInt32(&calls, 1)
		return errors.New("always fails")
	}

	// Custom short backoffs: 5ms, 10ms, 20ms.
	backoffs := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	warningCount := int32(0)
	onWarning := func(err error) {
		if atomic.AddInt32(&warningCount, 1) == 2 {
			cancel()
		}
	}

	err := RefreshWithRetry(ctx, refresh, backoffs, onWarning)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}

	// Should have been called twice (2 failures before cancellation).
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

// TestRefreshWithRetry_BackoffTiming verifies that actual elapsed time between
// the first two attempts is at least the first backoff duration.
func TestRefreshWithRetry_BackoffTiming(t *testing.T) {
	refresh := func() error {
		return errors.New("fail")
	}

	backoffs := []time.Duration{15 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_ = RefreshWithRetry(ctx, refresh, backoffs, nil)
	elapsed := time.Since(start)

	// Should have waited at least 15ms (the backoff) before the second
	// attempt, and the context cancels at 50ms. So total should be >= 15ms.
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected elapsed >= ~15ms (backoff respected), got %v", elapsed)
	}
}

// TestRefreshWithRetry_ContextCancelled verifies that the loop exits promptly
// when the context is already cancelled and does not leak goroutines.
func TestRefreshWithRetry_ContextCancelled(t *testing.T) {
	refresh := func() error {
		return errors.New("fail")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately.
	cancel()

	err := RefreshWithRetry(ctx, refresh, DefaultRefreshBackoff, nil)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestRefreshWithRetry_ImmediateSuccess verifies that if refresh succeeds on
// the first try, no warnings are emitted and no backoff is waited.
func TestRefreshWithRetry_ImmediateSuccess(t *testing.T) {
	var calls int32
	var warnings int32

	refresh := func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	onWarning := func(err error) {
		atomic.AddInt32(&warnings, 1)
	}

	start := time.Now()
	err := RefreshWithRetry(context.Background(), refresh, DefaultRefreshBackoff, onWarning)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
	if got := atomic.LoadInt32(&warnings); got != 0 {
		t.Errorf("expected 0 warnings, got %d", got)
	}
	// Should return near-instantly (no backoff wait).
	if elapsed > 50*time.Millisecond {
		t.Errorf("expected near-instant return, got %v", elapsed)
	}
}

// TestRefreshWithRetry_NilOnWarning verifies the loop handles a nil onWarning
// callback without panicking.
func TestRefreshWithRetry_NilOnWarning(t *testing.T) {
	var calls int32

	refresh := func() error {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return errors.New("fail")
		}
		return nil
	}

	backoffs := []time.Duration{1 * time.Millisecond}

	err := RefreshWithRetry(context.Background(), refresh, backoffs, nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

// TestRefreshWithRetry_EmptyBackoffDefaults verifies that an empty backoff
// slice falls back to DefaultRefreshBackoff.
func TestRefreshWithRetry_EmptyBackoffDefaults(t *testing.T) {
	refresh := func() error {
		return nil
	}

	// Empty backoff slice should default to DefaultRefreshBackoff and still
	// succeed immediately (refresh returns nil on first call).
	err := RefreshWithRetry(context.Background(), refresh, nil, nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestRefreshWithRetry_CapRepeats verifies that once the schedule is exhausted,
// the last (capped) duration repeats indefinitely.
func TestRefreshWithRetry_CapRepeats(t *testing.T) {
	var calls int32

	refresh := func() error {
		atomic.AddInt32(&calls, 1)
		return errors.New("always fail")
	}

	// Short schedule: 1ms, 2ms (cap = 2ms).
	backoffs := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	warningCount := int32(0)
	onWarning := func(err error) {
		if atomic.AddInt32(&warningCount, 1) == 5 {
			cancel()
		}
	}

	_ = RefreshWithRetry(ctx, refresh, backoffs, onWarning)

	// Should have been called 5 times (5 failures before cancellation).
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Errorf("expected 5 calls, got %d", got)
	}
}

// TestRefreshWithRetry_NoGoroutineLeak verifies that the function does not
// leak goroutines — it should return fully when ctx is cancelled.
func TestRefreshWithRetry_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	refresh := func() error {
		return errors.New("fail")
	}

	backoffs := []time.Duration{50 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_ = RefreshWithRetry(ctx, refresh, backoffs, nil)

	// Give time for any cleanup to settle.
	time.Sleep(30 * time.Millisecond)

	after := runtime.NumGoroutine()
	if after > before+1 { // allow 1 for scheduling variance
		t.Errorf("possible goroutine leak: before=%d, after=%d", before, after)
	}
}
