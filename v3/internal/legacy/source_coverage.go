package legacy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Durable assets use separate native stores. Verify they were copied before
// exposing the atomically imported relational data to target writers.
func validateSourceCoverage(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) error {
	if err := validateFileCoverage(ctx, source, sources, report); err != nil {
		return err
	}
	if err := validateBackgroundCoverage(ctx, source, sources, report); err != nil {
		return err
	}
	return nil
}
