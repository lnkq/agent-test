// Command upstream is a demo backend service used to exercise the gateway. It
// responds to any path with its own name, the request path and method, so a
// caller (or the gateway) can tell which upstream served the request.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"gateway/internal/shutdown"
)

func main() {
	name := flag.String("name", "upstream", "service name reported in responses")
	listen := flag.String("listen", "0.0.0.0:8080", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service": *name,
			"path":    r.URL.Path,
			"method":  r.Method,
		})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: *listen, Handler: mux}
	shutdown.OnSignal(srv)

	log.Printf("upstream %q listening on %s", *name, *listen)
	if err := shutdown.Serve(srv); err != nil {
		log.Fatalf("server: %v", err)
	}
}
