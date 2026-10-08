package legacy

import (
	"context"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Durable assets use separate native stores. Verify they were copied before
// exposing the atomically imported relational data to target writers.
func validateSourceCoverage(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) error {
	if err := validateKnownSources(ctx, source, sources, report); err != nil {
		return err
	}
	if err := validateFileCoverage(ctx, source, sources, report); err != nil {
		return err
	}
	if err := validateBackgroundCoverage(ctx, source, sources, report); err != nil {
		return err
	}
	return nil
}

// Discovery sees every application table. A nonempty unrecognized table must
// not disappear simply because both import and check lack the same loader.
func validateKnownSources(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) error {
	known := map[string]bool{}
	mark := func(name string) {
		if table := sources[name]; table != "" {
			known[table] = true
		}
	}
	for _, name := range strings.Fields(`users tokens channels options accounts balance_snapshots logs
		passkeys passkey_credentials custom_oauth_providers user_oauth_bindings ledger_entries
		request_audits request_attempt_audits request_executions reservations settlements outbox_events
		security_audit_events account_request_abuse_states two_fas two_fa_backup_codes
		desktop_authorized_devices desktop_auth_sessions community_channel_ratings
		codego_oidc_authorization_codes codego_oidc_access_tokens gateway_user_files gateway_upstream_file_mappings
		responses_background_jobs responses_background_events tasks task_workflows task_snapshots task_terminal_results
		point_accounts point_ledgers bonus_quota_credits user_wechat_bindings miniprogram_bind_codes`) {
		mark(name)
		// Domain schema aliases are used by the actual loaders and guards.
		for _, prefix := range []string{"billing_", "gateway_", "workflow_"} {
			mark(prefix + name)
		}
	}
	for _, names := range [][]string{commerceSourceNames, catalogDataSourceNames, fundingSourceNames, marketplaceSourceTables} {
		for _, name := range names {
			mark(name)
		}
	}
	for _, name := range marketplaceSourceTables {
		mark("marketplace_" + name)
	}
	for _, name := range channelMarketSourceTables {
		mark("marketplace_" + name)
	}
	for _, contract := range entitlementContracts {
		mark(contract.source)
	}
	for name := range entitlementAuditExclusions {
		mark(name)
	}
	for name := range sources {
		if strings.HasPrefix(name, "pet_") || strings.HasPrefix(name, "user_pet") || name == "pets" {
			mark(name) // explicitly retired and reported by reportRetiredSources
		}
	}
	// Schema tool bookkeeping contains no customer state.
	for _, name := range []string{"schema_migrations", "platform_schema_migrations", "atlas_schema_revisions", "goose_db_version"} {
		mark(name)
	}
	// These are read projections, not authoritative entitlements or money.
	// Native permissions and aggregates rebuild from channels, logs and ledger.
	for _, name := range strings.Fields(`abilities perf_metrics channel_perf_metrics channel_latency_histograms
		channel_consumer_metrics channel_consumer_identities user_usage_daily channel_usage_daily usage_daily_cursors billing_account_views`) {
		for _, alias := range []string{name, "readmodel_" + name} {
			if table := sources[alias]; table != "" && !known[table] {
				var count int64
				if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
					return err
				}
				report.Counts["rebuilt_read_projections."+name] = count
				known[table] = true
			}
		}
	}
	seen := map[string]bool{}
	var unknown []string
	for _, table := range sources {
		if !known[table] && !seen[table] {
			seen[table] = true
			unknown = append(unknown, table)
		}
	}
	sort.Strings(unknown)
	for _, table := range unknown {
		var populated bool
		if err := source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" LIMIT 1)").Scan(&populated); err != nil {
			return err
		}
		if populated {
			report.UnmappedSources = append(report.UnmappedSources, table)
			report.Issues = append(report.Issues, Issue{table, 0, "unmapped_source", "populated source needs an explicit native mapping or retirement contract"})
		}
	}
	return nil
}
