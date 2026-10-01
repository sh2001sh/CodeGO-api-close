// Command control serves the v3 administration and account API.
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
)

func main() {
	addr := flag.String("addr", "127.0.0.1:3002", "listen address")
	assets := flag.String("assets", "web/app/dist", "built application directory")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, log, *addr, *assets); err != nil {
		fmt.Fprintln(os.Stderr, "control:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, addr, assets string) error {
	cfg, err := configFromEnv()
	if err != nil {
		return err
	}
	deps, err := boot.Open(ctx, 32)
	if err != nil {
		return err
	}
	defer deps.Close()
	handler, err := controlHandler(deps, cfg, assets, log)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 64 << 10}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	log.Info("control listening", "addr", addr)
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	log.Info("control stopped")
	return err
}
