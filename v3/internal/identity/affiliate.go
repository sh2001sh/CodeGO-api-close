package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var ErrAffiliateFunds = errors.New("identity: insufficient affiliate funds")

type AffiliateTransferInput struct {
	AmountMicroCredits int64  `json:"amount_micro_credits"`
	OperationID        string `json:"operation_id"`
}

type AffiliateTransfer struct {
	AffiliateTransferInput
	AffiliateMicroCredits int64 `json:"affiliate_micro_credits"`
	WalletMicroCredits    int64 `json:"wallet_micro_credits"`
}

func (c *Control) TransferAffiliate(ctx context.Context, uid int64, in AffiliateTransferInput) (AffiliateTransfer, error) {
	var out AffiliateTransfer
	if uid <= 0 || in.AmountMicroCredits < 1000000 || !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`).MatchString(in.OperationID) {
		return out, ErrInvalidInput
	}
	if c.cfg.BudgetPoster == nil {
		return out, errors.New("identity: monetary ledger writer unavailable")
	}
	err := pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		var active int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&active); err != nil {
			return controlDBError(err)
		}
		replay, found, err := loadAffiliateTransferReplay(ctx, tx, uid, in)
		if err != nil {
			return err
		}
		if found {
			out = replay
			return nil
		}
		out, err = c.executeAffiliateTransfer(ctx, tx, uid, in)
		return err
	})
	if err != nil {
		return AffiliateTransfer{}, err
	}
	return out, nil
}

// loadAffiliateTransferReplay checks for a prior transfer with the same
// operation ID. found=true means the transaction should return replay
// as-is (after idempotency validation) without re-executing the transfer.
func loadAffiliateTransferReplay(ctx context.Context, tx pgx.Tx, uid int64, in AffiliateTransferInput) (replay AffiliateTransfer, found bool, err error) {
	err = tx.QueryRow(ctx, `SELECT amount,affiliate_balance,wallet_balance FROM v3_identity.affiliate_transfers WHERE user_id=$1 AND operation_id=$2`, uid, in.OperationID).
		Scan(&replay.AmountMicroCredits, &replay.AffiliateMicroCredits, &replay.WalletMicroCredits)
	if err == nil {
		if replay.AmountMicroCredits != in.AmountMicroCredits {
			return AffiliateTransfer{}, false, ErrDuplicate
		}
		replay.OperationID = in.OperationID
		return replay, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AffiliateTransfer{}, false, err
	}
	return AffiliateTransfer{}, false, nil
}

// executeAffiliateTransfer debits the user's affiliate account, credits
// their wallet, and records the transfer. Called only after
// loadAffiliateTransferReplay found no prior record.
func (c *Control) executeAffiliateTransfer(ctx context.Context, tx pgx.Tx, uid int64, in AffiliateTransferInput) (AffiliateTransfer, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, uid); err != nil {
		return AffiliateTransfer{}, err
	}
	wallet, affiliate, walletBalance, affiliateBalance, err := loadAffiliateWalletAccounts(ctx, tx, uid)
	if err != nil {
		return AffiliateTransfer{}, err
	}
	if affiliate == 0 || affiliateBalance < in.AmountMicroCredits {
		return AffiliateTransfer{}, ErrAffiliateFunds
	}
	if _, err = credits.Micro(walletBalance).Add(credits.Micro(in.AmountMicroCredits)); err != nil {
		return AffiliateTransfer{}, err
	}
	operation := fmt.Sprintf("affiliate-transfer:%d:%s", uid, in.OperationID)
	meta := map[string]any{"user_id": uid, "amount_micro_credits": in.AmountMicroCredits}
	debit, err := c.cfg.BudgetPoster.PostTx(ctx, tx, billing.Entry{AccountID: affiliate, Amount: -credits.Micro(in.AmountMicroCredits), Kind: "transfer", OperationID: operation + ":debit", Reason: "affiliate_wallet_transfer_debit", Metadata: meta})
	if err != nil {
		return AffiliateTransfer{}, err
	}
	credit, err := c.cfg.BudgetPoster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: credits.Micro(in.AmountMicroCredits), Kind: "transfer", OperationID: operation + ":credit", Reason: "affiliate_wallet_transfer_credit", Metadata: meta})
	if err != nil {
		return AffiliateTransfer{}, err
	}
	out := AffiliateTransfer{AffiliateTransferInput: in, AffiliateMicroCredits: int64(debit.Balance), WalletMicroCredits: int64(credit.Balance)}
	_, err = tx.Exec(ctx, `INSERT INTO v3_identity.affiliate_transfers(user_id,operation_id,amount,affiliate_balance,wallet_balance) VALUES($1,$2,$3,$4,$5)`, uid, in.OperationID, in.AmountMicroCredits, out.AffiliateMicroCredits, out.WalletMicroCredits)
	if err != nil {
		return AffiliateTransfer{}, err
	}
	return out, nil
}

// loadAffiliateWalletAccounts locks and reads the user's wallet and
// affiliate accounts in id order.
func loadAffiliateWalletAccounts(ctx context.Context, tx pgx.Tx, uid int64) (wallet, affiliate, walletBalance, affiliateBalance int64, err error) {
	rows, err := tx.Query(ctx, `SELECT id,kind,balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind IN('wallet','affiliate') ORDER BY id FOR UPDATE`, uid)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for rows.Next() {
		var id, balance int64
		var kind string
		if err = rows.Scan(&id, &kind, &balance); err != nil {
			rows.Close()
			return 0, 0, 0, 0, err
		}
		if kind == "wallet" {
			wallet, walletBalance = id, balance
		} else {
			affiliate, affiliateBalance = id, balance
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, 0, 0, 0, err
	}
	return wallet, affiliate, walletBalance, affiliateBalance, nil
}
