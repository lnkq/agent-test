package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway/internal/blackbox"
)

// healthFlags makes probes quick so exclusion/recovery is observable in tests.
var healthFlags = []string{"-health-interval=100ms", "-health-threshold=2"}

func upstreamHeader(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.Header.Get("X-Upstream")
}

func TestCanarySplitsTrafficAcrossWeightedUpstreams(t *testing.T) {
	a := namedUpstream(t, "A")
	b := namedUpstream(t, "B")
	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /canary
    upstreams:
      - url: %q
        weight: 1
      - url: %q
        weight: 1
`, a.URL, b.URL), healthFlags...)

	seen := map[string]bool{}
	for i := 0; i < 120; i++ {
		up := upstreamHeader(t, s.URL()+"/canary/x")
		seen[up] = true
		if up != a.URL && up != b.URL {
			t.Fatalf("X-Upstream = %q, want one of the configured upstreams", up)
		}
	}
	if !seen[a.URL] || !seen[b.URL] {
		t.Fatalf("expected both upstreams to be used, saw %v", seen)
	}
}

func TestDeadUpstreamExcludedFromPool(t *testing.T) {
	a := namedUpstream(t, "A")
	dead := httptest.NewServer(http.NewServeMux())
	deadURL := dead.URL
	dead.Close() // kill one upstream

	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
        weight: 1
      - url: %q
        weight: 1
`, a.URL, deadURL), healthFlags...)

	// Wait until the health registry excludes the dead upstream. Requiring 25
	// consecutive requests to all land on A makes a passing (random) selection
	// essentially impossible until the dead one is actually excluded.
	waitFor(t, 5*time.Second, func() bool {
		for i := 0; i < 25; i++ {
			if up := upstreamHeader(t, s.URL()+"/svc/x"); up != a.URL {
				return false
			}
		}
		return true
	})
	for i := 0; i < 20; i++ {
		if up := upstreamHeader(t, s.URL()+"/svc/x"); up != a.URL {
			t.Fatalf("request went to dead upstream %q", up)
		}
	}
}

func TestAllUpstreamsDownReturns502(t *testing.T) {
	deadA := httptest.NewServer(http.NewServeMux())
	deadB := httptest.NewServer(http.NewServeMux())
	a := deadA.URL
	b := deadB.URL
	deadA.Close()
	deadB.Close()

	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
      - url: %q
`, a, b), healthFlags...)

	// Wait until both are marked unhealthy, then the route must 502.
	waitFor(t, 5*time.Second, func() bool {
		resp, err := http.Get(s.URL() + "/svc/x")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusBadGateway
	})
}
