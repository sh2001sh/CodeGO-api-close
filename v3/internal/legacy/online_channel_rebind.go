package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A reviewed repair of a continuous, completed staging baseline. Ordinary Sync
// still rejects dependency drift. This operation neither acknowledges capture
// events nor changes native business tables, source rows, or copy checkpoints.
type OnlineChannelRebindOptions struct {
	OnlineOptions
	ExpectedSourceIdentity, ExpectedTargetShape, ExpectedCaptureHash string
	ExpectedDependenciesHash, ExpectedChangesHash                    string
}

type OnlineChannelRebindReport struct {
	RunID   string           `json:"run_id"`
	Applied bool             `json:"applied"`
	Changes int              `json:"channel_bindings"`
	Rows    map[string]int64 `json:"updated_rows"`
}

func ValidateOnlineChannelRebindOptions(opts OnlineChannelRebindOptions) error {
	if !onlineCaptureRunID.MatchString(opts.RunID) {
		return errors.New("legacy: channel rebind requires a valid online run ID")
	}
	for _, value := range []string{opts.ExpectedSourceIdentity, opts.ExpectedTargetShape, opts.ExpectedCaptureHash, opts.ExpectedDependenciesHash, opts.ExpectedChangesHash} {
		if !onlineReviewedSHA.MatchString(value) {
			return errors.New("legacy: channel rebind requires reviewed identity, shape, capture, dependencies and changes hashes")
		}
	}
	return nil
}

func onlineChannelChanges(old map[string]json.RawMessage, p *onlineProjector) (map[string]map[string][]int64, string, error) {
	next := p.onlineDependencyValues()
	changes := map[string]map[string][]int64{}
	for key, previous := range old {
		current, exists := next[key]
		if !exists {
			return nil, "", errors.New("legacy: channel rebind cannot accept deleted dependencies")
		}
		value, err := json.Marshal(current)
		if err != nil {
			return nil, "", err
		}
		// Compare without floating point: account/user timestamps can be large.
		var before, after []int64
		if strings.HasPrefix(key, "market.channel:") {
			if err := json.Unmarshal(previous, &before); err != nil {
				return nil, "", err
			}
			if err := json.Unmarshal(value, &after); err != nil {
				return nil, "", err
			}
			if len(before) == 2 && len(after) == 2 && before[0] == after[0] && before[1] == after[1] {
				continue
			}
			if !onlineGeneratedChannelBound(key, previous, value, p.market) {
				return nil, "", errors.New("legacy: channel rebind only accepts exact generated-to-real bindings with unchanged owners")
			}
			changes[key] = map[string][]int64{"before": before, "after": after}
		}
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return changes, hex.EncodeToString(sum[:]), nil
}

func rebindOnlineChannelRows(ctx context.Context, target pgx.Tx, specs []onlineSpec, changes map[string]map[string][]int64) (map[string]int64, error) {
	rows := map[string]int64{}
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, spec := range specs {
		if !strings.HasPrefix(spec.name, "market.") || spec.name == "market.ranking_snapshots" {
			continue
		}
		switch spec.name {
		case "market.settlements", "market.verification_runs", "market.gpt56_mapping_runs", "market.multiplier_trend_snapshots", "market.pelican_artifacts":
		default:
			return nil, errors.New("legacy: unsupported channel rebind projection")
		}
		for _, table := range spec.targets {
			for _, key := range keys {
				change := changes[key]
				tag, err := target.Exec(ctx, "UPDATE "+onlineStage(table)+" SET channel_id=$2 WHERE channel_id=$1", change["before"][0], change["after"][0])
				if err != nil {
					return nil, err
				}
				rows[spec.name] += tag.RowsAffected()
			}
		}
	}
	return rows, nil
}

func (m *Importer) RebindOnlineChannels(ctx context.Context, opts OnlineChannelRebindOptions) (OnlineChannelRebindReport, error) {
	ctx = m.historyContext(ctx)
	r := OnlineChannelRebindReport{RunID: opts.RunID, Rows: map[string]int64{}}
	if err := ValidateOnlineChannelRebindOptions(opts); err != nil {
		return r, err
	}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer rollbackReconcile(ctx, source)
	var locked bool
	if err := source.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock_shared($1)", onlineLock).Scan(&locked); err != nil || !locked {
		if err == nil {
			err = errors.New("legacy: source capture setup or seal is already running")
		}
		return r, err
	}
	identity, err := onlineDatabaseIdentity(ctx, source)
	if err != nil || identity != opts.ExpectedSourceIdentity {
		return r, errors.New("legacy: reviewed channel rebind source identity changed")
	}
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer rollbackReconcile(ctx, target)
	if err := onlineAuthorize(ctx, target, opts.RunID); err != nil {
		return r, err
	}
	if err := onlineLockTargetSchema(ctx, target); err != nil {
		return r, err
	}
	sources, specs, err := onlineBindings(ctx, source, target, opts.RunID)
	if err != nil {
		return r, err
	}
	if err := validateHistoryCutoff(ctx, target); err != nil {
		return r, err
	}
	var phase, shape, captureHash string
	var previous []byte
	if err := target.QueryRow(ctx, "SELECT phase,target_shape,capture_hash,dependencies FROM v3_migration_online.run WHERE singleton").Scan(&phase, &shape, &captureHash, &previous); err != nil {
		return r, err
	}
	previousHash := sha256.Sum256(previous)
	if (phase != "copied" && phase != "verified") || shape != opts.ExpectedTargetShape || captureHash != opts.ExpectedCaptureHash || hex.EncodeToString(previousHash[:]) != opts.ExpectedDependenciesHash {
		return r, errors.New("legacy: channel rebind phase or reviewed target bindings changed")
	}
	var complete, unsealed bool
	if err := target.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM v3_migration_online.progress WHERE NOT complete)").Scan(&complete); err != nil {
		return r, err
	}
	if err := source.QueryRow(ctx, "SELECT NOT sealed FROM v3_migration_capture.config WHERE singleton AND run_id=$1", opts.RunID).Scan(&unsealed); err != nil {
		return r, err
	}
	if !complete || !unsealed {
		return r, errors.New("legacy: channel rebind requires completed staging and continuous unsealed capture")
	}
	for _, table := range []string{"v3_identity.users", "v3_identity.api_keys", "v3_catalog.channels", "v3_billing.accounts", "v3_billing.ledger_entries", "v3_billing.balance_outbox"} {
		var populated bool
		if err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
			return r, err
		}
		if populated {
			return r, errors.New("legacy: channel rebind cannot modify native business or money")
		}
	}
	var old map[string]json.RawMessage
	if err := json.Unmarshal(previous, &old); err != nil {
		return r, err
	}
	// Reuse the existing exact dependency validation, including deleted-parent
	// archive checks. Its dependency update and all reference updates are one
	// transaction; no other operation can observe a half-rebound baseline.
	p, err := loadOnlineProjectorForEpoch(ctx, source, target, sources, true)
	if err != nil {
		return r, err
	}
	changes, changesHash, err := onlineChannelChanges(old, p)
	if err != nil {
		return r, err
	}
	if len(changes) == 0 || changesHash != opts.ExpectedChangesHash {
		return r, errors.New("legacy: reviewed channel rebind changes differ or are already applied")
	}
	r.Rows, err = rebindOnlineChannelRows(ctx, target, specs, changes)
	if err != nil {
		return r, err
	}
	if _, err := target.Exec(ctx, "UPDATE v3_migration_online.run SET phase='copied',verified_at=NULL WHERE singleton"); err != nil {
		return r, err
	}
	if err := target.Commit(ctx); err != nil {
		return r, err
	}
	r.Changes, r.Applied = len(changes), true
	return r, nil
}
