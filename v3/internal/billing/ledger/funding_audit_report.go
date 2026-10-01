package ledger

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type FundingEconomicsSource struct {
	Source  string        `json:"source"`
	Amount  credits.Micro `json:"amount_micro"`
	Revenue credits.Micro `json:"revenue_micro"`
	Cost    credits.Micro `json:"cost_micro"`
	Profit  credits.Micro `json:"profit_micro"`
}

type FundingDailyEconomics struct {
	Date              string                   `json:"date"`
	RecognizedRevenue credits.Micro            `json:"recognized_revenue_micro"`
	RecognizedCost    credits.Micro            `json:"recognized_cost_micro"`
	RecognizedProfit  credits.Micro            `json:"recognized_profit_micro"`
	UnattributedCost  credits.Micro            `json:"unattributed_cost_micro"`
	Sources           []FundingEconomicsSource `json:"sources"`
}

// DailyFundingEconomics is an internal procurement report; callers must require
// root authorization. A single snapshot joins actual source allocations with
// frozen request cost. Market explanatory gross is not another money movement.
func DailyFundingEconomics(ctx context.Context, pool *pgxpool.Pool, day time.Time) (FundingDailyEconomics, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return FundingDailyEconomics{}, err
	}
	local := day.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	result := FundingDailyEconomics{Date: start.Format("2006-01-02"), Sources: []FundingEconomicsSource{}}
	rows, err := pool.Query(ctx, dailyFundingEconomicsQuery, start.UTC(), start.AddDate(0, 0, 1).UTC())
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		entry, err := scanFundingEconomicsSource(rows)
		if err != nil {
			return result, err
		}
		if entry.Amount == 0 {
			continue
		}
		entry.Profit = entry.Revenue - entry.Cost
		result.Sources = append(result.Sources, entry)
		if err := result.accumulate(entry); err != nil {
			return result, err
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.RecognizedProfit = result.RecognizedRevenue - result.RecognizedCost
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].Source < result.Sources[j].Source })
	return result, nil
}

const dailyFundingEconomicsQuery = `WITH economics AS (
 SELECT * FROM v3_billing.request_economics WHERE settled_at >= $1 AND settled_at < $2
), allocated AS (
 SELECT a.request_id,a.source,a.amount,a.revenue_multiplier_ppm,e.procurement_cost_multiplier_ppm
 FROM economics e JOIN v3_billing.funding_allocations a ON a.request_id=e.request_id
), pieces AS (
 SELECT source,amount,revenue_multiplier_ppm,procurement_cost_multiplier_ppm FROM allocated
 UNION ALL
 SELECT CASE WHEN e.billing_source='wallet' THEN 'legacy_unattributed' ELSE 'subscription' END,
 e.actual_amount-coalesce((SELECT sum(a.amount) FROM allocated a WHERE a.request_id=e.request_id),0),
 CASE WHEN e.billing_source='wallet' THEN 0 ELSE e.revenue_multiplier_ppm END,e.procurement_cost_multiplier_ppm FROM economics e
)
 SELECT source,sum(amount)::text,round(sum(amount::numeric*revenue_multiplier_ppm)/1000000)::text,
 round(sum(amount::numeric*procurement_cost_multiplier_ppm)/1000000)::text,bool_or(amount<0)
 FROM pieces GROUP BY source`

// fundingRowScanner is satisfied by pgx.Rows.
type fundingRowScanner interface {
	Scan(dest ...any) error
}

// scanFundingEconomicsSource scans one grouped row of the daily funding
// economics query and converts its text-encoded amounts to credits.Micro.
func scanFundingEconomicsSource(row fundingRowScanner) (FundingEconomicsSource, error) {
	var entry FundingEconomicsSource
	var amount, revenue, cost string
	var invalid bool
	if err := row.Scan(&entry.Source, &amount, &revenue, &cost, &invalid); err != nil {
		return entry, err
	}
	if invalid {
		return entry, errors.New("ledger: funding allocation exceeds settled actual")
	}
	for _, field := range []struct {
		text  string
		value *credits.Micro
	}{{amount, &entry.Amount}, {revenue, &entry.Revenue}, {cost, &entry.Cost}} {
		n, err := strconv.ParseInt(field.text, 10, 64)
		if err != nil || n < 0 {
			return entry, credits.ErrOverflow
		}
		*field.value = credits.Micro(n)
	}
	return entry, nil
}

// accumulate folds one source's cost/revenue into the report's recognized or
// unattributed totals.
func (result *FundingDailyEconomics) accumulate(entry FundingEconomicsSource) error {
	var err error
	if entry.Source == "legacy_unattributed" || entry.Source == "other" {
		result.UnattributedCost, err = result.UnattributedCost.Add(entry.Cost)
		return err
	}
	result.RecognizedRevenue, err = result.RecognizedRevenue.Add(entry.Revenue)
	if err != nil {
		return err
	}
	result.RecognizedCost, err = result.RecognizedCost.Add(entry.Cost)
	return err
}
