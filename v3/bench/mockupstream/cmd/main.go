// Command mockupstream serves the fake upstream for k6 runs:
//
//	go run ./bench/mockupstream/cmd -addr :18080
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.Handle("POST /", mockupstream.Handler())
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	slog.Info("mock upstream listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("mock upstream stopped", "err", err)
		os.Exit(1)
	}
}
