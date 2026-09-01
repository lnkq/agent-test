package e2e

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gateway/internal/blackbox"
)

func TestMetricsExposeRequestObservations(t *testing.T) {
	up := echoUpstream(t)
	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
rate_limits:
  tight:
    rate: 1
    burst: 1
routes:
  - path: /svc
    upstreams:
      - url: %q
  - path: /limited
    upstreams:
      - url: %q
    limit: tight
`, up.URL, up.URL))

	// Generate traffic: a few proxied requests plus a 429 (burst = 1, so the
	// first hit passes and the immediate second is rejected).
	for i := 0; i < 5; i++ {
		if _, err := http.Get(s.URL() + "/svc/x"); err != nil {
			t.Fatalf("request: %v", err)
		}
	}
	http.Get(s.URL() + "/limited/x")
	http.Get(s.URL() + "/limited/x")

	// The up/status gauge is refreshed on the health ticker, so poll until all
	// expected metric families appear rather than sampling once.
	waitFor(t, 5*time.Second, func() bool {
		resp, err := http.Get(s.URL() + "/metrics")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return false
		}
		for _, want := range []string{
			"gateway_request_duration_seconds",
			"gateway_upstream_requests_total",
			"gateway_rate_limited_total",
			"gateway_upstream_up",
		} {
			if !strings.Contains(string(body), want) {
				return false
			}
		}
		return true
	})
}
