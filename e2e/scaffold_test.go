// Package e2e verifies the gateway through the single HTTP-boundary seam
// (internal/blackbox): the real binary is built and driven over HTTP with an
// injected config. Only external behaviour is asserted.
package e2e

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gateway/internal/blackbox"
)

const minimalConfig = `
server:
  listen: ":0"
`

func TestHealthz(t *testing.T) {
	s := blackbox.Start(t, minimalConfig)

	resp, err := http.Get(s.URL() + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}

func TestNegativeTimeoutFailsClosed(t *testing.T) {
	bad := `
server:
  listen: ":0"
routes:
  - path: /svc
    upstreams:
      - url: "http://127.0.0.1:1"
    timeout: -1s
`
	cfgPath := filepath.Join(t.TempDir(), "bad.yml")
	if err := os.WriteFile(cfgPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := blackbox.Exec(t, "-config", cfgPath)
	if err == nil {
		t.Fatalf("expected startup to fail on negative timeout, got success: %s", out)
	}
	if !strings.Contains(out, "timeout must not be negative") {
		t.Fatalf("error output should mention the negative timeout, got: %s", out)
	}
}

func TestInvalidConfigFailsClosed(t *testing.T) {
	bad := `
server:
  listen: ":0"
routes:
  - path: "no-leading-slash"
    upstreams:
      - url: "http://127.0.0.1:1"
`
	cfgPath := filepath.Join(t.TempDir(), "bad.yml")
	if err := os.WriteFile(cfgPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := blackbox.Exec(t, "-config", cfgPath)
	if err == nil {
		t.Fatalf("expected startup to fail on invalid config, got success: %s", out)
	}
	if !strings.Contains(out, "path must be non-empty") {
		t.Fatalf("error output should mention validation problem, got: %s", out)
	}
}
