package legacy

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Unlimited/older subscriptions did not create ledger reservations. A durable
// consume log still proves that their "consumed" preconsume row is terminal.
func (d *commerceData) loadCompletedRequests(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	d.completedRequests = map[string]int64{}
	var requests []string
	for _, record := range d.rows["subscription_pre_consume_records"] {
		state, _ := record.text("status")
		request, _ := record.text("request_id")
		if state == "consumed" && request != "" {
			requests = append(requests, request)
		}
	}
	if len(requests) == 0 || sources["logs"] == "" {
		return nil
	}
	rows, err := source.Query(ctx, `SELECT to_jsonb(l)->>'request_id',to_jsonb(l)->>'user_id' FROM `+sources["logs"]+` l
	 WHERE to_jsonb(l)->>'request_id'=ANY($1::text[]) AND to_jsonb(l)->>'type'='2'`, requests)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var request, userText string
		if err = rows.Scan(&request, &userText); err != nil {
			return err
		}
		user, err := strconv.ParseInt(userText, 10, 64)
		if err != nil {
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
