// Package reload watches a config file and applies changes without restarting
// the process. Reloading is fail-closed: if the new content fails to parse or
// validate, the previous config stays active and the error is logged.
package reload

import (
	"context"
	"crypto/sha256"
	"log"
	"os"
	"time"

	"gateway/internal/config"
)

// Watch polls path every interval. On a content change it parses the config and
// calls apply with the parsed result; on a parse/validation error the previous
// config is retained (fail-closed) and the error is logged. It returns when ctx
// is cancelled.
func Watch(ctx context.Context, path string, interval time.Duration, apply func(*config.Config)) {
	var last []byte
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("reload: read %s: %v", path, err)
				continue
			}
			if hashEqual(last, data) {
				continue
			}
			cfg, err := config.Parse(data)
			if err != nil {
				log.Printf("reload: keeping previous config: %v", err)
				continue
			}
			apply(cfg)
			last = data
			log.Printf("reload: applied new config (%d routes)", len(cfg.Routes))
		}
	}
}

func hashEqual(a, b []byte) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return sha256.Sum256(a) == sha256.Sum256(b)
}
