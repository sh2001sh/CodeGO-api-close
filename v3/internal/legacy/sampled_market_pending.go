package legacy

import (
	"context"
	"errors"
	"math"
	"sort"

	"github.com/jackc/pgx/v5"
)

type sampledPendingIncome struct{ owner, amount int64 }

// Inspect only current unsettled obligations. Reuse the normal projector for
// bounded differences; never grant the stale target's already released income.
func (p *onlineProjector) repairSampledPendingMarket(ctx context.Context, source pgx.Tx, sources map[string]string) (map[int64]int64, error) {
	table := sources["marketplace_settlements"]
	if table == "" {
		return nil, nil
	}
	read := func(tx pgx.Tx, query string, convert bool) (map[string]sampledPendingIncome, error) {
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		values := map[string]sampledPendingIncome{}
		for rows.Next() {
			var id string
			var v sampledPendingIncome
			if err = rows.Scan(&id, &v.owner, &v.amount); err != nil {
				return nil, err
			}
			if convert {
				micro, e := OpeningBalance(v.amount)
				if e != nil {
					return nil, e
				}
				v.amount = int64(micro)
			}
			if id == "" || v.owner <= 0 || v.amount < 0 {
				return nil, errors.New("legacy: invalid current pending income")
			}
			if _, exists := values[id]; exists {
				return nil, errors.New("legacy: duplicate current pending settlement")
			}
			values[id] = v
		}
		return values, rows.Err()
	}
	want, err := read(source, "SELECT id,owner_user_id,owner_net_amount FROM "+table+" WHERE status='pending'", true)
	if err != nil {
		return nil, err
	}
	actual, err := read(p.target, "SELECT id,owner_user_id,net_micro FROM "+onlineStage("v3_channelmarket.settlements")+" WHERE status='pending'", false)
	if err != nil {
		return nil, err
	}
	ids, balances, err := sampledPendingDifference(want, actual)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return balances, nil
	}
	rows, err := source.Query(ctx, "SELECT jsonb_build_object('id',id),to_jsonb(t) FROM "+table+" t WHERE id=ANY($1::text[])", ids)
	if err != nil {
		return nil, err
	}
	var inputs []onlineInput
	for rows.Next() {
		var input onlineInput
		if err = rows.Scan(&input.key, &input.raw); err != nil {
			rows.Close()
			return nil, err
		}
		inputs = append(inputs, input)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(inputs) != len(ids) {
		return nil, errors.New("legacy: current pending repair references deleted settlement")
	}
	for _, spec := range onlineSpecs(sources) {
		if spec.name != "market.settlements" {
			continue
		}
		if err = p.apply(ctx, spec, inputs); err != nil {
			return nil, err
		}
		matches, e := p.matchRetainedInputs(ctx, spec, inputs)
		if e != nil {
			return nil, e
		}
		for _, match := range matches {
			if !match {
				return nil, errors.New("legacy: current pending repair did not match sealed source")
			}
		}
		return balances, nil
	}
	return nil, errors.New("legacy: pending settlement projection missing")
}

func sampledPendingDifference(want, actual map[string]sampledPendingIncome) ([]string, map[int64]int64, error) {
	changed := map[string]bool{}
	balances := map[int64]int64{}
	for id, v := range want {
		if v.owner <= 0 || v.amount < 0 || balances[v.owner] > math.MaxInt64-v.amount {
			return nil, nil, errors.New("legacy: invalid pending owner sum")
		}
		balances[v.owner] += v.amount
		if a, ok := actual[id]; !ok || a != v {
			changed[id] = true
		}
	}
	for id := range actual {
		if _, ok := want[id]; !ok {
			changed[id] = true
		}
	}
	if len(changed) > 1024 {
		return nil, nil, errors.New("legacy: pending market repair exceeds bounded final-window budget")
	}
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, balances, nil
}
