package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"gateway/internal/blackbox"
)

func TestFrontendServed(t *testing.T) {
	s := blackbox.Start(t, minimalConfig)

	resp, err := http.Get(s.URL() + "/ui/")
	if err != nil {
		t.Fatalf("get /ui/: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ui/ status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Gateway Tester") {
		t.Errorf("/ui/ body missing app title")
	}

	js, err := http.Get(s.URL() + "/ui/app.js")
	if err != nil {
		t.Fatalf("get /ui/app.js: %v", err)
	}
	defer js.Body.Close()
	if js.StatusCode != http.StatusOK {
		t.Fatalf("/ui/app.js status = %d, want 200", js.StatusCode)
	}
}
