package legacy

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// OpenBackgroundMigrationRepository uses exactly the gateway/worker Redis
// namespace and derived encryption key. Their configured Redis database is 0.
func OpenBackgroundMigrationRepository(ctx context.Context) (*live.RedisBackgroundRepository, func(), error) {
	if os.Getenv("V3_REDIS_ADDR") == "" || os.Getenv("V3_SECRET_KEY") == "" {
		return nil, nil, errors.New("V3_REDIS_ADDR and V3_SECRET_KEY are required for background history assets")
	}
	crypto, err := catalog.NewAESGCMFromBase64(os.Getenv("V3_SECRET_KEY"))
	if err != nil {
		return nil, nil, errors.New("legacy: invalid target background encryption key")
	}
	client, err := redisx.Connect(redisx.Config{Addr: os.Getenv("V3_REDIS_ADDR"), Password: os.Getenv("V3_REDIS_PASSWORD"), PoolSize: 2})
	if err != nil {
		return nil, nil, err
	}
	closeClient := func() { _ = client.Close() }
	if err = client.Ping(ctx).Err(); err != nil {
		closeClient()
		return nil, nil, err
	}
	repository, err := live.NewRedisBackgroundRepository(client, "", crypto.DeriveKey("background-jobs"))
	if err != nil {
		closeClient()
		return nil, nil, err
	}
	return repository, closeClient, nil
}

// ImportBackground copies settled terminal results as a separate resumable
// asset step before the atomic database import. Every source row is validated
// before the first Redis write; each job and all its events publish atomically.
func (m *Importer) ImportBackground(ctx context.Context, repository *live.RedisBackgroundRepository, apply bool) (Report, error) {
	return m.backgroundAssets(ctx, repository, apply, false)
}

// CheckBackground compares pre-copied results and events without changing the
// target, including immutable routing/ownership/result/time facts.
func (m *Importer) CheckBackground(ctx context.Context, repository *live.RedisBackgroundRepository) (Report, error) {
	return m.backgroundAssets(ctx, repository, false, true)
}

func (m *Importer) backgroundAssets(ctx context.Context, repository *live.RedisBackgroundRepository, apply, check bool) (Report, error) {
	report := Report{Counts: map[string]int64{}, Amounts: map[string]string{}, Issues: []Issue{}}
	if m.source == nil {
		return report, errors.New("legacy: source database is required")
	}
	tx, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		return report, err
	}
	assets, err := loadSourceBackground(ctx, tx, sources, &report)
	if err != nil {
		return report, err
	}
	if len(assets) > 0 && (apply || check) && repository == nil {
		return report, errors.New("legacy: target background repository is required")
	}
	if repository != nil {
		// Refuse existing conflicts before installing any missing asset. Exact
		// replay does not refresh TTL or reschedule work.
		for _, asset := range assets {
			err = repository.VerifyTerminalJob(ctx, asset.job, asset.events)
			if err != nil && (check || !errors.Is(err, live.ErrNotFound)) {
				return report, fmt.Errorf("legacy: copied background history is missing, changed or corrupt: %w", err)
			}
		}
		if apply {
			for _, asset := range assets {
				if err = repository.ImportTerminalJob(ctx, asset.job, asset.events); err != nil {
					return report, fmt.Errorf("legacy: background history asset publication failed: %w", err)
				}
			}
		}
		if apply || check {
			if err = checkBackgroundAssets(ctx, repository, assets, &report); err != nil {
				return report, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return report, err
	}
	report.Applied = apply
	return report, nil
}

func checkBackgroundAssets(ctx context.Context, repository *live.RedisBackgroundRepository, assets []backgroundAsset, report *Report) error {
	for _, asset := range assets {
		if err := repository.VerifyTerminalJob(ctx, asset.job, asset.events); err != nil {
			return fmt.Errorf("legacy: copied background history is missing, changed or corrupt: %w", err)
		}
		report.Counts["check:responses_background_jobs"]++
		report.Counts["check:responses_background_events"] += int64(len(asset.events))
	}
	return nil
}

// The database preview/check uses this read-only hook. No Redis configuration
// is needed when the source has no background history.
func validateBackgroundCoverage(ctx context.Context, tx pgx.Tx, sources map[string]string, report *Report) error {
	assets, err := loadSourceBackground(ctx, tx, sources, report)
	if err != nil || len(assets) == 0 {
		return err
	}
	repository, closeClient, err := OpenBackgroundMigrationRepository(ctx)
	if err == nil {
		defer closeClient()
		err = checkBackgroundAssets(ctx, repository, assets, report)
	}
	if err != nil {
		report.Issues = append(report.Issues, Issue{"responses_background_jobs", 0, "background_assets_not_migrated", "run migrate background with the target Redis and encryption key before database application"})
	}
	return nil
}
