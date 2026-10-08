package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *UserRefunds) Create(ctx context.Context, uid int64, in UserRefundRequest) (UserRefundResult, error) {
	if uid <= 0 || strings.TrimSpace(in.TradeNo) == "" || (in.OrderType != "balance" && in.OrderType != "subscription") {
		return UserRefundResult{}, ErrInvalid
	}
	if s.provider == nil || s.poster == nil {
		return UserRefundResult{}, ErrProviderUnavailable
	}
	o, err := scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND trade_no=$2`, uid, in.TradeNo))
	if err != nil {
		return UserRefundResult{}, err
	}
	if (o.Kind == "subscription") != (in.OrderType == "subscription") {
		return UserRefundResult{}, ErrInvalid
	}
	var existing string
	err = s.pool.QueryRow(ctx, `SELECT refund_no FROM v3_commerce.user_refunds WHERE order_id=$1 AND status<>'failed'`, o.ID).Scan(&existing)
	if err == nil {
		return s.dispatch(ctx, uid, existing, false)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UserRefundResult{}, err
	}
	account, _, err := s.refundAccount(ctx, s.pool, o, false)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return UserRefundResult{}, err
		}
		return UserRefundResult{}, ErrRefundUnavailable
	}
	token, err := s.leaseAccount(ctx, account)
	if err != nil {
		return UserRefundResult{}, err
	}
	var record userRefundRecord
	initial := true
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var e error
		record, initial, e = s.createUserRefundTx(ctx, tx, uid, account, token, o)
		return e
	})
	// Use a fresh bounded context even if the browser disconnected. The durable
	// lease remains recoverable by Run when Redis is temporarily unavailable.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	releaseErr := s.releaseAccount(cleanup, account, token)
	if err != nil || releaseErr != nil {
		return UserRefundResult{}, errors.Join(err, releaseErr)
	}
	return s.dispatch(ctx, uid, record.RefundNo, initial)
}

// createUserRefundTx locks the order and reuses a racing caller's reservation
// if one landed first, otherwise validates and creates a new refund record.
func (s *UserRefunds) createUserRefundTx(ctx context.Context, tx pgx.Tx, uid, account int64, token string, o Order) (userRefundRecord, bool, error) {
	var record userRefundRecord
	var stored string
	if e := tx.QueryRow(ctx, `SELECT token FROM v3_commerce.user_refund_freezes WHERE account_id=$1 FOR UPDATE`, account).Scan(&stored); e != nil {
		return record, true, e
	}
	if stored != token {
		return record, true, ErrStateConflict
	}
	var e error
	o, e = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, o.ID, uid))
	if e != nil {
		return record, true, e
	}
	// A racing caller may have committed its reservation between the
	// first lookup and this order lock. Reuse it without locking its row
	// here (dispatch locks refund then order, so reversing that deadlocks).
	record, e = scanUserRefund(tx.QueryRow(ctx, `SELECT `+userRefundColumns+` FROM v3_commerce.user_refunds r JOIN v3_commerce.orders o ON o.id=r.order_id WHERE r.order_id=$1 AND r.status<>'failed'`, o.ID))
	if e == nil {
		return record, false, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return record, true, e
	}
	if o.Provider != "epay" || o.Currency != "cny" || o.State != "paid" {
		return record, true, ErrRefundUnavailable
	}
	record, e = s.reserveUserRefundTx(ctx, tx, uid, account, token, o)
	return record, true, e
}

// reserveUserRefundTx verifies the account still backs this order, freezes
// it against concurrent usage, and computes the refundable quote.
func (s *UserRefunds) reserveUserRefundTx(ctx context.Context, tx pgx.Tx, uid, account int64, token string, o Order) (userRefundRecord, error) {
	var record userRefundRecord
	var lockedUser int64
	if e := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, uid).Scan(&lockedUser); e != nil {
		return record, e
	}
	currentAccount, sub, e := s.refundAccount(ctx, tx, o, true)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return record, e
	}
	if e != nil || currentAccount != account {
		return record, ErrRefundUnavailable
	}
	originalState := ""
	if sub > 0 {
		if e = tx.QueryRow(ctx, `SELECT state FROM v3_commerce.subscriptions WHERE id=$1`, sub).Scan(&originalState); e != nil {
			return record, e
		}
	}
	if e = s.freezeTx(ctx, tx, account, token, originalState == "expired"); e != nil {
		return record, e
	}
	remaining, reserved, e := s.refundableBalance(ctx, tx, o, account)
	if e != nil {
		return record, e
	}
	quote := refundQuote(o, remaining, o.Credits, "")
	if !quote.Refundable {
		return record, ErrRefundUnavailable
	}
	var providerOrder string
	if e = tx.QueryRow(ctx, `SELECT coalesce(payment_event_id,'') FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&providerOrder); e != nil {
		return record, e
	}
	if providerOrder == "" {
		return record, ErrRefundUnavailable
	}
	no, e := tradeNumber()
	if e != nil {
		return record, e
	}
	no = "RF" + strings.TrimPrefix(no, "v3_")
	gross := proportionalMinor(o.AmountMinor, remaining, o.Credits)
	fee := proportionalMinor(gross, 2, 100)
	return s.insertUserRefundTx(ctx, tx, insertUserRefundParams{uid: uid, account: account, sub: sub, originalState: originalState,
		providerOrder: providerOrder, no: no, gross: gross, fee: fee, remaining: remaining, reserved: reserved, order: o})
}

type insertUserRefundParams struct {
	uid, account, sub   int64
	originalState       string
	providerOrder, no   string
	gross, fee          int64
	remaining, reserved credits.Micro
	order               Order
}

// insertUserRefundTx creates the refund row, posts the reservation ledger
// entry when credits are being held, and cancels the subscription if any.
func (s *UserRefunds) insertUserRefundTx(ctx context.Context, tx pgx.Tx, p insertUserRefundParams) (userRefundRecord, error) {
	var record userRefundRecord
	_, e := tx.Exec(ctx, `INSERT INTO v3_commerce.user_refunds(refund_no,order_id,user_id,account_id,subscription_id,
	 original_subscription_state,provider_order_id,gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
	 VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,$8,$9,$10,$11,$12,'processing')`, p.no, p.order.ID, p.uid, p.account, p.sub,
		p.originalState, p.providerOrder, p.gross, p.fee, p.gross-p.fee, int64(p.remaining), int64(p.reserved))
	if e != nil {
		return record, e
	}
	if p.reserved > 0 {
		_, e = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: p.account, Amount: -p.reserved, Kind: "refund", OperationID: refundOperation(p.no) + ":reserve",
			Reason: "unused credit refund reserved before provider request", Metadata: map[string]any{"refund_trade_no": p.order.TradeNo, "refund_no": p.no}})
		if e != nil {
			return record, e
		}
		if e = verifyUserRefundReservationTx(ctx, tx, p); e != nil {
			return record, e
		}
	}
	if p.sub > 0 {
		if _, e = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state='canceled',ended_at=$2 WHERE id=$1`, p.sub, s.now()); e != nil {
			return record, e
		}
		if e = RestoreSubscriptionGroupTx(ctx, tx, p.sub, s.now()); e != nil {
			return record, e
		}
	}
	record, e = scanUserRefund(tx.QueryRow(ctx, `SELECT `+userRefundColumns+` FROM v3_commerce.user_refunds r JOIN v3_commerce.orders o ON o.id=r.order_id WHERE r.refund_no=$1`, p.no))
	return record, e
}
