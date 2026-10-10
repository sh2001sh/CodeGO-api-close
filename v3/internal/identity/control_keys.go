package identity

import (
	"context"
	"crypto/rand"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

type KeyInput struct {
	ID                          int64      `json:"id"`
	Name                        string     `json:"name"`
	Status                      string     `json:"status"`
	Group                       *string    `json:"group"`
	AllowedModels               []string   `json:"allowed_models"`
	AllowedCIDRs                []string   `json:"allowed_cidrs"`
	ExpiresAt                   *time.Time `json:"expires_at"`
	BudgetLimited               bool       `json:"budget_limited"`
	BudgetMicroCredits          *int64     `json:"budget_micro_credits,omitempty"`
	CrossGroupRetry             bool       `json:"cross_group_retry"`
	MaxMarketplaceMultiplierPPM int64      `json:"max_marketplace_multiplier_ppm"`
}

type KeyRecord struct {
	KeyInput
	Prefix            string     `json:"key_prefix"`
	CreatedAt         time.Time  `json:"created_at"`
	LastUsedAt        *time.Time `json:"last_used_at"`
	BudgetAccountID   int64      `json:"budget_account_id"`
	UserID            int64      `json:"user_id"`
	SpentMicroCredits int64      `json:"spent_micro_credits"`
}

func validateKey(in *KeyInput) error {
	if len(in.Name) > 100 || len(in.AllowedCIDRs) > 100 || len(in.AllowedModels) > 1000 {
		return ErrInvalidInput
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "disabled" {
		return ErrInvalidInput
	}
	if in.Group != nil && *in.Group == "" {
		in.Group = nil
	}
	if in.Group != nil && len(*in.Group) > 100 {
		return ErrInvalidInput
	}
	if in.BudgetMicroCredits != nil && (*in.BudgetMicroCredits < 0 || !in.BudgetLimited) || in.MaxMarketplaceMultiplierPPM < 0 {
		return ErrInvalidInput
	}
	for _, cidr := range in.AllowedCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

func (c *Control) encryptKey(raw string) ([]byte, error) {
	nonce := make([]byte, c.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.cipher.Seal(nonce, nonce, []byte(raw), nil), nil
}

func (c *Control) decryptKey(b []byte) (string, error) {
	n := c.cipher.NonceSize()
	if len(b) < n+c.cipher.Overhead() {
		return "", ErrInvalidInput
	}
	raw, err := c.cipher.Open(nil, b[:n], b[n:], nil)
	return string(raw), err
}

func (c *Control) CreateKey(ctx context.Context, uid int64, in KeyInput) (KeyRecord, string, error) {
	if err := validateKey(&in); err != nil {
		return KeyRecord{}, "", err
	}
	raw, hash, prefix, err := GenerateKey()
	if err != nil {
		return KeyRecord{}, "", err
	}
	encrypted, err := c.encryptKey(raw)
	if err != nil {
		return KeyRecord{}, "", err
	}
	k := KeyRecord{KeyInput: in, Prefix: prefix, UserID: uid}
	err = pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if err := c.validateKeyGroup(ctx, tx, uid, &in); err != nil {
			return err
		}
		k.KeyInput = in
		err := tx.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,name,key_hash,key_prefix,key_ciphertext,status,allowed_models,allowed_cidrs,expires_at,group_name,budget_limited,cross_group_retry,max_marketplace_multiplier)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::cidr[],$9,$10,$11,$12,$13::numeric/1000000) RETURNING id,created_at`, uid, in.Name, hash[:], prefix, encrypted, in.Status, in.AllowedModels, in.AllowedCIDRs, in.ExpiresAt, in.Group, in.BudgetLimited, in.CrossGroupRetry, in.MaxMarketplaceMultiplierPPM).Scan(&k.ID, &k.CreatedAt)
		if err != nil {
			return err
		}
		if in.BudgetLimited {
			id, err := c.setKeyBudget(ctx, tx, k.ID, in.BudgetMicroCredits)
			k.BudgetAccountID = id
			if k.BudgetMicroCredits == nil {
				zero := int64(0)
				k.BudgetMicroCredits = &zero
			}
			return err
		}
		return nil
	})
	if err != nil {
		return KeyRecord{}, "", controlDBError(err)
	}
	return k, raw, controlDBError(err)
}

const keyColumns = `k.id,k.name,k.status,k.group_name,k.allowed_models,k.allowed_cidrs::text[],k.expires_at,k.key_prefix,k.created_at,k.last_used_at,k.budget_limited,
 CASE WHEN k.budget_limited THEN coalesce(b.balance,0) ELSE NULL END,k.cross_group_retry,(k.max_marketplace_multiplier*1000000)::bigint,coalesce(b.id,0),k.user_id,
 (coalesce((SELECT sum(l.amount) FROM v3_billing.usage_logs l WHERE l.key_id=k.id AND l.user_id=k.user_id),0)
 +coalesce((SELECT t.amount FROM v3_billing.retired_usage_totals t WHERE t.key_id=k.id AND t.user_id=k.user_id),0))::bigint`
const keyFrom = `v3_identity.api_keys k LEFT JOIN v3_billing.accounts b ON b.owner_type='api_key' AND b.owner_id=k.id AND b.kind='key_budget'`

func scanKey(row pgx.Row) (KeyRecord, error) {
	var k KeyRecord
	err := row.Scan(&k.ID, &k.Name, &k.Status, &k.Group, &k.AllowedModels, &k.AllowedCIDRs, &k.ExpiresAt, &k.Prefix, &k.CreatedAt, &k.LastUsedAt,
		&k.BudgetLimited, &k.BudgetMicroCredits, &k.CrossGroupRetry, &k.MaxMarketplaceMultiplierPPM, &k.BudgetAccountID, &k.UserID, &k.SpentMicroCredits)
	return k, controlDBError(err)
}

func (c *Control) ListKeys(ctx context.Context, uid int64, before int64, limit int) ([]KeyRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := c.pool.Query(ctx, `SELECT `+keyColumns+` FROM `+keyFrom+` WHERE k.user_id=$1 AND k.deleted_at IS NULL AND ($2::bigint=0 OR k.id<$2) ORDER BY k.id DESC LIMIT $3`, uid, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]KeyRecord, 0)
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (c *Control) UpdateKey(ctx context.Context, uid int64, in KeyInput) error {
	if in.ID <= 0 {
		return ErrInvalidInput
	}
	if err := validateKey(&in); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.api_keys WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, in.ID, uid).Scan(&id); err != nil {
			return controlDBError(err)
		}
		if err := c.validateKeyGroup(ctx, tx, uid, &in); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE v3_identity.api_keys SET name=$3,status=$4,allowed_models=$5,allowed_cidrs=$6::cidr[],expires_at=$7,
		 group_name=$8,budget_limited=$9,cross_group_retry=$10,max_marketplace_multiplier=$11::numeric/1000000 WHERE id=$1 AND user_id=$2`, in.ID, uid, in.Name, in.Status, in.AllowedModels, in.AllowedCIDRs, in.ExpiresAt, in.Group, in.BudgetLimited, in.CrossGroupRetry, in.MaxMarketplaceMultiplierPPM)
		if err != nil {
			return err
		}
		if in.BudgetLimited {
			_, err = c.setKeyBudget(ctx, tx, in.ID, in.BudgetMicroCredits)
		}
		return err
	})
}

func (c *Control) DeleteKey(ctx context.Context, uid, id int64) error {
	tag, err := c.pool.Exec(ctx, `UPDATE v3_identity.api_keys SET deleted_at=now(),status='disabled' WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, id, uid)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (c *Control) RevealKey(ctx context.Context, uid, id int64) (string, error) {
	var encrypted []byte
	err := c.pool.QueryRow(ctx, `SELECT key_ciphertext FROM v3_identity.api_keys WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, id, uid).Scan(&encrypted)
	if err != nil {
		return "", controlDBError(err)
	}
	return c.decryptKey(encrypted)
}
