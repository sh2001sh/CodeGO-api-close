package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type sourceUser struct {
	ID             int64   `json:"id"`
	ExternalID     string  `json:"external_id"`
	Username       string  `json:"username"`
	DisplayName    string  `json:"display_name"`
	Email          string  `json:"email"`
	Password       string  `json:"password"`
	Role           int     `json:"role"`
	Status         int     `json:"status"`
	Group          string  `json:"group"`
	GPTUnits       int64   `json:"quota"`
	WalletUnits    int64   `json:"claude_quota"`
	Setting        string  `json:"setting"`
	Remark         string  `json:"remark"`
	CreatedAt      int64   `json:"created_at"`
	LastLoginAt    int64   `json:"last_login_at"`
	DeletedAt      *string `json:"deleted_at"`
	GitHubID       string  `json:"github_id"`
	DiscordID      string  `json:"discord_id"`
	OIDCID         string  `json:"oidc_id"`
	WeChatID       string  `json:"wechat_id"`
	TelegramID     string  `json:"telegram_id"`
	LinuxDOID      string  `json:"linux_do_id"`
	InviterID      int64   `json:"inviter_id"`
	AffCode        string  `json:"aff_code"`
	AffCount       int64   `json:"aff_count"`
	AffUnits       int64   `json:"aff_quota"`
	AffHistory     int64   `json:"aff_history"`
	UsedUnits      int64   `json:"used_quota"`
	RequestCount   int64   `json:"request_count"`
	StripeCustomer string  `json:"stripe_customer"`
	wallet         Wallet
}

func loadUsers(ctx context.Context, tx pgx.Tx, sources map[string]string) ([]sourceUser, error) {
	raw, err := loadRows(ctx, tx, sources["users"])
	if err != nil {
		return nil, err
	}
	users := make([]sourceUser, len(raw))
	for i, row := range raw {
		if err = json.Unmarshal(row, &users[i]); err != nil {
			return nil, fmt.Errorf("legacy: decode user: %w", err)
		}
		u := &users[i]
		u.wallet = Wallet{UserID: u.ID, GPTUnits: u.GPTUnits, ProjectionUnits: u.WalletUnits}
	}
	if sources["accounts"] == "" {
		return users, nil
	}
	rows, err := tx.Query(ctx, `SELECT a.owner_id, s.available_balance, s.reserved_balance
		FROM `+sources["accounts"]+` a JOIN `+sources["balance_snapshots"]+` s ON a.account_id=s.account_id
		WHERE a.owner_type='user' AND a.account_type='claude_wallet' AND a.quota_unit='quota'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[int64]*sourceUser, len(users))
	for i := range users {
		byID[users[i].ID] = &users[i]
	}
	for rows.Next() {
		var uid, balance, reserved int64
		if err = rows.Scan(&uid, &balance, &reserved); err != nil {
			return nil, err
		}
		if u := byID[uid]; u != nil {
			if u.wallet.SnapshotUnits != nil {
				return nil, fmt.Errorf("legacy: duplicate canonical wallet for user %d", uid)
			}
			u.wallet.SnapshotUnits, u.wallet.ReservedUnits = &balance, reserved
		} else {
			return nil, fmt.Errorf("legacy: canonical wallet references missing user %d", uid)
		}
	}
	return users, rows.Err()
}

func (m *Importer) importUsers(ctx context.Context, tx pgx.Tx, users []sourceUser) error {
	for _, u := range users {
		role, status := "user", "active"
		if u.Role >= 100 {
			role = "root"
		} else if u.Role >= 10 {
			role = "admin"
		}
		if u.Status != 1 {
			status = "disabled"
		}
		if u.Group == "" {
			u.Group = "default"
		}
		if u.Setting == "" {
			u.Setting = "{}"
		}
		if !json.Valid([]byte(u.Setting)) {
			return fmt.Errorf("legacy: user %d has invalid settings JSON", u.ID)
		}
		_, err := tx.Exec(ctx, `INSERT INTO v3_identity.users
			(id,external_id,username,display_name,email,password_hash,role,status,group_name,settings,remark,created_at,last_login_at,deleted_at,aff_code)
			VALUES ($1,NULLIF($2,''),$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10::jsonb,$11,
			CASE WHEN $12::bigint>0 THEN to_timestamp($12) ELSE now() END,
			CASE WHEN $13::bigint>0 THEN to_timestamp($13) END,$14::timestamptz,NULLIF($15,'')) ON CONFLICT(id) DO NOTHING`,
			u.ID, u.ExternalID, u.Username, u.DisplayName, u.Email, u.Password, role, status, u.Group, u.Setting, u.Remark, u.CreatedAt, u.LastLoginAt, u.DeletedAt, u.AffCode)
		if err != nil {
			return fmt.Errorf("legacy: import user %d: %w", u.ID, err)
		}
		var username string
		if err = tx.QueryRow(ctx, `SELECT username FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&username); err != nil {
			return err
		}
		if username != u.Username {
			return fmt.Errorf("legacy: target user ID %d belongs to a different identity", u.ID)
		}
		used, err := FromV2Units(u.UsedUnits)
		if err != nil {
			return err
		}
		affHistory, err := FromV2Units(u.AffHistory)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_identity.users SET stripe_customer=$2,affiliate_count=$3,affiliate_history=$4,used_credits=$5,request_count=$6
			WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM v3_billing.ledger_entries e JOIN v3_billing.accounts a ON a.id=e.account_id WHERE a.owner_type='user' AND a.owner_id=$1 AND e.kind<>'opening')`, u.ID, u.StripeCustomer, u.AffCount, int64(affHistory), int64(used), u.RequestCount); err != nil {
			return err
		}
		for provider, subject := range map[string]string{"github": u.GitHubID, "discord": u.DiscordID, "oidc": u.OIDCID, "wechat": u.WeChatID, "telegram": u.TelegramID, "linux_do": u.LinuxDOID} {
			if subject == "" {
				continue
			}
			var linkedUser int64
			if err = tx.QueryRow(ctx, `INSERT INTO v3_identity.user_identities(provider,subject,user_id) VALUES($1,$2,$3)
				ON CONFLICT(provider,subject) DO UPDATE SET subject=EXCLUDED.subject RETURNING user_id`, provider, subject, u.ID).Scan(&linkedUser); err != nil {
				return err
			}
			if linkedUser != u.ID {
				return fmt.Errorf("legacy: %s identity collision for user %d", provider, u.ID)
			}
		}
		amount, _ := ValidateWallet(u.wallet)
		if err = opening(ctx, tx, "user", u.ID, "wallet", int64(amount)); err != nil {
			return err
		}
		if u.AffUnits > 0 {
			affiliate, err := OpeningBalance(u.AffUnits)
			if err != nil {
				return err
			}
			if err = opening(ctx, tx, "user", u.ID, "affiliate", int64(affiliate)); err != nil {
				return err
			}
		}
	}
	for _, u := range users {
		if u.InviterID <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE v3_identity.users SET inviter_id=$2 WHERE id=$1 AND inviter_id IS NULL
			AND EXISTS(SELECT 1 FROM v3_identity.users WHERE id=$2)`, u.ID, u.InviterID); err != nil {
			return err
		}
	}
	return nil
}

func opening(ctx context.Context, tx pgx.Tx, owner string, id int64, kind string, amount int64) error {
	var account int64
	err := tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES($1,$2,$3)
		ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, owner, id, kind).Scan(&account)
	if err != nil {
		return err
	}
	operation := fmt.Sprintf("v2-import:%s:%d:%s", owner, id, kind)
	var existing int64
	err = tx.QueryRow(ctx, `SELECT amount FROM v3_billing.ledger_entries WHERE operation_id=$1`, operation).Scan(&existing)
	if err == nil {
		if existing != amount {
			return fmt.Errorf("legacy: opening amount changed for %s %d", owner, id)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var balance, version int64
	if err = tx.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance, &version); err != nil {
		return err
	}
	if balance != 0 || version != 0 {
		return fmt.Errorf("legacy: target account %d already has activity", account)
	}
	if amount == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_billing.ledger_entries(account_id,amount,balance_after,kind,operation_id,reason)
		VALUES($1,$2,$2,'opening',$3,'offline v2 migration')`, account, amount, operation); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_billing.accounts SET balance=$2,version=1 WHERE id=$1 AND version=0`, account, amount)
	return err
}
