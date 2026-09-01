// Package ratelimit provides a per-instance token bucket limiter used by the
// gateway's routes. Limiting is deliberately per gateway process; distributing
// limits across instances is out of scope for this example.
//
// TODO(multi-instance): for a cluster-wide limit, back the bucket with a shared
// store (e.g. a Redis token pool) instead of in-process state.
package ratelimit

import (
	"sync"
	"time"
)

// Bucket is a token bucket that refills at [tokens per second] up to a burst
// ceiling. One request consumes one token; when fewer than one token is
// available the request is denied.
type Bucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

// New creates a bucket with the given refill rate and burst capacity.
func New(rate float64, burst int) *Bucket {
	return &Bucket{
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		now:    time.Now,
	}
}

// Allow reports whether a request may proceed, consuming a token if so.
// It is safe for concurrent use.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if b.last.IsZero() {
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
