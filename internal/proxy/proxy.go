// Package proxy implements the gateway's HTTP routing and proxying. It
// compiles an immutable snapshot of the config (so hot-reload can atomically
// swap it), does longest-prefix route matching, picks an upstream and reverse
// proxies the request with a per-route timeout.
package proxy

import (
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gateway/internal/config"
	"gateway/internal/health"
	"gateway/internal/metrics"
	"gateway/internal/ratelimit"
)

// DefaultTimeout applies when a route does not specify a timeout.
const DefaultTimeout = 30 * time.Second

// Snapshot is an immutable, compiled view of the config used by the handler.
// Reloads replace the whole snapshot atomically.
type Snapshot struct {
	routes []route
}

type target struct {
	raw    string // original URL string, key into the health registry
	url    *url.URL
	weight int
	rp     *httputil.ReverseProxy
}

type route struct {
	path    string
	targets []*target
	timeout time.Duration
	bucket  *ratelimit.Bucket
}

// Handler routes and proxies HTTP requests. Use Update to install a new
// compiled config snapshot.
type Handler struct {
	snap atomic.Pointer[Snapshot]
	rng  *rand.Rand
	reg  *health.Registry
	m    *metrics.Metrics
}

// New creates a Handler over the given health registry (may be nil to disable
// health-based filtering) and metrics collector (may be nil to skip
// instrumentation).
func New(reg *health.Registry, m *metrics.Metrics) *Handler {
	h := &Handler{rng: rand.New(rand.NewSource(time.Now().UnixNano())), reg: reg, m: m}
	h.snap.Store(&Snapshot{})
	return h
}

// Update atomically installs a new compiled config snapshot and reconciles the
// set of known upstream URLs in the health registry.
func (h *Handler) Update(cfg *config.Config) {
	h.snap.Store(Compile(cfg))
	if h.reg != nil {
		h.reg.Set(cfg.UpstreamURLs())
	}
}

// Compile turns a validated config into a runnable snapshot.
func Compile(cfg *config.Config) *Snapshot {
	routes := make([]route, 0, len(cfg.Routes))
	for _, r := range cfg.Routes {
		rt := route{path: r.Path, timeout: DefaultTimeout}
		if r.Timeout > 0 {
			rt.timeout = time.Duration(r.Timeout)
		}
		// A per-route transport bounds upstream latency via the route timeout.
		tr := &http.Transport{
			ResponseHeaderTimeout: rt.timeout,
			DialContext: (&net.Dialer{
				Timeout:   rt.timeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
		}
		for _, u := range r.Upstreams {
			uu, _ := url.Parse(u.URL)
			rp := httputil.NewSingleHostReverseProxy(uu)
			rp.Transport = tr
			// 504 for an upstream timeout; 502 for any other failure.
			rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					http.Error(w, http.StatusText(http.StatusGatewayTimeout), http.StatusGatewayTimeout)
					return
				}
				http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
			}
			rt.targets = append(rt.targets, &target{raw: u.URL, url: uu, weight: u.Weight, rp: rp})
		}
		if r.Limit != "" {
			if rl, ok := cfg.RateLimits[r.Limit]; ok {
				rt.bucket = ratelimit.New(rl.Rate, rl.Burst)
			}
		}
		routes = append(routes, rt)
	}
	// Longer prefixes match first; ordering is otherwise irrelevant.
	sort.Slice(routes, func(i, j int) bool { return len(routes[i].path) > len(routes[j].path) })
	return &Snapshot{routes: routes}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt := match(h.snap.Load().routes, r.URL.Path)
	if rt == nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if rt.bucket != nil && !rt.bucket.Allow() {
		if h.m != nil {
			h.m.MarkRateLimited(rt.path)
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}
	t := pickHealthy(h.rng, rt.targets, h.reg)
	if t == nil {
		http.Error(w, "no healthy upstream", http.StatusBadGateway)
		return
	}
	w.Header().Set("X-Upstream", t.raw)
	start := time.Now()
	t.rp.ServeHTTP(w, r)
	if h.m != nil {
		h.m.ObserveRequest(rt.path, t.raw, time.Since(start).Seconds())
	}
}

// match returns the route with the longest prefix matching path, or nil.
func match(routes []route, path string) *route {
	for i := range routes {
		if strings.HasPrefix(path, routes[i].path) {
			return &routes[i]
		}
	}
	return nil
}

// pick returns a target chosen by weighted random selection. Weighted choice is
// the single mechanism for both load balancing and canary.
func pick(rng *rand.Rand, targets []*target) *target {
	total := 0
	for _, t := range targets {
		total += t.weight
	}
	n := rng.Intn(total)
	for _, t := range targets {
		n -= t.weight
		if n < 0 {
			return t
		}
	}
	return targets[len(targets)-1]
}

// pickHealthy returns a weighted-random choice among targets the registry
// currently considers healthy, or nil if none are usable. A nil registry treats
// every target as healthy.
//
// TODO(multi-instance): canary client stickiness would hash a stable request
// attribute instead of picking at random.
func pickHealthy(rng *rand.Rand, targets []*target, reg *health.Registry) *target {
	pool := targets[:0:0]
	for _, t := range targets {
		if reg == nil || reg.Healthy(t.raw) {
			pool = append(pool, t)
		}
	}
	if len(pool) == 0 {
		return nil
	}
	return pick(rng, pool)
}
