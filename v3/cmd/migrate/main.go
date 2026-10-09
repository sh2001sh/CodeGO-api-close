// Command migrate applies v3 schema and performs staged online or atomic offline v2 migration.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/legacy"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		if errors.Is(err, legacy.ErrSourceDrainBlocked) {
			os.Exit(3)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	command, apply, err := parseMigrateArgs(args)
	if err != nil {
		if len(args) > 0 && args[0] == "drain" {
			report, _ := legacy.NewSourceDrainReport(legacy.SourceDrainOptions{})
			report.ErrorCode = "invalid_drain_arguments"
			if writeErr := json.NewEncoder(output).Encode(report); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	if command == "files" {
		return runFiles(ctx, apply, output)
	}
	if command == "background" {
		return runBackground(ctx, apply, output)
	}
	if command == "drain" {
		return runDrain(ctx, output)
	}
	if isOnlineCommand(command) {
		return runOnline(ctx, command, apply, output)
	}
	pool, err := pg.Connect(ctx, pg.Config{DSN: os.Getenv("V3_PG_DSN"), MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()
	switch command {
	case "schema":
		return applySchema(ctx, pool.Pool, output)
	case "ledger-check":
		return checkLedger(ctx, pool.Pool, output)
	}
	return runLegacyImport(ctx, command, apply, pool.Pool, output)
}

// parseMigrateArgs validates the migrate subcommand and its -apply/-offline
// flags, returning the subcommand name and whether -apply was set.
func parseMigrateArgs(args []string) (command string, apply bool, err error) {
	if len(args) == 0 {
		return "", false, errors.New("usage: migrate schema | drain | files [-apply -offline] | background [-apply -offline] | import [-apply -offline] | check | ledger-check | online-{prepare,copy,sync,verify,seal,unseal,finalize,backup,delta,restore-delta}")
	}
	flags := flag.NewFlagSet("migrate "+args[0], flag.ContinueOnError)
	applyFlag := flags.Bool("apply", false, "commit the import; default is dry-run")
	offline := flags.Bool("offline", false, "assert that all v2 writers are stopped")
	if err := flags.Parse(args[1:]); err != nil {
		return "", false, err
	}
	if flags.NArg() != 0 {
		return "", false, errors.New("unexpected arguments")
	}
	if isOnlineCommand(args[0]) {
		if err := validateOnlineFlags(args[0], *applyFlag, *offline); err != nil {
			return "", false, err
		}
		return args[0], *applyFlag, nil
	}
	if args[0] != "schema" && args[0] != "drain" && args[0] != "files" && args[0] != "background" && args[0] != "import" && args[0] != "check" && args[0] != "ledger-check" {
		return "", false, errors.New("unknown command; use schema, drain, files, background, import, check or ledger-check")
	}
	if args[0] == "drain" && (*applyFlag || *offline) {
		return "", false, errors.New("drain is source-only and accepts no apply or offline flags")
	}
	if *applyFlag && !*offline {
		return "", false, errors.New("apply requires -offline after stopping all v2 writers")
	}
	return args[0], *applyFlag, nil
}

// runLegacyImport runs the v2 Postgres importer in "check" or "import" mode
// and writes its report as indented JSON before returning any import error,
// so a partial report is still visible on failure.
func runLegacyImport(ctx context.Context, command string, apply bool, pool *pgxpool.Pool, output io.Writer) error {
	crypto, err := catalog.NewAESGCMFromBase64(os.Getenv("V3_SECRET_KEY"))
	if err != nil {
		return err
	}
	sourceDSN := os.Getenv("V3_SOURCE_PG_DSN")
	if sourceDSN == "" {
		return errors.New("V3_SOURCE_PG_DSN must identify the read-only v2 source database")
	}
	source, err := pg.Connect(ctx, pg.Config{DSN: sourceDSN, MaxConns: 2})
	if err != nil {
		return fmt.Errorf("source postgres: %w", err)
	}
	defer source.Close()
	sourceSecret := os.Getenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET")
	if sourceSecret == "" {
		sourceSecret = os.Getenv("LEGACY_CRYPTO_SECRET")
	}
	importer := legacy.NewImporter(source.Pool, pool, crypto).WithSourceCryptoSecret(sourceSecret)
	var report legacy.Report
	var importErr error
	if command == "check" {
		report, importErr = importer.Check(ctx)
	} else {
		report, importErr = importer.Import(ctx, apply)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		return err
	}
	return importErr
}

func checkLedger(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	var accounts, differences int64
	err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE a.balance<>COALESCE(e.total,0))
		FROM v3_billing.accounts a LEFT JOIN(SELECT account_id,sum(amount) total FROM v3_billing.ledger_entries GROUP BY account_id)e ON e.account_id=a.id`).Scan(&accounts, &differences)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(output, "accounts=%d ledger_balance_differences=%d\n", accounts, differences); err != nil {
		return err
	}
	if differences != 0 {
		return errors.New("ledger reconciliation failed")
	}
	return nil
}
