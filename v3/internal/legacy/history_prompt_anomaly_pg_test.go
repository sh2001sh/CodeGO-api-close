//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/audit"
)

const promptAnomalyMetadata = `{"usage_semantic":"anthropic","cache_tokens":0,"cache_creation_tokens":9827}`

func assertPromptAnomaly(t *testing.T, target *pgxpool.Pool, stage bool) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"v3_audit.events", "v3_billing.usage_logs"} {
		if stage {
			table = onlineStage(table)
		}
		var prompt, amount int64
		var anomaly bool
		if err := target.QueryRow(ctx, "SELECT prompt_tokens,amount,legacy_prompt_anomaly FROM "+table+" WHERE id=51").Scan(&prompt, &amount, &anomaly); err != nil || prompt != -12 || amount != 100 || !anomaly {
			t.Fatalf("historical evidence changed: %s %d %d %v %v", table, prompt, amount, anomaly, err)
		}
	}
	if !stage {
		s := audit.New(target, audit.Config{})
		summary, err := s.Summarize(ctx, audit.Principal{UserID: 7}, audit.Query{})
		if err != nil || summary.Requests != 2 || summary.Amount != 120 || summary.PromptTokens != 2 || summary.CompletionTokens != 8 || summary.PromptTokensUnknownRequests != 1 {
			t.Fatalf("anomaly summary lost charges or reported negative tokens: %+v %v", summary, err)
		}
		if _, err = target.Exec(ctx, "UPDATE v3_billing.usage_logs SET legacy_prompt_anomaly=false WHERE id=51"); err == nil {
			t.Fatal("negative live usage accepted")
		}
		if _, err = target.Exec(ctx, "UPDATE v3_audit.events SET legacy_prompt_anomaly=false WHERE id=51"); err == nil {
			t.Fatal("unmarked negative historical statistic accepted")
		}
		page, err := s.List(ctx, audit.Principal{UserID: 7}, audit.Query{})
		if err != nil || len(page.Items) != 2 {
			t.Fatalf("history read failed: %+v %v", page, err)
		}
		var found bool
		for _, row := range page.Items {
			if row.ID == 51 {
				found = row.PromptTokens == -12 && row.Amount == 100
			}
		}
		if !found {
			t.Fatal("API did not preserve anomalous raw evidence")
		}
	}
}

func TestHistoryPromptAnomalyOldTargetRejectedWithoutOpening(t *testing.T) {
	source, target, crypto := importTestDBBefore(t, "20261010000110_legacy_prompt_anomaly.sql")
	historyFixture(t, source)
	seedRetiredHistoryFixture(t, source)
	m := NewImporter(source, target, crypto)
	ctx := context.Background()
	opts := OnlineOptions{RunID: "unsupported-prompt-anomaly", SourceAdmin: source}
	if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET prompt_tokens=-12,other=$1 WHERE id=51", promptAnomalyMetadata); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CopyOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "requires schema 110") {
		t.Fatalf("unsupported schema accepted anomalous evidence: %v", err)
	}
	var entries int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries").Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("refused copy opened ledger: %d %v", entries, err)
	}
}

func TestHistoryPromptAnomalyOfflineAndOnlinePreserveCharges(t *testing.T) {
	for _, online := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "online"}[online], func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET prompt_tokens=-12,other=$1 WHERE id=51", promptAnomalyMetadata); err != nil {
				t.Fatal(err)
			}
			if !online {
				r, err := m.Import(ctx, true)
				if err != nil || len(r.Issues) > 0 || r.Counts["history.legacy_prompt_anomalies"] != 1 {
					t.Fatalf("import %+v %v", r, err)
				}
			} else {
				onlineMigrationReady(t, m, opts)
				assertPromptAnomaly(t, target, true)
				// A live delta fixes the statistic, then a new witnessed anomaly
				// must replay both the raw value and its marker without billing.
				if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET prompt_tokens=5,other='{}' WHERE id=51"); err != nil {
					t.Fatal(err)
				}
				onlineMigrationSync(t, m, opts)
				if _, err := m.VerifyOnline(ctx, opts); err != nil {
					t.Fatal(err)
				}
				var marker bool
				if err := target.QueryRow(ctx, "SELECT legacy_prompt_anomaly FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=51").Scan(&marker); err != nil || marker {
					t.Fatalf("stale anomaly marker %v %v", marker, err)
				}
				if _, err := source.Exec(ctx, "UPDATE migration_source.logs SET prompt_tokens=-12,other=$1 WHERE id=51", promptAnomalyMetadata); err != nil {
					t.Fatal(err)
				}
				onlineMigrationSync(t, m, opts)
				if _, err := m.VerifyOnline(ctx, opts); err != nil {
					t.Fatal(err)
				}
				if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
					t.Fatal(err)
				}
				if _, err := m.FinalizeOnline(ctx, opts); err != nil {
					t.Fatal(err)
				}
			}
			assertPromptAnomaly(t, target, false)
			if _, err := m.Check(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := target.Exec(ctx, "UPDATE v3_billing.usage_logs SET prompt_tokens=-11 WHERE id=51"); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Check(ctx); err == nil {
				t.Fatal("tampered raw statistic accepted by full verification")
			}
		})
	}
}
