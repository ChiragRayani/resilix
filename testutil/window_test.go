package testutil

import (
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

func TestCountWindow_FailureRateAndReset(t *testing.T) {
	w := resilix.NewCountWindow(4)
	w.Record(true)
	w.Record(false)
	w.Record(false)

	if w.Total() != 3 {
		t.Fatalf("expected total 3, got %d", w.Total())
	}
	if r := w.FailureRate(); r < 2.0/3.0-1e-9 || r > 2.0/3.0+1e-9 {
		t.Fatalf("expected failure rate 2/3, got %f", r)
	}

	w.Reset()
	if w.Total() != 0 || w.FailureRate() != 0 {
		t.Fatalf("after Reset expected empty window, total=%d rate=%f", w.Total(), w.FailureRate())
	}
}

func TestAtomicCounter_ConsecutiveFailuresResetsOnSuccess(t *testing.T) {
	var c resilix.AtomicCounter
	c.Record(false)
	c.Record(false)
	if c.ConsecutiveFailures() != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", c.ConsecutiveFailures())
	}
	c.Record(true)
	if c.ConsecutiveFailures() != 0 {
		t.Fatalf("expected reset after success, got %d", c.ConsecutiveFailures())
	}
	if c.Total() != 3 {
		t.Fatalf("expected total 3, got %d", c.Total())
	}
}

func TestSlidingWindow_FailureRate(t *testing.T) {
	clock := NewFakeClock(time.Now())
	w := resilix.NewSlidingWindow(60*time.Second, 6)
	w.SetClock(clock)

	w.Record(false)
	w.Record(false)
	w.Record(true)
	w.Record(true)

	rate := w.FailureRate()
	if rate != 0.5 {
		t.Fatalf("expected 0.5, got %f", rate)
	}

	clock.Advance(61 * time.Second)
	w.Record(true)

	rate = w.FailureRate()
	if rate != 0.0 {
		t.Fatalf("expected 0.0 after eviction, got %f", rate)
	}
}

func TestSlidingWindow_SetClockBracketsBuckets(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(start)
	w := resilix.NewSlidingWindow(10*time.Second, 2)
	w.SetClock(clock)

	w.Record(true)
	clock.Advance(6 * time.Second)
	w.Record(false)

	if w.FailureRate() <= 0 || w.Total() == 0 {
		t.Fatalf("expected mixed results in window, rate=%f total=%d", w.FailureRate(), w.Total())
	}
}
