// Command gateway serves the v3 relay API.
//
//	gateway -addr :3000                      # production wiring, see cmd/internal/boot for env
//	gateway -addr :3000 -bench-upstream URL  # bench mode: static key and route, billing off
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:3000", "listen address")
	benchUpstream := flag.String("bench-upstream", "", "bench mode: route every request to this upstream base URL")
	benchKey := flag.String("bench-key", "sk-bench", "bench mode: the only accepted API key")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log, closeLog, err := boot.ProcessLogger("gateway", os.Getenv("V3_LOG_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gateway logging:", err)
		os.Exit(1)
	}
	err = run(ctx, log, *addr, *benchUpstream, *benchKey)
	if closeErr := closeLog(); closeErr != nil {
		fmt.Fprintln(os.Stderr, "gateway logging close:", closeErr)
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, addr, benchUpstream, benchKey string) error {
	var handler http.Handler
	if benchUpstream != "" {
		log.Warn("bench mode: static key, single route, billing disabled", "upstream", benchUpstream)
		g, err := gateway.New(benchDeps(benchUpstream, benchKey))
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		g.Register(mux)
		handler = mux
	} else {
		deps, err := boot.Open(ctx, 32)
		if err != nil {
			return err
		}
		defer deps.Close()
		var closeFn func()
		if handler, closeFn, err = prodHandler(ctx, deps, log); err != nil {
			return err
		}
		defer closeFn() // runs after serve returns, i.e. after in-flight requests drained
	}
	return serve(ctx, log, addr, handler)
}

func serve(ctx context.Context, log *slog.Logger, addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("gateway listening", "addr", addr)

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Stop outstanding HTTP connections; production cleanup still waits for
		// bounded drain/finalization before closing the WAL or dependencies.
		return errors.Join(err, srv.Close())
	}
	return nil
}
