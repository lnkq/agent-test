// Package shutdown provides a small, shared graceful-shutdown helper for the
// gateway and the demo upstream binaries.
package shutdown

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// OnSignal arranges for srv to be closed gracefully when SIGINT or SIGTERM is
// received. It must be called before Serve.
func OnSignal(srv *http.Server) {
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Print("shutting down")
		_ = srv.Close()
	}()
}

// Serve blocks serving srv, returning nil on a graceful shutdown or the server
// error otherwise.
func Serve(srv *http.Server) error {
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
