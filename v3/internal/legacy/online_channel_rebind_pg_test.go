//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func channelRebindReview(t *testing.T, source, target *pgxpool.Pool, opts OnlineOptions, publicID string, nextID int64) OnlineChannelRebindOptions {
	t.Helper()
	ctx := context.Background()
	r := OnlineChannelRebindOptions{OnlineOptions: opts}
	var previous []byte
	if err := target.QueryRow(ctx, "SELECT target_shape,capture_hash,dependencies FROM v3_migration_online.run").Scan(&r.ExpectedTargetShape, &r.ExpectedCaptureHash, &previous); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(previous)
	r.ExpectedDependenciesHash = hex.EncodeToString(sum[:])
	var old map[string]json.RawMessage
	if err := json.Unmarshal(previous, &old); err != nil {
		t.Fatal(err)
	}
	key := "market.channel:" + publicID
	var before []int64
	if err := json.Unmarshal(old[key], &before); err != nil {
		t.Fatal(err)
	}
	changes := map[string]map[string][]int64{key: {"before": before, "after": {nextID, before[1]}}}
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(data)
	r.ExpectedChangesHash = hex.EncodeToString(sum[:])
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	r.ExpectedSourceIdentity, err = onlineDatabaseIdentity(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func channelRebindProgress(t *testing.T, target *pgxpool.Pool) string {
	t.Helper()
	var value string
	if err := target.QueryRow(context.Background(), "SELECT jsonb_agg(to_jsonb(p) ORDER BY name)::text FROM v3_migration_online.progress p").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func channelRebindFields(t *testing.T, target *pgxpool.Pool) string {
	t.Helper()
	var value string
	if err := target.QueryRow(context.Background(), "SELECT jsonb_agg(to_jsonb(t)-'channel_id' ORDER BY id)::text FROM "+onlineStage("v3_channelmarket.settlements")+" t").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func channelRebindGuardedExec(t *testing.T, target *pgxpool.Pool, runID, query string) {
	t.Helper()
	ctx := context.Background()
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := onlineAuthorize(ctx, tx, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, query); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineChannelRebindContinuousCaptureExactFieldsAndResume(t *testing.T) {
	m, source, target, opts := onlineGeneratedChannelFixture(t)
	ctx := context.Background()
	review := channelRebindReview(t, source, target, opts, "legacy-public-201", 13)
	progress, fields := channelRebindProgress(t, target), channelRebindFields(t, target)
	unchanged := retainedRowXmin(t, target, 53)
	var journal uint32
	if err := source.QueryRow(ctx, "SELECT 'v3_migration_capture.events'::regclass::oid").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, source, "UPDATE marketplace.channels SET internal_channel_id=13 WHERE id='legacy-public-201'")
	if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "structural dependency changed") {
		t.Fatalf("ordinary sync bypassed drift protection: %v", err)
	}
	r, err := m.RebindOnlineChannels(ctx, review)
	if err != nil || !r.Applied || r.Changes != 1 || r.Rows["market.settlements"] == 0 {
		t.Fatalf("reviewed repair %+v err=%v", r, err)
	}
	if channelRebindProgress(t, target) != progress || channelRebindFields(t, target) != fields || retainedRowXmin(t, target, 53) != unchanged {
		t.Fatal("rebind changed copy checkpoints, exact money/lifecycle fields or unrelated logs")
	}
	var continuous bool
	if err := source.QueryRow(ctx, "SELECT 'v3_migration_capture.events'::regclass::oid=$1 AND NOT sealed AND run_id=$2 AND NOT EXISTS(SELECT 1 FROM v3_migration_capture.events WHERE acked) FROM v3_migration_capture.config", journal, opts.RunID).Scan(&continuous); err != nil || !continuous {
		t.Fatalf("repair changed capture epoch or consumed pending events: %t %v", continuous, err)
	}
	if _, err := m.RebindOnlineChannels(ctx, review); err == nil {
		t.Fatal("stale review or repeated repair accepted")
	}
	onlineMigrationSync(t, m, opts)
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if r, err := m.FinalizeOnline(ctx, opts); err != nil || !r.Applied || len(r.Issues) != 0 {
		t.Fatalf("isolated finalization %+v %v", r, err)
	}
	if r, err := m.Check(ctx); err != nil || len(r.Issues) != 0 {
		t.Fatalf("independent check %+v %v", r, err)
	}
	t.Log("same capture journal, unchanged checkpoints and exact non-ID fields; pending replay, Verify, isolated Finalize and independent Check passed")
}

func TestOnlineChannelRebindRejectsUnreviewedOrUnsafeChanges(t *testing.T) {
	for _, failure := range []string{"source", "shape", "capture", "dependencies", "changes", "owner", "deleted", "incomplete", "sealed"} {
		t.Run(failure, func(t *testing.T) {
			m, source, target, opts := onlineGeneratedChannelFixture(t)
			ctx := context.Background()
			review := channelRebindReview(t, source, target, opts, "legacy-public-201", 13)
			onlineRecoveryExec(t, source, "UPDATE marketplace.channels SET internal_channel_id=13 WHERE id='legacy-public-201'")
			switch failure {
			case "source":
				review.ExpectedSourceIdentity = strings.Repeat("0", 64)
			case "shape":
				review.ExpectedTargetShape = strings.Repeat("0", 64)
			case "capture":
				review.ExpectedCaptureHash = strings.Repeat("0", 64)
			case "dependencies":
				review.ExpectedDependenciesHash = strings.Repeat("0", 64)
			case "changes":
				review.ExpectedChangesHash = strings.Repeat("0", 64)
			case "owner":
				onlineRecoveryExec(t, source, "UPDATE marketplace.channels SET owner_user_id=8 WHERE id='legacy-public-201'")
			case "deleted":
				onlineRecoveryExec(t, source, "DELETE FROM marketplace.channels WHERE id='legacy-public-201'")
			case "incomplete":
				channelRebindGuardedExec(t, target, opts.RunID, "UPDATE v3_migration_online.progress SET complete=false WHERE name='market.settlements'")
			case "sealed":
				if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
					t.Fatal(err)
				}
			}
			progress, fields := channelRebindProgress(t, target), channelRebindFields(t, target)
			if r, err := m.RebindOnlineChannels(ctx, review); err == nil || r.Applied {
				t.Fatalf("unsafe change accepted: %+v %v", r, err)
			}
			if channelRebindProgress(t, target) != progress || channelRebindFields(t, target) != fields {
				t.Fatal("rejected repair changed staging")
			}
			onlineRecoveryAssertNoMoney(t, target)
		})
	}
}

type channelRebindLateFailureTx struct {
	pgx.Tx
	writes int
}

func (t *channelRebindLateFailureTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	t.writes++
	if t.writes == 2 {
		return pgconn.CommandTag{}, errors.New("isolated late channel repair failure")
	}
	return t.Tx.Exec(ctx, q, args...)
}

func TestOnlineChannelRebindLateFailureRollsBackAndPreservesLargeAmounts(t *testing.T) {
	_, _, target, opts := onlineGeneratedChannelFixture(t)
	ctx := context.Background()
	channelRebindGuardedExec(t, target, opts.RunID, "UPDATE "+onlineStage("v3_channelmarket.settlements")+" SET gross_micro=9007199254740994,net_micro=9007199254740984")
	fields := channelRebindFields(t, target)
	var beforeID int64
	if err := target.QueryRow(ctx, "SELECT channel_id FROM "+onlineStage("v3_channelmarket.settlements")+" LIMIT 1").Scan(&beforeID); err != nil {
		t.Fatal(err)
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
		t.Fatal(err)
	}
	changes := map[string]map[string][]int64{"market.channel:legacy-public-201": {"before": {beforeID, 7}, "after": {13, 7}}}
	specs := []onlineSpec{{name: "market.settlements", targets: []string{"v3_channelmarket.settlements"}}, {name: "market.verification_runs", targets: []string{"v3_channelmarket.verification_runs"}}}
	if _, err := rebindOnlineChannelRows(ctx, &channelRebindLateFailureTx{Tx: tx}, specs, changes); err == nil {
		t.Fatal("late failure not propagated")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var storedID int64
	if err := target.QueryRow(ctx, "SELECT channel_id FROM "+onlineStage("v3_channelmarket.settlements")+" LIMIT 1").Scan(&storedID); err != nil || storedID != beforeID || channelRebindFields(t, target) != fields {
		t.Fatalf("late failure changed ID or exact large amount: %d %v", storedID, err)
	}
}
