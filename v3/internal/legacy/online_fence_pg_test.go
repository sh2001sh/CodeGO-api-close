//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type onlineFenceBarrier struct {
	arrived     chan struct{}
	release     chan struct{}
	queryPrefix string
	fired       atomic.Bool
	once        sync.Once
}

func (b *onlineFenceBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	prefix := b.queryPrefix
	if prefix == "" {
		prefix = "INSERT INTO v3_identity.users"
	}
	if strings.Contains(data.SQL, prefix) && b.fired.CompareAndSwap(false, true) {
		close(b.arrived)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*onlineFenceBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (b *onlineFenceBarrier) unblock()                                                      { b.once.Do(func() { close(b.release) }) }

func onlineFenceView(t *testing.T, target *pgxpool.Pool, runID string) *onlineView {
	t.Helper()
	ctx := context.Background()
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	view, err := onlineLoadView(ctx, tx, runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestOnlineFenceUnsealCannotRaceFinalCommit(t *testing.T) {
	for _, mode := range []string{"finalize", "private_import", "cancel_final"} {
		t.Run(mode, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			// Match the migration CLI pools. Final holds one source session and
			// must reuse it in Import rather than wait forever for a third slot.
			sourceConfig := source.Config().Copy()
			sourceConfig.MaxConns = 2
			boundedSource, err := pgxpool.NewWithConfig(ctx, sourceConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer boundedSource.Close()
			m.source = boundedSource
			opts.SourceAdmin = boundedSource
			onlineMigrationReady(t, m, opts)
			view := onlineFenceView(t, target, opts.RunID)
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			barrier := &onlineFenceBarrier{arrived: make(chan struct{}), release: make(chan struct{})}
			defer barrier.unblock()
			config := target.Config().Copy()
			config.MaxConns = 2
			config.ConnConfig.Tracer = barrier
			traced, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer traced.Close()
			candidate := NewImporter(boundedSource, traced, m.crypto)
			finalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer func() {
				cancel()
				barrier.unblock()
			}()
			type finalResult struct {
				report Report
				err    error
			}
			completed := make(chan finalResult, 1)
			go func() {
				var r Report
				var err error
				if mode == "private_import" {
					r, err = candidate.Import(context.WithValue(finalCtx, onlineContextKey{}, view), true)
				} else {
					r, err = candidate.FinalizeOnline(finalCtx, opts)
				}
				completed <- finalResult{r, err}
			}()
			select {
			case <-barrier.arrived:
			case r := <-completed:
				t.Fatalf("final ended before deterministic pre-commit barrier: %+v %v", r.report, r.err)
			case <-finalCtx.Done():
				t.Fatal("final did not reach its pre-commit barrier")
			}
			// This query is a separate source session and never uses target locks.
			if _, err = UnsealOnlineCapture(ctx, source, opts.RunID); err == nil {
				t.Fatal("source reopened while the target final transaction was pending")
			} else {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("unseal did not fail on the held source advisory lock: %v", err)
				}
			}
			var sealed bool
			if err := source.QueryRow(ctx, "SELECT sealed FROM v3_migration_capture.config WHERE singleton").Scan(&sealed); err != nil || !sealed {
				t.Fatalf("source fence changed during final sealed=%v err=%v", sealed, err)
			}
			if mode == "cancel_final" {
				cancel()
			}
			barrier.unblock()
			r := <-completed
			if mode == "cancel_final" {
				if r.err == nil || r.report.Applied {
					t.Fatal("cancelled final committed")
				}
				onlineRecoveryAssertNoMoney(t, target)
			} else if r.err != nil || !r.report.Applied {
				t.Fatalf("protected final failed: %+v %v", r.report, r.err)
			}
			if err := source.QueryRow(ctx, "SELECT sealed FROM v3_migration_capture.config WHERE singleton").Scan(&sealed); err != nil || !sealed {
				t.Fatalf("final failure/success automatically released its source fence sealed=%v err=%v", sealed, err)
			}
			if _, err := UnsealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatalf("completed/cancelled final leaked its session lock: %v", err)
			}
		})
	}
}

func TestOnlineFencePrivateImportRejectsUnsealedSource(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	onlineMigrationReady(t, m, opts)
	ctx := context.WithValue(context.Background(), onlineContextKey{}, onlineFenceView(t, target, opts.RunID))
	if _, err := m.Import(ctx, true); err == nil || !strings.Contains(err.Error(), "source fence was released") {
		t.Fatalf("private final import accepted an unfenced source: %v", err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

func TestOnlineFenceSourceIdentityRejectsWrongDatabaseBeforeCapture(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	config := source.Config().Copy()
	config.ConnConfig.Host = "localhost"
	alias, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer alias.Close()
	if err := ValidateOnlineCaptureSource(ctx, source, alias); err != nil {
		t.Fatalf("same true database via host alias rejected: %v", err)
	}
	if err := ValidateOnlineCaptureSource(ctx, source, target); err == nil || !strings.Contains(err.Error(), "different database") {
		t.Fatalf("different true database accepted: %v", err)
	}
	reader := readonlySource(t, source)
	onlineRecoveryExec(t, source, "REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC")
	if err := ValidateOnlineCaptureSource(ctx, reader, source); err == nil {
		t.Fatal("database identity silently degraded without pg_control_system privileges")
	}
	opts.SourceAdmin = target
	if _, err := m.PrepareOnline(ctx, opts, true); err == nil || !strings.Contains(err.Error(), "different database") {
		t.Fatalf("prepare wrote through mismatched source admin: %v", err)
	}
	for _, db := range []*pgxpool.Pool{source, target} {
		var capture bool
		if err := db.QueryRow(ctx, "SELECT to_regclass('v3_migration_capture.config') IS NOT NULL").Scan(&capture); err != nil || capture {
			t.Fatalf("mismatched admin left capture state=%v err=%v", capture, err)
		}
	}
}

func TestOnlineFenceConcurrentDDLBeforeFirstTargetLockRefusesAdoption(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	barrier := &onlineFenceBarrier{arrived: make(chan struct{}), release: make(chan struct{}), queryPrefix: "LOCK TABLE "}
	config := target.Config().Copy()
	config.MaxConns = 2
	config.ConnConfig.Tracer = barrier
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	finalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer func() {
		cancel()
		barrier.unblock()
	}()
	candidate := NewImporter(source, traced, m.crypto)
	completed := make(chan error, 1)
	go func() {
		_, err := candidate.FinalizeOnline(finalCtx, opts)
		completed <- err
	}()
	select {
	case <-barrier.arrived:
	case err := <-completed:
		t.Fatalf("final ended before its first target lock: %v", err)
	case <-finalCtx.Done():
		t.Fatal("final did not reach its first target lock")
	}
	// This DDL commits after discovery but before the target transaction's
	// first LOCK. Acquiring a SERIALIZABLE snapshot before that lock would
	// hide the new column from schema checks and adopt an older staging table.
	onlineRecoveryExec(t, target, "ALTER TABLE v3_billing.historical_entries ADD COLUMN cutover_probe text")
	barrier.unblock()
	if err := <-completed; err == nil || !strings.Contains(err.Error(), "target schema changed") {
		t.Fatalf("concurrent DDL was hidden by an early target snapshot: %v", err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	var exists bool
	if err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='v3_billing.historical_entries'::regclass AND attname='cutover_probe' AND NOT attisdropped)").Scan(&exists); err != nil || !exists {
		t.Fatalf("refused final lost the concurrently added column exists=%v err=%v", exists, err)
	}
	if _, err := UnsealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatalf("DDL refusal leaked source lock: %v", err)
	}
}
