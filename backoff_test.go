package resilix

import (
	"testing"
	"time"
)

func TestConstantBackoff(t *testing.T) {
	b := ConstantBackoff(7 * time.Millisecond)
	if d := b(1); d != 7*time.Millisecond {
		t.Fatalf("attempt 1: got %v", d)
	}
	if d := b(99); d != 7*time.Millisecond {
		t.Fatalf("attempt 99: got %v", d)
	}
}

func TestExponentialBackoff_Cap(t *testing.T) {
	b := ExponentialBackoff(time.Second, 2.0, 2500*time.Millisecond)
	if d := b(1); d != time.Second {
		t.Fatalf("attempt 1: got %v", d)
	}
	if d := b(3); d != 2500*time.Millisecond {
		t.Fatalf("attempt 3 should cap at 2500ms, got %v", d)
	}
}

func TestExponentialBackoff_NoCap(t *testing.T) {
	b := ExponentialBackoff(time.Millisecond, 10.0, 0)
	if d := b(3); d != 100*time.Millisecond {
		t.Fatalf("attempt 3: got %v want 100ms", d)
	}
}

func TestLinearBackoff_Cap(t *testing.T) {
	b := LinearBackoff(100*time.Millisecond, 50*time.Millisecond, 220*time.Millisecond)
	if d := b(1); d != 100*time.Millisecond {
		t.Fatalf("attempt 1: got %v", d)
	}
	if d := b(5); d != 220*time.Millisecond {
		t.Fatalf("attempt 5 should cap, got %v", d)
	}
}

func TestExponentialBackoffWithJitter_StaysNonNegative(t *testing.T) {
	b := ExponentialBackoffWithJitter(10*time.Millisecond, 2.0, time.Second, 0.5)
	for attempt := 1; attempt <= 50; attempt++ {
		if d := b(attempt); d < 0 {
			t.Fatalf("attempt %d: negative duration %v", attempt, d)
		}
	}
}
