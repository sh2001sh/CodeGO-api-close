package commerce

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type walletParticipant struct {
	id             int64
	external, name string
}

// CreateWalletTransfer serializes a request ID, verifies a locked credential,
// and commits sender debit, recipient credit, fee income and history together.
// Bad password attempts commit independently of the rejected money movement.
func (s *Service) CreateWalletTransfer(ctx context.Context, uid int64, in WalletTransferInput) (WalletTransferItem, error) {
	in.RecipientExternalID = strings.ToUpper(strings.TrimSpace(in.RecipientExternalID))
	in.RequestID = strings.TrimSpace(in.RequestID)
	var result WalletTransferItem
	if uid <= 0 || s.poster == nil || !validWalletExternalID(in.RecipientExternalID) || in.Amount < walletMinimum || in.Amount%walletMinimum != 0 || in.RequestID == "" || len(in.RequestID) > 128 || strings.ContainsRune(in.RequestID, 0) || in.PaymentPassword == "" || len(in.PaymentPassword) > 72 {
		return result, ErrInvalid
	}
	fee := walletFee(in.Amount)
	total, err := in.Amount.Add(fee)
	if err != nil {
		return result, ErrInvalid
	}
	var authErr error
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "wallet-transfer-request:"+in.RequestID); err != nil {
			return err
		}
		var recipientID int64
		err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE external_id=$1 AND status='active' AND deleted_at IS NULL`, in.RecipientExternalID).Scan(&recipientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if recipientID == uid {
			return ErrWalletSelf
		}
		participants, err := lockWalletParticipants(ctx, tx, uid, recipientID)
		if err != nil {
			return err
		}
		if err = s.authorizeWalletTransferTx(ctx, tx, uid, in.PaymentPassword, &authErr); err != nil || authErr != nil {
			return err
		}
		reused, err := reuseWalletTransferTx(ctx, tx, uid, recipientID, in, &result)
		if reused || err != nil {
			return err
		}
		result, err = s.insertWalletTransferTx(ctx, tx, uid, recipientID, in, fee, total, participants)
		return err
	})
	if err != nil {
		if errors.Is(err, ledger.ErrWalletRewardTransferLocked) {
			return WalletTransferItem{}, ErrWalletRewardLocked
		}
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			return WalletTransferItem{}, ErrWalletInsufficient
		}
		return WalletTransferItem{}, err
	}
	if authErr != nil {
		return WalletTransferItem{}, authErr
	}
	return result, nil
}

// authorizeWalletTransferTx verifies the sender's payment password. A bad
// password is reported through authErr so the caller commits the attempt
// independently of the rejected money movement, rather than returning err
// (which would roll the transaction back).
func (s *Service) authorizeWalletTransferTx(ctx context.Context, tx pgx.Tx, uid int64, password string, authErr *error) error {
	credential, err := walletCredentialTx(ctx, tx, uid)
	if err == nil && credential.hash == "" {
		err = ErrWalletPasswordNotSet
	}
	if err == nil {
		err = s.verifyWalletCredentialTx(ctx, tx, uid, credential, credential.hash, password, ErrWalletPasswordWrong)
	}
	if err != nil {
		if isWalletAuthError(err) {
			*authErr = err
			return nil
		}
		return err
	}
	return nil
}

// reuseWalletTransferTx returns the previously committed transfer when this
// request ID was already applied with the same parties and amount.
func reuseWalletTransferTx(ctx context.Context, tx pgx.Tx, uid, recipientID int64, in WalletTransferInput, result *WalletTransferItem) (bool, error) {
	var existingAmount int64
	var senderID, receiverID int64
	err := tx.QueryRow(ctx, `SELECT sender_user_id,recipient_user_id,amount FROM v3_commerce.wallet_transfers WHERE request_id=$1`, in.RequestID).Scan(&senderID, &receiverID, &existingAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if senderID != uid || receiverID != recipientID || existingAmount != int64(in.Amount) {
		return true, ErrStateConflict
	}
	*result, err = walletTransferItem(tx.QueryRow(ctx, walletTransferSelect+` WHERE t.request_id=$2 AND (t.sender_user_id=$1 OR t.recipient_user_id=$1)`, uid, in.RequestID))
	return true, err
}

// insertWalletTransferTx locks the sender/recipient/platform accounts in a
// deadlock-safe order, posts the debit/credit/fee ledger entries, and
// records the transfer row.
func (s *Service) insertWalletTransferTx(ctx context.Context, tx pgx.Tx, uid, recipientID int64, in WalletTransferInput, fee, total credits.Micro, participants map[int64]walletParticipant) (WalletTransferItem, error) {
	var result WalletTransferItem
	accounts, err := walletTransferAccounts(ctx, tx, uid, recipientID)
	if err != nil {
		return result, err
	}
	if accounts[uid].balance < total {
		return result, ErrWalletInsufficient
	}
	available, err := ledger.TransferableBalanceTx(ctx, tx, accounts[uid].id, s.cfg.Now())
	if err != nil {
		return result, err
	}
	if available < total {
		return result, ErrWalletRewardLocked
	}
	meta := map[string]any{"request_id": in.RequestID, "sender_user_id": uid, "recipient_user_id": recipientID, "amount_micro": int64(in.Amount), "fee_micro": int64(fee)}
	debit, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: accounts[uid].id, Amount: -total, Kind: "transfer", OperationID: "wallet-transfer:" + in.RequestID + ":debit", Reason: "wallet_peer_transfer_debit", Metadata: meta})
	if err != nil {
		return result, err
	}
	credit, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: accounts[recipientID].id, Amount: in.Amount, Kind: "transfer", OperationID: "wallet-transfer:" + in.RequestID + ":credit", Reason: "wallet_peer_transfer_credit", Metadata: meta})
	if err != nil {
		return result, err
	}
	if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: accounts[0].id, Amount: fee, Kind: "transfer", OperationID: "wallet-transfer:" + in.RequestID + ":fee", Reason: "wallet_peer_transfer_fee", Metadata: meta}); err != nil {
		return result, err
	}
	sender, receiver := participants[uid], participants[recipientID]
	created := s.cfg.Now()
	if err = tx.QueryRow(ctx, `INSERT INTO v3_commerce.wallet_transfers(request_id,sender_user_id,recipient_user_id,sender_external_id,recipient_external_id,sender_display_name_masked,recipient_display_name_masked,amount,fee,total_debit,sender_balance_after,recipient_balance_after,created_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, in.RequestID, uid, recipientID, sender.external, receiver.external, maskWalletName(sender.name), maskWalletName(receiver.name), int64(in.Amount), int64(fee), int64(total), int64(debit.Balance), int64(credit.Balance), created).Scan(&result.ID); err != nil {
		return result, err
	}
	return WalletTransferItem{ID: result.ID, RequestID: in.RequestID, Direction: "outgoing", CounterpartyExternalID: receiver.external, CounterpartyDisplayName: maskWalletName(receiver.name), Amount: in.Amount, Fee: fee, TotalDebit: total, BalanceAfter: debit.Balance, Status: "completed", CreatedAt: created.Unix()}, nil
}

func lockWalletParticipants(ctx context.Context, tx pgx.Tx, sender, recipient int64) (map[int64]walletParticipant, error) {
	rows, err := tx.Query(ctx, `SELECT id,coalesce(external_id,''),coalesce(nullif(trim(display_name),''),username) FROM v3_identity.users WHERE id=ANY($1) AND status='active' AND deleted_at IS NULL ORDER BY id FOR SHARE`, []int64{sender, recipient})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]walletParticipant, 2)
	for rows.Next() {
		var p walletParticipant
		if err = rows.Scan(&p.id, &p.external, &p.name); err != nil {
			return nil, err
		}
		out[p.id] = p
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 2 {
		return nil, ErrNotFound
	}
	return out, nil
}

type walletTransferAccount struct {
	id      int64
	balance credits.Micro
}

func walletTransferAccounts(ctx context.Context, tx pgx.Tx, sender, recipient int64) (map[int64]walletTransferAccount, error) {
	ids := []int64{sender, recipient}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	// DO NOTHING avoids taking existing row locks before we can order by the
	// actual account IDs. This also prevents opposite-direction transfer deadlocks.
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) SELECT 'user',id,'wallet' FROM unnest($1::bigint[]) AS id ORDER BY id ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, ids); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',1,'platform_revenue') ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,CASE WHEN owner_type='platform' THEN 0 ELSE owner_id END,balance FROM v3_billing.accounts WHERE (owner_type='user' AND owner_id=ANY($1) AND kind='wallet') OR (owner_type='platform' AND owner_id=1 AND kind='platform_revenue') ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]walletTransferAccount, 3)
	for rows.Next() {
		var owner int64
		var a walletTransferAccount
		if err = rows.Scan(&a.id, &owner, &a.balance); err != nil {
			return nil, err
		}
		out[owner] = a
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 3 {
		return nil, ErrNotFound
	}
	return out, nil
}
