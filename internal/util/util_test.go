package util

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForZeroAlreadyZero(t *testing.T) {
	var counter atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if !WaitForZero(ctx, &counter, time.Millisecond) {
		t.Errorf("Expected WaitForZero to return true immediately for a zeroed counter")
	}
}

func TestWaitForZeroWaitsThenSucceeds(t *testing.T) {
	var counter atomic.Int64
	counter.Add(1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		counter.Add(-1)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if !WaitForZero(ctx, &counter, 5*time.Millisecond) {
		t.Errorf("Expected WaitForZero to return true once the counter reached zero")
	}
}

func TestWaitForZeroTimesOut(t *testing.T) {
	var counter atomic.Int64
	counter.Add(1) // never decremented

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if WaitForZero(ctx, &counter, 5*time.Millisecond) {
		t.Errorf("Expected WaitForZero to return false when the counter never reaches zero")
	}
}

// TestWaitForZeroDoesNotFalsePositiveOnRacingCtxDone is a regression test:
// the counter can reach zero shortly before ctx is done, but if the next
// ticker tick hasn't happened yet, a naive implementation would report a
// timeout even though the thing being waited for already finished. The poll
// interval here is deliberately longer than ctx's timeout so the ticker
// cannot have ticked before ctx.Done() fires - only an explicit counter
// check in the ctx.Done() branch itself can catch this.
func TestWaitForZeroDoesNotFalsePositiveOnRacingCtxDone(t *testing.T) {
	var counter atomic.Int64
	counter.Add(1)

	go func() {
		time.Sleep(2 * time.Millisecond)
		counter.Add(-1)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if !WaitForZero(ctx, &counter, time.Second) {
		t.Errorf("Expected WaitForZero to notice the counter was already zero when ctx.Done() fired, not report a timeout")
	}
}
