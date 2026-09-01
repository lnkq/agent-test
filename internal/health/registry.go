// Package health actively probes gateway upstreams and tracks which are
// currently usable. A target is excluded once it has failed a configurable
// number of consecutive probes and is re-included again after a single success.
//
// TODO(multi-instance): replace the static Set() with service discovery so
// upstreams can be added/removed as machines scale.
package health

import (
	"context"
	"net"
	"net/url"
	"sync"
	"time"
)

// ProbeTimeout bounds each individual TCP probe.
const ProbeTimeout = 500 * time.Millisecond

// Registry holds the liveness state of every known upstream URL.
type Registry struct {
	mu         sync.Mutex
	targets    map[string]*target
	interval   time.Duration
	failThresh int
	wg         sync.WaitGroup
}

type target struct {
	failures int
	healthy  bool
}

// New builds a registry that probes every interval and drops a target after
// failThreshold consecutive failures.
func New(interval time.Duration, failThreshold int) *Registry {
	return &Registry{
		targets:    make(map[string]*target),
		interval:   interval,
		failThresh: failThreshold,
	}
}

// Set reconciles the desired set of upstream URLs, adding new ones (initially
// healthy so traffic can flow before the first probe) and forgetting removed
// ones.
func (r *Registry) Set(urls []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]bool, len(urls))
	for _, u := range urls {
		seen[u] = true
		if _, ok := r.targets[u]; !ok {
			r.targets[u] = &target{healthy: true}
		}
	}
	for u := range r.targets {
		if !seen[u] {
			delete(r.targets, u)
		}
	}
}

// Healthy reports whether the given upstream URL is currently usable.
func (r *Registry) Healthy(urlStr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.targets[urlStr]
	return ok && t.healthy
}

// Snapshot returns a copy of the current healthy/unhealthy state keyed by
// upstream URL, used to refresh the up/status gauges.
func (r *Registry) Snapshot() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.targets))
	for k, t := range r.targets {
		out[k] = t.healthy
	}
	return out
}

// Start launches the probe loop in the background; it stops when ctx ends.
func (r *Registry) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.run(ctx)
}

// Run is the reload-aware probe loop that runs until ctx is cancelled.
func (r *Registry) run(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.probeAll()
		}
	}
}

func (r *Registry) probeAll() {
	type entry struct{ key string }
	var entries []entry
	r.mu.Lock()
	for k := range r.targets {
		entries = append(entries, entry{key: k})
	}
	r.mu.Unlock()

	for _, e := range entries {
		ok := probe(e.key)
		r.mu.Lock()
		st, exists := r.targets[e.key]
		if !exists {
			r.mu.Unlock()
			continue
		}
		if ok {
			st.failures = 0
			st.healthy = true
		} else {
			st.failures++
			if st.failures >= r.failThresh {
				st.healthy = false
			}
		}
		r.mu.Unlock()
	}
}

// probe reports whether a plain TCP connection to the URL's host succeeds.
func probe(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", u.Host, ProbeTimeout)
	if err != nil {
		return false
	}
	return conn.Close() == nil
}
