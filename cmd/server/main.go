// Command gateway is the API gateway. It reads a YAML config, exposes a
// health endpoint now, and (in later tickets) proxies requests to weighted
// upstreams with rate limiting and canary support.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gateway/internal/config"
	"gateway/internal/frontend"
	"gateway/internal/health"
	"gateway/internal/metrics"
	"gateway/internal/proxy"
	"gateway/internal/reload"
	"gateway/internal/shutdown"
)

func main() {
	cfgPath := flag.String("config", "config.yml", "path to config file")
	listenFlag := flag.String("listen", "", "override server.listen address")
	healthInterval := flag.Duration("health-interval", time.Second, "upstream health probe interval")
	healthThreshold := flag.Int("health-threshold", 2, "consecutive failures before an upstream is excluded")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	listen := cfg.Server.Listen
	if *listenFlag != "" {
		listen = *listenFlag
	}

	// The mux is built here; /healthz is always available for readiness probes
	// and every other request is handled by the config-driven proxy router.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})

	// Health registry actively probes upstreams and is reconciliated on reload.
	reg := health.New(*healthInterval, *healthThreshold)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.Start(ctx)

	m := metrics.New()
	mux.Handle("/metrics", promhttp.Handler())

	// Embedded request-testing UI.
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
	mux.Handle("/ui/", http.StripPrefix("/ui", frontend.Handler()))
	// Refresh upstream up/status gauges from the health registry.
	go func() {
		t := time.NewTicker(*healthInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.UpdateUpstreams(reg.Snapshot())
			}
		}
	}()

	router := proxy.New(reg, m)
	router.Update(cfg)
	mux.Handle("/", router)

	// Hot-reload: apply config changes without restarting; fail-closed on error.
	go reload.Watch(ctx, *cfgPath, 500*time.Millisecond, router.Update)

	srv := &http.Server{
		Addr:    listen,
		Handler: mux,
	}

	shutdown.OnSignal(srv)
	log.Printf("loaded %d routes; gateway listening on %s", len(cfg.Routes), listen)
	if err := shutdown.Serve(srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
