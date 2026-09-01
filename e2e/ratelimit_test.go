package e2e

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"gateway/internal/blackbox"
)

func TestRateLimitReturns429AfterBurst(t *testing.T) {
	up := echoUpstream(t)
	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
rate_limits:
  strict:
    rate: 1
    burst: 1
routes:
  - path: /limited
    upstreams:
      - url: %q
    limit: strict
`, up.URL))

	status := func() int {
		resp, err := http.Get(s.URL() + "/limited/x")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	// First hit consumes the single burst token.
	if got := status(); got != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", got)
	}
	// Immediate second hit must be denied — no token refilled yet.
	if got := status(); got != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", got)
	}
	// After more than one/second, the token refills and the request passes.
	time.Sleep(1100 * time.Millisecond)
	if got := status(); got != http.StatusOK {
		t.Fatalf("request after refill status = %d, want 200", got)
	}
}

func TestUnlimitedRouteIsNotThrottled(t *testing.T) {
	up := echoUpstream(t)
	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /open
    upstreams:
      - url: %q
`, up.URL))

	for i := 0; i < 20; i++ {
		if got := bodyOf(t, s.URL()+"/open/x"); got == "" {
			t.Fatalf("unlimited route throttled at iteration %d", i)
		}
	}
}
