package resilix

import (
	"sync"
	"sync/atomic"
	"time"
)

// --- Count-based window ---------------------------------------------------

// CountWindow uses a simple rolling count of the last N calls.
// It is the lightest option and suitable when call rate is stable.
type CountWindow struct {
	mu       sync.Mutex
	ring     []bool // circular buffer; true = failure
	head     int
	size     int
	failures int64
	total    int64
}

// NewCountWindow returns a window that tracks the last `size` calls.
func NewCountWindow(size int) *CountWindow {
	if size <= 0 {
		size = 10
	}
	return &CountWindow{ring: make([]bool, size), size: size}
}

func (w *CountWindow) Record(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// evict the oldest slot
	if w.total >= int64(w.size) {
		if w.ring[w.head] {
			w.failures--
		}
	} else {
		w.total++
	}

	w.ring[w.head] = !success
	if !success {
		w.failures++
	}
	w.head = (w.head + 1) % w.size
}

func (w *CountWindow) FailureRate() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.total == 0 {
		return 0
	}
	return float64(w.failures) / float64(w.total)
}

func (w *CountWindow) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ring = make([]bool, w.size)
	w.head = 0
	w.failures = 0
	w.total = 0
}

func (w *CountWindow) Total() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}

// --- Sliding time-based window -------------------------------------------

// bucket is one time slice inside the sliding window.
type bucket struct {
	ts       time.Time
	failures int64
	total    int64
}

// SlidingWindow tracks calls within a rolling time range divided into buckets.
// It is more accurate than CountWindow for high-variance traffic.
type SlidingWindow struct {
	mu           sync.Mutex
	buckets      []bucket
	bucketWidth  time.Duration
	windowSize   time.Duration
	numBuckets   int
	currentIndex int
	clock        Clock
}

// NewSlidingWindow creates a window of the given duration split into `buckets` slots.
// Example: NewSlidingWindow(60*time.Second, 6) → 6×10s buckets, 60s total.
func NewSlidingWindow(window time.Duration, numBuckets int) *SlidingWindow {
	if numBuckets <= 0 {
		numBuckets = 10
	}
	return &SlidingWindow{
		buckets:     make([]bucket, numBuckets),
		bucketWidth: window / time.Duration(numBuckets),
		windowSize:  window,
		numBuckets:  numBuckets,
		clock:       defaultClock,
	}
}

func (w *SlidingWindow) currentBucket(now time.Time) *bucket {
	// advance the ring if current bucket has expired
	cur := &w.buckets[w.currentIndex]
	if now.Sub(cur.ts) >= w.bucketWidth {
		// rotate forward, clearing stale buckets
		w.currentIndex = (w.currentIndex + 1) % w.numBuckets
		cur = &w.buckets[w.currentIndex]
		cur.ts = now
		cur.failures = 0
		cur.total = 0
	}
	return cur
}

func (w *SlidingWindow) evictOld(now time.Time) {
	cutoff := now.Add(-w.windowSize)
	for i := range w.buckets {
		if !w.buckets[i].ts.IsZero() && w.buckets[i].ts.Before(cutoff) {
			w.buckets[i].failures = 0
			w.buckets[i].total = 0
		}
	}
}

func (w *SlidingWindow) Record(success bool) {
	now := w.clock.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evictOld(now)
	b := w.currentBucket(now)
	b.total++
	if !success {
		b.failures++
	}
}

func (w *SlidingWindow) FailureRate() float64 {
	now := w.clock.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evictOld(now)

	var total, failures int64
	for i := range w.buckets {
		total += w.buckets[i].total
		failures += w.buckets[i].failures
	}
	if total == 0 {
		return 0
	}
	return float64(failures) / float64(total)
}

func (w *SlidingWindow) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buckets = make([]bucket, w.numBuckets)
	w.currentIndex = 0
}

func (w *SlidingWindow) Total() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	var total int64
	for i := range w.buckets {
		total += w.buckets[i].total
	}
	return total
}

// SetClock allows injecting a fake clock in tests to control time for sliding window boundaries.
func (w *SlidingWindow) SetClock(clock Clock) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.clock = clock
}

// --- Atomic count-based threshold (lightweight, no window) ---------------

// AtomicCounter is the simplest possible failure counter:
// just a running total, reset on success. Use when you want
// "N consecutive failures" semantics rather than a rate.
type AtomicCounter struct {
	failures atomic.Int64
	total    atomic.Int64
}

func (c *AtomicCounter) Record(success bool) {
	c.total.Add(1)
	if success {
		c.failures.Store(0)
	} else {
		c.failures.Add(1)
	}
}

func (c *AtomicCounter) FailureRate() float64 {
	t := c.total.Load()
	if t == 0 {
		return 0
	}
	return float64(c.failures.Load()) / float64(t)
}

func (c *AtomicCounter) Reset() {
	c.failures.Store(0)
	c.total.Store(0)
}

func (c *AtomicCounter) Total() int64 { return c.total.Load() }

// ConsecutiveFailures returns the current consecutive failure count.
func (c *AtomicCounter) ConsecutiveFailures() int64 { return c.failures.Load() }
