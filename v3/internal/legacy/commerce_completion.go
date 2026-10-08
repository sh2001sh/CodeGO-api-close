package legacy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Unlimited/older subscriptions did not create ledger reservations. A durable
// consume log still proves that their "consumed" preconsume row is terminal.
func (d *commerceData) loadCompletedRequests(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	d.completedRequests = map[string]int64{}
	requests := d.consumedSubscriptionRequests()
	if len(requests) == 0 || sources["logs"] == "" {
		return nil
	}
	rows, err := source.Query(ctx, `SELECT l.request_id,l.user_id FROM `+sources["logs"]+` l
	 WHERE l.request_id=ANY($1::text[]) AND l.type=2`, requests)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var request string
		var user int64
		if err = rows.Scan(&request, &user); err != nil {
			return err
		}
		if existing := d.completedRequests[request]; existing != 0 && existing != user {
			d.completedRequests[request] = -1
		} else {
			d.completedRequests[request] = user
		}
	}
	return rows.Err()
}

func (d *commerceData) consumedSubscriptionRequests() []string {
	seen := map[string]bool{}
	var requests []string
	for _, record := range d.rows["subscription_pre_consume_records"] {
		state, _ := record.text("status")
		request, _ := record.text("request_id")
		if state == "consumed" && request != "" && !seen[request] {
			seen[request] = true
			requests = append(requests, request)
		}
	}
	return requests
}

func (d *commerceData) loadReservationStates(ctx context.Context, source pgx.Tx, table string) error {
	d.reservationStates = map[string][]string{}
	requests := d.consumedSubscriptionRequests()
	if len(requests) == 0 || table == "" {
		return nil
	}
	// Only consumed subscription records consult reservation evidence. Aggregate
	// all matching attempts in SQL: a single nonsettled (including NULL) status
	// rejects the request, while an absent row still requires a durable consume
	// log. This retains at most one small summary per relevant request.
	rows, err := source.Query(ctx, "SELECT request_id,bool_and(COALESCE(status,'')='settled') FROM "+table+" WHERE request_id=ANY($1::text[]) GROUP BY request_id", requests)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var request string
		var settled bool
		if err := rows.Scan(&request, &settled); err != nil {
			return err
		}
		state := "nonterminal"
		if settled {
			state = "settled"
		}
		d.reservationStates[request] = []string{state}
	}
	return rows.Err()
}
