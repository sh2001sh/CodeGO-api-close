package legacy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

// Derive callback metadata only after all original orders, subscriptions and
// reserved cards exist. Sharing the target transaction prevents a committed
// opening balance from exposing an incompletely initialized checkout.
func (m *Importer) initializeCommerceRuntime(ctx context.Context, tx pgx.Tx, report *Report) error {
	service := commerce.New(m.pool, nil, nil, commerce.Config{})
	initializers := []struct {
		name string
		run  func(context.Context, pgx.Tx) (int, error)
	}{
		{"subscriptions", service.InitializeImportedSubscriptionsTx},
		{"monthly_purchase_benefits", service.InitializeImportedMonthlyBenefitsTx},
		{"group_checkouts", service.InitializeImportedGroupCheckoutsTx},
		{"checkout_discounts", service.InitializeImportedCheckoutDiscountsTx},
	}
	for _, initializer := range initializers {
		count, err := initializer.run(ctx, tx)
		if err != nil {
			return fmt.Errorf("legacy: initialize %s: %w", initializer.name, err)
		}
		report.Counts["initialized:"+initializer.name] = int64(count)
	}
	return nil
}
