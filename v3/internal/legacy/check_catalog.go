package legacy

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) checkChannels(ctx context.Context, target pgx.Tx, rows []json.RawMessage, report *Report) error {
	for _, row := range rows {
		c, provider, err := decodeChannel(row)
		if err != nil {
			return err
		}
		status := "enabled"
		if c.Status == 2 {
			status = "disabled"
		} else if c.Status != 1 {
			status = "auto_disabled"
		}
		fields := map[string]any{"id": c.ID, "name": c.Name, "provider": provider, "base_url": c.BaseURL, "status": status, "priority": c.Priority, "weight": c.Weight, "model_mapping": jsonObject(c.ModelMapping), "param_override": jsonObject(c.ParamOverride), "header_override": jsonObject(c.HeaderOverride), "status_code_mapping": jsonObject(c.StatusMapping), "remark": c.Remark, "auto_disable": c.AutoBan == nil || *c.AutoBan != 0}
		fields["proxy_url"] = c.ProxyURL
		match, err := checkProjection(ctx, target, "v3_catalog.channels", fields)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "channel", c.ID, "channel routing or provider differs from source")
		}
		var settingsMatch bool
		if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.channels WHERE id=$1 AND settings @> $2::jsonb)`, c.ID, jsonObject(c.Settings)).Scan(&settingsMatch); err != nil {
			return err
		}
		if !settingsMatch {
			checkIssue(report, "channel", c.ID, "channel settings differ from source")
		}
		groups := list(c.Group)
		if len(groups) == 0 {
			groups = []string{"default"}
		}
		for _, group := range groups {
			match, err = checkProjection(ctx, target, "v3_catalog.channel_groups", map[string]any{"channel_id": c.ID, "group_name": group})
			if err != nil {
				return err
			}
			if !match {
				checkIssue(report, "channel", c.ID, "channel group association is missing")
			}
		}
		for _, model := range list(c.Models) {
			match, err = checkProjection(ctx, target, "v3_catalog.channel_models", map[string]any{"channel_id": c.ID, "model": model})
			if err != nil {
				return err
			}
			if !match {
				checkIssue(report, "channel", c.ID, "channel model association is missing")
			}
		}
		if err = m.checkCredentials(ctx, target, c, report); err != nil {
			return err
		}
	}
	report.Counts["check:channels"] = int64(len(rows))
	return nil
}

func (m *Importer) checkCredentials(ctx context.Context, target pgx.Tx, c sourceChannel, report *Report) error {
	rows, err := target.Query(ctx, `SELECT secret,kind,status,expires_at FROM v3_catalog.channel_credentials WHERE channel_id=$1 ORDER BY id`, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	secrets := splitSecrets(c.Key)
	index := 0
	for rows.Next() {
		var ciphertext []byte
		var kind, status string
		var expiry any
		values, err := rows.Values()
		if err != nil {
			return err
		}
		ciphertext, _ = values[0].([]byte)
		kind, _ = values[1].(string)
		status, _ = values[2].(string)
		expiry = values[3]
		if index >= len(secrets) {
			checkIssue(report, "channel", c.ID, "unexpected additional credential")
			index++
			continue
		}
		plain, decryptErr := m.decrypt(ciphertext)
		expectedKind, expectedStatus, expectedExpiry, err := credentialProperties(secrets[index], c.ChannelInfo, index)
		if err != nil {
			return err
		}
		actualExpiry, _ := json.Marshal(expiry)
		wantedExpiry, _ := json.Marshal(expectedExpiry)
		if decryptErr != nil || string(plain) != secrets[index] || kind != expectedKind || status != expectedStatus || string(actualExpiry) != string(wantedExpiry) {
			checkIssue(report, "channel", c.ID, "credential decryption, lifecycle or expiry differs from source")
		}
		index++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if index != len(secrets) {
		checkIssue(report, "channel", c.ID, "credential count differs from source")
	}
	return nil
}
