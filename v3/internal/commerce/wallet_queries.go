package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) WalletRecipient(ctx context.Context, uid int64, externalID string) (WalletRecipient, error) {
	externalID = strings.ToUpper(strings.TrimSpace(externalID))
	var out WalletRecipient
	if uid <= 0 || !validWalletExternalID(externalID) {
		return out, ErrNotFound
	}
	var recipient int64
	var name string
	err := s.pool.QueryRow(ctx, `SELECT id,external_id,coalesce(nullif(trim(display_name),''),username) FROM v3_identity.users WHERE external_id=$1 AND status='active' AND deleted_at IS NULL`, externalID).Scan(&recipient, &out.ExternalID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if uid == recipient {
		return out, ErrWalletSelf
	}
	out.DisplayNameMasked = maskWalletName(name)
	return out, nil
}

func (s *Service) WalletOverview(ctx context.Context, uid int64, page, pageSize int) (WalletOverview, error) {
	out := WalletOverview{MicroPerCredit: credits.PerCredit, MinMicro: walletMinimum, FeeBPS: walletFeeBPS}
	if uid <= 0 {
		return out, ErrInvalid
	}
	var hasAccountPassword, hasPaymentPassword bool
	var email string
	var failed int
	var locked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT coalesce(u.password_hash,'')<>'',coalesce(u.email,''),coalesce(s.password_hash,'')<>'',coalesce(s.failed_attempts,0),s.locked_until,coalesce(a.balance,0)
	 FROM v3_identity.users u LEFT JOIN v3_commerce.wallet_transfer_security s ON s.user_id=u.id LEFT JOIN v3_billing.accounts a ON a.owner_type='user' AND a.owner_id=u.id AND a.kind='wallet' WHERE u.id=$1 AND u.status='active' AND u.deleted_at IS NULL`, uid).Scan(&hasAccountPassword, &email, &hasPaymentPassword, &failed, &locked, &out.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return out, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var account int64
		err := tx.QueryRow(ctx, `SELECT id,balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet' FOR UPDATE`, uid).Scan(&account, &out.Balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.RewardLocked, err = ledger.LockedRewardAmountTx(ctx, tx, account, s.cfg.Now())
		if out.Balance > out.RewardLocked {
			out.Transferable = out.Balance - out.RewardLocked
		}
		return err
	})
	if err != nil {
		return out, err
	}
	out.Security = WalletSecurity{PasswordSet: hasPaymentPassword, RequiresAccountPassword: hasAccountPassword, RemainingPasswordAttempts: walletMaxFailures - failed, EmailBound: email != "", EmailMasked: maskWalletEmail(email), EmailRecoveryAvailable: s.cfg.WalletRecovery != nil}
	if recovery, ok := s.cfg.WalletRecovery.(interface {
		Available(context.Context) (bool, error)
	}); ok {
		out.Security.EmailRecoveryAvailable, err = recovery.Available(ctx)
		if err != nil {
			return out, err
		}
	}
	if locked != nil && locked.After(s.cfg.Now()) {
		out.Security.LockedUntil = locked.Unix()
		out.Security.RemainingPasswordAttempts = 0
	}
	out.History, err = s.WalletTransfers(ctx, uid, page, pageSize)
	return out, err
}

const walletTransferSelect = `SELECT t.id,t.request_id,CASE WHEN t.sender_user_id=$1 THEN 'outgoing' ELSE 'incoming' END,
 CASE WHEN t.sender_user_id=$1 THEN t.recipient_external_id ELSE t.sender_external_id END,
 CASE WHEN t.sender_user_id=$1 THEN t.recipient_display_name_masked ELSE t.sender_display_name_masked END,
 t.amount,t.fee,t.total_debit,CASE WHEN t.sender_user_id=$1 THEN t.sender_balance_after ELSE t.recipient_balance_after END,t.status,t.created_at FROM v3_commerce.wallet_transfers t`

func walletTransferItem(row scanner) (WalletTransferItem, error) {
	var item WalletTransferItem
	var created time.Time
	err := row.Scan(&item.ID, &item.RequestID, &item.Direction, &item.CounterpartyExternalID, &item.CounterpartyDisplayName, &item.Amount, &item.Fee, &item.TotalDebit, &item.BalanceAfter, &item.Status, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	item.CreatedAt = created.Unix()
	return item, err
}

func (s *Service) WalletTransfers(ctx context.Context, uid int64, page, pageSize int) (WalletHistory, error) {
	if uid <= 0 {
		return WalletHistory{}, ErrInvalid
	}
	page = max(1, min(page, 1_000_000))
	if pageSize <= 0 {
		pageSize = 10
	}
	pageSize = min(pageSize, 50)
	out := WalletHistory{Page: page, PageSize: pageSize, Items: make([]WalletTransferItem, 0)}
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.wallet_transfers WHERE sender_user_id=$1 OR recipient_user_id=$1`, uid).Scan(&out.Total)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, walletTransferSelect+` WHERE t.sender_user_id=$1 OR t.recipient_user_id=$1 ORDER BY t.id DESC LIMIT $2 OFFSET $3`, uid, pageSize, int64(page-1)*int64(pageSize))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := walletTransferItem(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}
