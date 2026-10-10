package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type historyCutoffContextKey struct{}

// WithHistoryCutoff fixes one boundary for preview, copy, replay and verification.
// A zero cutoff preserves the existing all-history migration contract.
func (m *Importer) WithHistoryCutoff(cutoff time.Time) *Importer {
	m.historyCutoff = cutoff.UTC().Truncate(time.Second)
	return m
}

func historyCutoffFrom(ctx context.Context) time.Time {
	cutoff, _ := ctx.Value(historyCutoffContextKey{}).(time.Time)
	return cutoff
}

func historyCutoffLabel(ctx context.Context) string {
	cutoff := historyCutoffFrom(ctx)
	if cutoff.IsZero() {
		return ""
	}
	return cutoff.Format(time.RFC3339)
}

func validateHistoryCutoff(ctx context.Context, target pgx.Tx) error {
	var exists bool
	if err := target.QueryRow(ctx, "SELECT to_regclass('v3_audit.history_retention') IS NOT NULL").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if historyCutoffFrom(ctx).IsZero() {
			return nil
		}
		return errors.New("legacy: history cutoff requires the retention schema")
	}
	var stored *time.Time
	err := target.QueryRow(ctx, "SELECT cutoff FROM v3_audit.history_retention WHERE singleton").Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	actual := historyCutoffFrom(ctx)
	if stored == nil && actual.IsZero() || stored != nil && stored.Equal(actual) {
		return nil
	}
	return errors.New("legacy: history cutoff changed; start an independent migration")
}

func bindHistoryCutoff(ctx context.Context, target pgx.Tx) error {
	if err := validateHistoryCutoff(ctx, target); err != nil {
		return err
	}
	var exists bool
	if err := target.QueryRow(ctx, "SELECT to_regclass('v3_audit.history_retention') IS NOT NULL").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	} // Explicitly unfiltered older schema upgrade fixtures.
	var cutoff *time.Time
	if value := historyCutoffFrom(ctx); !value.IsZero() {
		cutoff = &value
	}
	_, err := target.Exec(ctx, "INSERT INTO v3_audit.history_retention(singleton,cutoff) VALUES(true,$1) ON CONFLICT DO NOTHING", cutoff)
	if err != nil {
		return err
	}
	return validateHistoryCutoff(ctx, target)
}

// Only ordinary histories are filtered. Money provenance, administrative quota
// changes and refunds remain intact. Unknown/null states remain for validation.
func historyWindow(ctx context.Context, name, alias, requests, attempts string) string {
	cutoff := historyCutoffFrom(ctx)
	if cutoff.IsZero() {
		return "TRUE"
	}
	stamp := "'" + cutoff.Format(time.RFC3339) + "'::timestamptz"
	requestOwn := func(a string) string {
		return fmt.Sprintf("(%[1]s.created_at IS NULL OR %[1]s.started_at IS NULL OR %[1]s.created_at >= %[2]s OR %[1]s.started_at >= %[2]s OR %[1]s.completed_at >= %[2]s OR %[1]s.status IS NULL OR %[1]s.status NOT IN ('succeeded','failed','canceled','cancelled','completed') OR %[1]s.completed_at IS NULL OR %[1]s.completed_at <= '1970-01-01'::timestamptz)", a, stamp)
	}
	attemptOwn := requestOwn
	requestPredicate := func(a string) string {
		own := requestOwn(a)
		if attempts == "" {
			return own
		}
		return "(" + own + " OR EXISTS(SELECT 1 FROM " + attempts + " retention_child WHERE retention_child.request_id=" + a + ".request_id AND " + attemptOwn("retention_child") + "))"
	}
	switch name {
	case "logs":
		return fmt.Sprintf("(%[1]s.created_at IS NULL OR %[1]s.type IS NULL OR %[1]s.created_at >= %[2]d OR %[1]s.type IN (1,3,6))", alias, cutoff.Unix())
	case "request_audits":
		return requestPredicate(alias)
	case "request_attempt_audits":
		own := attemptOwn(alias)
		if requests == "" {
			return own
		}
		return "(" + own + " OR EXISTS(SELECT 1 FROM " + requests + " retention_parent WHERE retention_parent.request_id=" + alias + ".request_id AND " + requestPredicate("retention_parent") + "))"
	}
	return "TRUE"
}

func walkHistoryRequests(ctx context.Context, source pgx.Tx, requests, attempts string, visit func(json.RawMessage) error) error {
	if requests == "" {
		return nil
	}
	rows, err := source.Query(ctx, "SELECT to_jsonb(t) FROM "+requests+" t WHERE "+historyWindow(ctx, "request_audits", "t", requests, attempts))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := visit(raw); err != nil {
			return err
		}
	}
	return rows.Err()
}
