package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gateway/internal/blackbox"
)

// echoUpstream returns a test double backend that echoes method + path + body
// plus the X-Test header value, so tests can assert the gateway genuinely
// proxied the request including custom headers.
func echoUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %s body=%s hdr=%s", r.Method, r.URL.Path, body, r.Header.Get("X-Test"))
	}))
	t.Cleanup(s.Close)
	return s
}

func proxyConfig(upstreamURL string) string {
	return fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: %q
    timeout: 5s
`, upstreamURL)
}

func TestProxyProxiesRequest(t *testing.T) {
	up := echoUpstream(t)
	s := blackbox.Start(t, proxyConfig(up.URL))

	req, err := http.NewRequest(http.MethodPost, s.URL()+"/svc/hello?x=1", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Test", "custom-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	// Method, body and a custom header preserved; path prefixed by the route.
	want := "POST /svc/hello body=payload hdr=custom-value"
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestRouteNotFound(t *testing.T) {
	up := echoUpstream(t)
	s := blackbox.Start(t, proxyConfig(up.URL))

	resp, err := http.Get(s.URL() + "/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnreachableUpstreamReturns502(t *testing.T) {
	// Point at a closed port so the dial fails.
	dead := httptest.NewServer(http.NewServeMux())
	addr := dead.URL
	dead.Close()
	s := blackbox.Start(t, proxyConfig(addr))

	resp, err := http.Get(s.URL() + "/svc/boom")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func TestSlowUpstreamReturns504OnTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond within the tiny route timeout; return when the gateway
		// (or client) closes the connection so cleanup does not hang.
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)
	cfg := fmt.Sprintf(`
server:
  listen: ":0"
routes:
  - path: /slow
    upstreams:
      - url: %q
    timeout: 50ms
`, slow.URL)
	s := blackbox.Start(t, cfg)

	resp, err := http.Get(s.URL() + "/slow/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 on timeout", resp.StatusCode)
	}
}
