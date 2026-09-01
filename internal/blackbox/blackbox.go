// Package blackbox hosts the single test seam for the whole project: it builds
// the real gateway binary and drives it over the HTTP boundary with an
// injected config file. Tests assert external behaviour only — internal
// implementation is deliberately not observed.
package blackbox

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// binary builds cmd/server once per test run and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gateway-bin-*")
		if err != nil {
			buildErr = fmt.Errorf("temp dir: %w", err)
			return
		}
		binPath = filepath.Join(dir, "gateway")
		out, err := exec.Command("go", "build", "-o", binPath, "gateway/cmd/server").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build gateway: %v: %s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("binary: %v", buildErr)
	}
	return binPath
}

// Server is a running gateway instance under test.
type Server struct {
	t       *testing.T
	cmd     *exec.Cmd
	baseURL string
	cfgPath string
}

// freePort reserves and releases a TCP port so a test server can bind to it.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// Start boots a gateway with the given config content on a free port and waits
// until /healthz is ready. The process is terminated on test cleanup. Extra
// args (e.g. health-probe flags) are appended to the command line.
func Start(t *testing.T, cfg string, extraArgs ...string) *Server {
	t.Helper()
	bin := binary(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	listen := freePort(t)
	args := append([]string{"-config", cfgPath, "-listen", listen}, extraArgs...)
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gateway: %v", err)
	}

	s := &Server{t: t, cmd: cmd, baseURL: "http://" + listen, cfgPath: cfgPath}
	t.Cleanup(s.stop)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("gateway did not become ready on %s", listen)
	return nil
}

func (s *Server) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

// URL returns the base URL of the server.
func (s *Server) URL() string { return s.baseURL }

// CfgPath returns the path of the config file the server was started with, so
// tests can rewrite it to exercise hot-reload.
func (s *Server) CfgPath() string { return s.cfgPath }

// RewriteConfig overwrites the running server's config file with new content.
func (s *Server) RewriteConfig(content string) {
	s.t.Helper()
	if err := os.WriteFile(s.cfgPath, []byte(content), 0o644); err != nil {
		s.t.Fatalf("rewrite config: %v", err)
	}
}

// Exec runs the gateway binary once to completion and returns combined output
// and the exit error. Used to assert fail-closed behaviour on startup.
func Exec(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binary(t), args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
