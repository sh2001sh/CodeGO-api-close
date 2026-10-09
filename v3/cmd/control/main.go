// Command control serves the v3 administration and account API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:3002", "listen address")
	assets := flag.String("assets", "web/app/dist", "built application directory")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log, closeLog, err := boot.ProcessLogger("control", os.Getenv("V3_LOG_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "control logging:", err)
		os.Exit(1)
	}
	err = run(ctx, log, *addr, *assets)
	if closeErr := closeLog(); closeErr != nil {
		fmt.Fprintln(os.Stderr, "control logging close:", closeErr)
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
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
	if cfg.LedgerArchiveDSN != "" {
		cfg.LedgerArchive, err = openLedgerArchive(ctx, cfg.LedgerArchiveDSN)
		if err != nil {
			return err
		}
		defer cfg.LedgerArchive.Close()
	}
	archiveCtx, cancelArchive := context.WithTimeout(ctx, 10*time.Second)
	err = ledger.ValidateHistoryArchive(archiveCtx, deps.PG.Pool, cfg.LedgerArchive)
	cancelArchive()
	if err != nil {
		return err
	}
	handler, err := controlHandler(deps, cfg, assets, log)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 64 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
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

func openLedgerArchive(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// A parsing error can contain the supplied secret-bearing DSN.
		return nil, errors.New("V3_LEDGER_ARCHIVE_PG_DSN must be a valid PostgreSQL DSN")
	}
	cfg.MaxConns, cfg.MinConns = 4, 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	return pgxpool.NewWithConfig(ctx, cfg)
}
