package legacy

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const reconcileDomainWorkers = 3

var reconcileSnapshotID = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{8}-[0-9]+$`)

func (m *Importer) validateReconcilePools() error {
	if m.source == nil || m.pool == nil || m.source == m.pool {
		return errors.New("legacy: parallel retained reconciliation requires independent source and target pools")
	}
	// The coordinator retains one connection in each database until every
	// worker finishes. Waiting for a fourth connection in a smaller pool would
	// deadlock; reject explicitly rather than silently changing execution mode.
	if m.source.Config().MaxConns < reconcileDomainWorkers+1 || m.pool.Config().MaxConns < reconcileDomainWorkers+1 {
		return errors.New("legacy: parallel retained reconciliation requires at least four source and target connections")
	}
	return nil
}

func reconcileDomains(specs []onlineSpec) ([]onlineSpec, [reconcileDomainWorkers][]onlineSpec, error) {
	var serial []onlineSpec
	var domains [reconcileDomainWorkers][]onlineSpec
	for _, spec := range specs {
		switch spec.name {
		case "logs", "ledger_entries":
			serial = append(serial, spec)
		case "request_audits", "request_attempt_audits":
			domains[0] = append(domains[0], spec)
		case "funding_lots", "funding_allocations":
			domains[1] = append(domains[1], spec)
		default:
			if !strings.HasPrefix(spec.name, "market.") || !cmStreamedTable(strings.TrimPrefix(spec.name, "market.")) {
				return nil, domains, errors.New("legacy: unsupported parallel retained reconciliation domain")
			}
			domains[2] = append(domains[2], spec)
		}
	}
	// The target request projection determines whether attempts are linked or
	// orphaned. Keep parents first even if a caller supplies specs out of order.
	for _, pair := range []struct {
		domain int
		first  string
	}{{0, "request_audits"}, {1, "funding_lots"}} {
		if len(domains[pair.domain]) == 2 && domains[pair.domain][0].name != pair.first {
			domains[pair.domain][0], domains[pair.domain][1] = domains[pair.domain][1], domains[pair.domain][0]
		}
	}
	return serial, domains, nil
}

func addReconcileReport(total *OnlineReconcileReport, delta OnlineReconcileReport) {
	total.Scanned += delta.Scanned
	total.Unchanged += delta.Unchanged
	total.Repaired += delta.Repaired
	total.Pruned += delta.Pruned
}

func rollbackReconcile(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

func (p *onlineProjector) acquireReconcileRead(ctx context.Context) error {
	if p.reconcileReadToken == nil {
		return nil
	}
	if p.reconcileReadHeld {
		return errors.New("legacy: retained reconciliation source read token is already held")
	}
	select {
	case p.reconcileReadToken <- struct{}{}:
		p.reconcileReadHeld = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *onlineProjector) releaseReconcileRead() {
	if p.reconcileReadHeld {
		<-p.reconcileReadToken
		p.reconcileReadHeld = false
	}
}

func reconcileSpecs(ctx context.Context, conn *pgxpool.Conn, source pgx.Tx, p *onlineProjector, runID string, specs []onlineSpec, coordinatorPID uint32) (OnlineReconcileReport, error) {
	r := OnlineReconcileReport{RunID: runID, Phase: "reconciling"}
	defer p.releaseReconcileRead()
	for _, spec := range specs {
		for {
			target, err := conn.Begin(ctx)
			if err != nil {
				return r, err
			}
			var delta OnlineReconcileReport
			var complete bool
			if coordinatorPID == 0 {
				delta, complete, err = p.reconcilePage(ctx, source, target, runID, spec)
			} else {
				delta, complete, err = p.reconcilePageAuthorized(ctx, source, target, runID, spec, coordinatorPID)
			}
			if err == nil {
				err = target.Commit(ctx)
			}
			if err != nil {
				rollbackReconcile(ctx, target)
				p.releaseReconcileRead()
				return r, err
			}
			p.releaseReconcileRead()
			// The report describes acknowledged commits from this invocation,
			// including pages committed before another domain failed. It is not
			// a claim that Applied=false means no earlier page was committed.
			addReconcileReport(&r, delta)
			if complete {
				break
			}
		}
	}
	return r, nil
}

func onlineAuthorizeReconcileWorker(ctx context.Context, target pgx.Tx, runID string, coordinatorPID uint32) error {
	var id, phase string
	if err := target.QueryRow(ctx, "SELECT run_id,phase FROM v3_migration_online.run WHERE singleton FOR SHARE").Scan(&id, &phase); err != nil {
		return err
	}
	if id != runID || phase != "reconciling" {
		return errors.New("legacy: parallel retained reconciliation ownership or phase changed")
	}
	var held bool
	// The coordinator acquired this session lock through onlineConnection and
	// keeps that physical connection checked out until all workers have exited.
	// Check its PID, current database and the bigint advisory-lock key, rather
	// than accepting a different session's lock or the two-int advisory key.
	if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
	 WHERE locktype='advisory' AND pid=$1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
	 AND classid=0 AND objid=$2::oid AND objsubid=1 AND mode='ExclusiveLock' AND granted)`, coordinatorPID, onlineLock).Scan(&held); err != nil {
		return err
	}
	if !held {
		return errors.New("legacy: parallel retained reconciliation coordinator lock was lost")
	}
	_, err := target.Exec(ctx, "SELECT set_config('codego.online_writer',$1,true)", runID)
	return err
}

func (m *Importer) reconcileParallel(ctx context.Context, coordinator *pgxpool.Conn, source pgx.Tx, p *onlineProjector, runID string, domains [reconcileDomainWorkers][]onlineSpec) (OnlineReconcileReport, error) {
	r := OnlineReconcileReport{RunID: runID, Phase: "reconciling"}
	var snapshot string
	if err := source.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil {
		return r, err
	}
	if !reconcileSnapshotID.MatchString(snapshot) {
		return r, errors.New("legacy: unexpected retained reconciliation snapshot identifier")
	}
	var coordinatorPID uint32
	if err := coordinator.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&coordinatorPID); err != nil {
		return r, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Read one source page at a time. Ordinary pages release the token before
	// projection and target comparison, preserving three-domain overlap. A wide
	// page retains it through commit/rollback, so three legal 64 MiB inputs cannot
	// simultaneously create large JSON buffers and target PostgreSQL workspaces.
	p.reconcileReadToken = make(chan struct{}, 1)
	p.reconcileReadHeld = false
	type result struct {
		report OnlineReconcileReport
		err    error
	}
	results := make(chan result, reconcileDomainWorkers)
	workers := 0
	for _, domain := range domains {
		if len(domain) == 0 {
			continue
		}
		workers++
		go func(specs []onlineSpec) {
			report, err := m.reconcileDomain(workerCtx, snapshot, p, runID, specs, coordinatorPID)
			// Buffering permits every worker to publish its committed counters
			// before cancellation. The coordinator still waits for all releases.
			results <- result{report: report, err: err}
			if err != nil {
				cancel()
			}
		}(domain)
	}
	var firstErr error
	for i := 0; i < workers; i++ {
		completed := <-results
		addReconcileReport(&r, completed.report)
		firstCancelled := errors.Is(firstErr, context.Canceled) || errors.Is(firstErr, context.DeadlineExceeded)
		completedCancelled := errors.Is(completed.err, context.Canceled) || errors.Is(completed.err, context.DeadlineExceeded)
		if completed.err != nil && (firstErr == nil || firstCancelled && !completedCancelled) {
			firstErr = completed.err
		}
	}
	return r, firstErr
}

func (m *Importer) reconcileDomain(ctx context.Context, snapshot string, shared *onlineProjector, runID string, specs []onlineSpec, coordinatorPID uint32) (OnlineReconcileReport, error) {
	r := OnlineReconcileReport{RunID: runID, Phase: "reconciling"}
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer rollbackReconcile(ctx, source)
	// PostgreSQL does not accept a parameter for SET TRANSACTION SNAPSHOT.
	// The coordinator validated the identifier before interpolation, and this
	// must be the first statement after BEGIN, before this worker reads rows.
	if !reconcileSnapshotID.MatchString(snapshot) {
		return r, errors.New("legacy: unexpected retained reconciliation snapshot identifier")
	}
	if _, err := source.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+snapshot+"'"); err != nil {
		return r, err
	}
	target, err := m.pool.Acquire(ctx)
	if err != nil {
		return r, err
	}
	defer target.Release()
	// Dependencies, funding identities and source projections are immutable
	// after coordinator initialization. Only the per-page target field changes,
	// so each domain gets its own projector rather than racing on shared.target.
	p := *shared
	p.target = nil
	return reconcileSpecs(ctx, target, source, &p, runID, specs, coordinatorPID)
}
