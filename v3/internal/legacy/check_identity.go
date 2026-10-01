package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (m *Importer) checkUsers(ctx context.Context, target pgx.Tx, users []sourceUser, report *Report) error {
	for _, u := range users {
		role, status, group := "user", "active", u.Group
		if u.Role >= 100 {
			role = "root"
		} else if u.Role >= 10 {
			role = "admin"
		}
		if u.Status != 1 {
			status = "disabled"
		}
		if group == "" {
			group = "default"
		}
		fields := map[string]any{"id": u.ID, "username": u.Username, "display_name": u.DisplayName, "role": role, "status": status, "group_name": group, "remark": u.Remark, "settings": jsonObject(u.Setting)}
		for name, value := range map[string]string{"email": u.Email, "password_hash": u.Password, "external_id": u.ExternalID, "aff_code": u.AffCode} {
			if value == "" {
				fields[name] = nil
			} else {
				fields[name] = value
			}
		}
		fields["deleted_at"] = u.DeletedAt
		used, _ := FromV2Units(u.UsedUnits)
		history, _ := FromV2Units(u.AffHistory)
		fields["stripe_customer"], fields["affiliate_count"], fields["affiliate_history"], fields["used_credits"], fields["request_count"] = u.StripeCustomer, u.AffCount, int64(history), int64(used), u.RequestCount
		if u.InviterID > 0 {
			fields["inviter_id"] = u.InviterID
		}
		if u.CreatedAt > 0 {
			fields["created_at"] = time.Unix(u.CreatedAt, 0)
		}
		if u.LastLoginAt > 0 {
			fields["last_login_at"] = time.Unix(u.LastLoginAt, 0)
		}
		match, err := checkProjection(ctx, target, "v3_identity.users", fields)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "user", u.ID, "identity, settings or relationship differs from source")
		}
		amount, _ := ValidateWallet(u.wallet)
		if err = checkAccount(ctx, target, "user", u.ID, "wallet", int64(amount), report); err != nil {
			return err
		}
		if u.AffUnits > 0 {
			affiliate, _ := OpeningBalance(u.AffUnits)
			if err = checkAccount(ctx, target, "user", u.ID, "affiliate", int64(affiliate), report); err != nil {
				return err
			}
		}
		for provider, subject := range map[string]string{"github": u.GitHubID, "discord": u.DiscordID, "oidc": u.OIDCID, "wechat": u.WeChatID, "telegram": u.TelegramID, "linux_do": u.LinuxDOID} {
			if subject == "" {
				continue
			}
			match, err = checkProjection(ctx, target, "v3_identity.user_identities", map[string]any{"provider": provider, "subject": subject, "user_id": u.ID})
			if err != nil {
				return err
			}
			if !match {
				checkIssue(report, "user", u.ID, "identity provider binding differs from source")
			}
		}
	}
	report.Counts["check:users"] = int64(len(users))
	return nil
}

func (m *Importer) checkKeys(ctx context.Context, target pgx.Tx, rows []json.RawMessage, channelMarket *channelMarketData, report *Report) error {
	for _, row := range rows {
		k, key, cidrs, err := decodeKey(row)
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(key))
		status := "active"
		if k.Status != 1 {
			status = "disabled"
		}
		fields := map[string]any{"id": k.ID, "user_id": k.UserID, "name": k.Name, "key_hash": hash[:], "status": status, "cross_group_retry": k.CrossGroupRetry, "budget_limited": !k.Unlimited, "allowed_cidrs": cidrs, "deleted_at": k.DeletedAt}
		group := channelMarket.targetKeyGroup(k.Group, k.UserID)
		if group == "" {
			fields["group_name"] = nil
		} else {
			fields["group_name"] = group
		}
		if k.ModelsEnabled {
			models := list(k.Models)
			if models == nil {
				models = []string{}
			}
			fields["allowed_models"] = models
		} else {
			fields["allowed_models"] = nil
		}
		multiplier := k.Multiplier.String()
		if multiplier == "" {
			multiplier = "0"
		}
		fields["max_marketplace_multiplier"] = multiplier
		if k.ExpiredTime > 0 {
			fields["expires_at"] = time.Unix(k.ExpiredTime, 0)
		} else {
			fields["expires_at"] = nil
		}
		if k.CreatedTime > 0 {
			fields["created_at"] = time.Unix(k.CreatedTime, 0)
		}
		match, err := checkProjection(ctx, target, "v3_identity.api_keys", fields)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "api_key", k.ID, "key identity, owner or limits differ from source")
		}
		var ciphertext []byte
		err = target.QueryRow(ctx, `SELECT key_ciphertext FROM v3_identity.api_keys WHERE id=$1`, k.ID).Scan(&ciphertext)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil {
			plain, decryptErr := m.decrypt(ciphertext)
			if decryptErr != nil || string(plain) != key {
				checkIssue(report, "api_key", k.ID, "key ciphertext cannot reveal the original API key")
			}
		}
		if !k.Unlimited {
			amount, _ := OpeningBalance(k.RemainUnits)
			if err = checkAccount(ctx, target, "api_key", k.ID, "key_budget", int64(amount), report); err != nil {
				return fmt.Errorf("legacy: check key budget: %w", err)
			}
		}
	}
	report.Counts["check:api_keys"] = int64(len(rows))
	return nil
}

func (m *Importer) decrypt(ciphertext []byte) ([]byte, error) {
	crypto, ok := m.crypto.(catalog.Decrypter)
	if !ok {
		return nil, fmt.Errorf("legacy: check requires credential decryption")
	}
	return crypto.Decrypt(ciphertext)
}
