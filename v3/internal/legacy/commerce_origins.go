package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

// These verified remaining paid lots let the new refund service distinguish
// paid funds from gifts and transfers in the imported wallet opening balance.
type commerceRefundOrigin struct {
	OrderID, UserID, Original, Remaining int64
}

func (d *commerceData) loadRefundOrigins(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	table := sources["billing_funding_lots"]
	if table == "" {
		table = sources["funding_lots"]
	}
	if table == "" {
		return nil
	}
	accounts := map[string]int64{}
	rows, err := loadRows(ctx, source, sources["accounts"])
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var account struct {
			ID     string `json:"account_id"`
			Owner  string `json:"owner_type"`
			Kind   string `json:"account_type"`
			Unit   string `json:"quota_unit"`
			UserID int64  `json:"owner_id"`
		}
		if err = json.Unmarshal(raw, &account); err != nil {
			return err
		}
		if account.Owner == "user" && account.Kind == "claude_wallet" && account.Unit == "quota" {
			accounts[account.ID] = account.UserID
		}
	}
	orders := map[string]commerceRow{}
	for _, row := range d.rows["top_ups"] {
		trade, _ := row.text("trade_no")
		orders[trade] = row
	}
	rows, err = loadRows(ctx, source, table)
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var row struct {
			Source    string `json:"source"`
			Account   string `json:"account_id"`
			Key       string `json:"idempotency_key"`
			Original  int64  `json:"original_amount"`
			Remaining int64  `json:"remaining_amount"`
		}
		if err = json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Source != "topup" {
			continue
		}
		if !strings.HasPrefix(row.Key, "topup:") || !strings.HasSuffix(row.Key, ":unified") {
			return fmt.Errorf("legacy: paid funding lot has unsupported topup attribution")
		}
		trade := strings.TrimSuffix(strings.TrimPrefix(row.Key, "topup:"), ":unified")
		order := orders[trade]
		if order == nil {
			return fmt.Errorf("legacy: paid funding lot references missing topup order")
		}
		projection, err := d.projectOrder("top_ups", order)
		if err != nil {
			return err
		}
		uid := projection.values["user_id"].(int64)
		if accounts[row.Account] != uid {
			return fmt.Errorf("legacy: topup funding lot owner differs from payment owner")
		}
		original, err := OpeningBalance(row.Original)
		if err != nil {
			return err
		}
		remaining, err := OpeningBalance(row.Remaining)
		if err != nil {
			return err
		}
		if row.Original <= 0 || row.Remaining > row.Original || int64(original) != projection.values["credits"].(int64) {
			return fmt.Errorf("legacy: paid funding lot amounts differ from topup grant")
		}
		d.refundOrigins = append(d.refundOrigins, commerceRefundOrigin{projection.values["id"].(int64), uid, int64(original), int64(remaining)})
	}
	return nil
}

func (d *commerceData) validateRefundOrigins(report *Report) {
	seen := map[int64]bool{}
	sums := map[int64]*big.Int{}
	for _, origin := range d.refundOrigins {
		if seen[origin.OrderID] {
			report.Issues = append(report.Issues, Issue{"top_ups", origin.OrderID, "duplicate_refund_origin", "multiple source funding lots for one paid topup"})
		}
		seen[origin.OrderID] = true
		if sums[origin.UserID] == nil {
			sums[origin.UserID] = new(big.Int)
		}
		sums[origin.UserID].Add(sums[origin.UserID], big.NewInt(origin.Remaining))
	}
	for _, row := range d.rows["users"] {
		uid, _ := row.integer("id")
		if sums[uid] == nil {
			continue
		}
		wallet, err := commerceUnits(row, "claude_quota")
		if err != nil || sums[uid].Cmp(big.NewInt(wallet)) > 0 {
			report.Issues = append(report.Issues, Issue{"user", uid, "refund_origin_exceeds_wallet", "verifiable paid-origin remainder exceeds imported wallet balance"})
		}
	}
	report.Counts["topup_refund_origins"] = int64(len(d.refundOrigins))
}

func (m *Importer) importRefundOrigins(ctx context.Context, tx pgx.Tx, d *commerceData) error {
	for _, origin := range d.refundOrigins {
		var account, cursor int64
		err := tx.QueryRow(ctx, `SELECT a.id,e.id FROM v3_billing.accounts a JOIN v3_billing.ledger_entries e ON e.account_id=a.id
			WHERE a.owner_type='user' AND a.owner_id=$1 AND a.kind='wallet' AND e.operation_id=$2`, origin.UserID, fmt.Sprintf("v2-import:user:%d:wallet", origin.UserID)).Scan(&account, &cursor)
		if err == pgx.ErrNoRows && origin.Remaining == 0 {
			continue
		}
		if err != nil {
			return fmt.Errorf("legacy: refund origin has no canonical opening anchor: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.user_refund_origins(order_id,account_id,original_credits,remaining_credits,ledger_cursor)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT(order_id) DO NOTHING`, origin.OrderID, account, origin.Original, origin.Remaining, cursor); err != nil {
			return err
		}
	}
	return nil
}

func (m *Importer) checkRefundOrigins(ctx context.Context, tx pgx.Tx, d *commerceData, report *Report) error {
	for _, origin := range d.refundOrigins {
		var matches bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.user_refund_origins f JOIN v3_billing.accounts a ON a.id=f.account_id
		 JOIN v3_billing.ledger_entries e ON e.id=f.ledger_cursor WHERE f.order_id=$1 AND a.owner_type='user' AND a.owner_id=$2 AND a.kind='wallet'
		 AND f.original_credits=$3 AND f.remaining_credits=$4 AND e.account_id=a.id AND e.operation_id=$5)`, origin.OrderID, origin.UserID, origin.Original, origin.Remaining, fmt.Sprintf("v2-import:user:%d:wallet", origin.UserID)).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches && origin.Remaining > 0 {
			checkIssue(report, "top_ups", origin.OrderID, "verified paid funding origin or opening watermark differs")
		}
	}
	return nil
}
