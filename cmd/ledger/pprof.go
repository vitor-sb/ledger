package main

import (
	"net/http"
	"net/http/pprof"
	"time"
)

// newPprofServer serves the runtime profiles on their own listener. It is opt-in
// (PPROF_ADDR) and kept apart from the API on purpose: profiles expose internals and
// must never be reachable through the public port. Use a loopback address, e.g.
// PPROF_ADDR=127.0.0.1:6060.
//
// Importing net/http/pprof already registers its handlers on http.DefaultServeMux (in its
// init), so that mux must never be served. The API uses its own mux (httpapi.NewHandler)
// and this server a private one; TestAPIDoesNotServeProfiles guards that.
func newPprofServer(address string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	// No WriteTimeout: /profile?seconds=N holds the response open for N seconds.
	return &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}
