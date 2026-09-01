package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway/internal/blackbox"
)

// namedUpstream returns a test double that reports its own name, so tests can
// tell which upstream the gateway routed to.
func namedUpstream(t *testing.T, name string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s", name)
	}))
	t.Cleanup(s.Close)
	return s
}

// bodyOf returns the response body for a GET to the gateway's URL. It fails the
// test on transport or status errors.
func bodyOf(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d body=%s", url, resp.StatusCode, b)
	}
	return string(b)
}

// waitFor polls fn until it returns true, failing after timeout.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestHotReloadAppliesWithoutRestart(t *testing.T) {
	a := namedUpstream(t, "A")
	b := namedUpstream(t, "B")

	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
`, a.URL))

	if got := bodyOf(t, s.URL()+"/svc/x"); got != "A" {
		t.Fatalf("initial route served %q, want A", got)
	}

	// Repoint the same route at B and wait for the reload to take effect —
	// without restarting the process (same base URL, /healthz still alive).
	s.RewriteConfig(fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
`, b.URL))

	waitFor(t, 5*time.Second, func() bool { return bodyOf(t, s.URL()+"/svc/x") == "B" })
	if bodyOf(t, s.URL()+"/healthz") != "ok" {
		t.Fatalf("gateway did not stay healthy after reload")
	}
}

func TestHotReloadFailClosedKeepsPreviousConfig(t *testing.T) {
	a := namedUpstream(t, "A")
	s := blackbox.Start(t, fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
`, a.URL))

	// Write a broken config: the previous (valid) one must keep serving.
	s.RewriteConfig(`
server:
  listen: ":0"
routes:
  - path: "no-leading-slash"
    upstreams: []
`)
	time.Sleep(1200 * time.Millisecond)
	if got := bodyOf(t, s.URL()+"/svc/x"); got != "A" {
		t.Fatalf("after broken reload served %q, want A (previous config)", got)
	}
}
