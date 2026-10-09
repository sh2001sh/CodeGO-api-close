package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func loadOnlineProjector(ctx context.Context, source, target pgx.Tx, sources map[string]string) (*onlineProjector, error) {
	funding, err := loadFundingDependencies(ctx, source, sources)
	if err != nil {
		return nil, err
	}
	market, err := loadChannelMarketBase(ctx, source, sources, false)
	if err != nil {
		return nil, err
	}
	if len(market.issues) > 0 {
		return nil, fmt.Errorf("legacy: online marketplace dependencies invalid: %s", market.issues[0].Detail)
	}
	p := &onlineProjector{funding: funding, market: market, target: target, users: map[int64]bool{}, history: &historyData{accounts: map[string]historyAccount{}, retiredAccounts: map[string]bool{}}}
	for id := range funding.users {
		p.users[id] = true
	}
	dependencies, err := json.Marshal(p.onlineDependencyValues())
	if err != nil {
		return nil, err
	}
	var previous []byte
	if err = target.QueryRow(ctx, "SELECT dependencies FROM v3_migration_online.run WHERE singleton").Scan(&previous); err != nil {
		return nil, err
	}
	var old, next map[string]json.RawMessage
	if err = json.Unmarshal(previous, &old); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(dependencies, &next); err != nil {
		return nil, err
	}
	for key, raw := range old {
		var before, after bytes.Buffer
		if err = json.Compact(&before, raw); err != nil {
			return nil, err
		}
		value, exists := next[key]
		if !exists {
			return nil, errors.New("legacy: online structural dependency was deleted; rebuild the baseline")
		}
		if err = json.Compact(&after, value); err != nil {
			return nil, err
		}
		if !bytes.Equal(before.Bytes(), after.Bytes()) {
			return nil, errors.New("legacy: online structural dependency changed; rebuild the baseline")
		}
	}
	if _, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET dependencies=$1 WHERE singleton", dependencies); err != nil {
		return nil, err
	}
	staged := onlineStageTx{Tx: target}
	p.mappings, err = loadFundingAccountIndex(ctx, staged)
	if err != nil {
		return nil, err
	}
	reservedNew := false
	reserve := func(owner string, id int64, kind string) error {
		key := fundingAccountKey{owner, kind, id}
		if _, exists := p.mappings[key]; exists {
			return nil
		}
		_, err := target.Exec(ctx, `INSERT INTO v3_migration_online.account_ids(id,owner_type,owner_id,kind)
		 SELECT nextval(pg_get_serial_sequence('v3_billing.accounts','id')),$1,$2,$3
		 WHERE NOT EXISTS(SELECT 1 FROM v3_migration_online.account_ids WHERE owner_type=$1 AND owner_id=$2 AND kind=$3)`, owner, id, kind)
		if err == nil {
			// Mark this key until the actual IDs are reloaded below. User wallets
			// also appear among historical accounts; reserve each only once.
			p.mappings[key] = 0
			reservedNew = true
		}
		return err
	}
	for id := range p.users {
		if err = reserve("user", id, "wallet"); err != nil {
			return nil, err
		}
	}
	for id, account := range funding.accounts {
		if retiredAccountKind(account.Kind) {
			p.history.retiredAccounts[id] = true
			continue
		}
		if account.ID == "" || account.Unit != "quota" || account.OwnerType == "" || account.Kind == "" {
			return nil, errors.New("legacy: invalid online historical account")
		}
		p.history.accounts[id] = account
		owner, kind := historicalAccountMapping(account)
		if kind != "" {
			if err = reserve(owner, account.OwnerID, kind); err != nil {
				return nil, err
			}
		}
	}
	if reservedNew {
		p.mappings, err = loadFundingAccountIndex(ctx, staged)
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *onlineProjector) historicalAccounts() ([]onlineProjection, error) {
	var out []onlineProjection
	for _, a := range p.history.accounts {
		owner, kind := historicalAccountMapping(a)
		var mapped *int64
		if id, exists := p.mappings[fundingAccountKey{owner, kind, a.OwnerID}]; exists {
			mapped = &id
		}
		values := map[string]any{"source_account_id": a.ID, "account_id": mapped, "owner_type": a.OwnerType, "owner_id": a.OwnerID, "account_type": a.Kind, "unit": a.Unit, "status": a.Status, "version": a.Version, "metadata": historyMetadata(a.Metadata), "created_at": historyDate(a.CreatedAt), "updated_at": historyDate(a.UpdatedAt)}
		out = append(out, onlineProjection{"v3_billing", "historical_accounts", []string{"source_account_id"}, values})
	}
	return out, nil
}

func (p *onlineProjector) project(ctx context.Context, spec onlineSpec, raw json.RawMessage) ([]onlineProjection, error) {
	if strings.HasPrefix(spec.name, "market.") {
		return p.projectOnlineMarket(strings.TrimPrefix(spec.name, "market."), raw)
	}
	if spec.name == "funding_lots" || spec.name == "funding_allocations" {
		return p.projectOnlineFunding(spec.name, raw)
	}
	var fields map[string]any
	var err error
	switch spec.name {
	case "ledger_entries":
		if p.history.retiredHistoryEntry(raw) {
			return nil, nil
		}
		e, decodeErr := decodeHistoryEntry(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if _, exists := p.history.accounts[e.AccountID]; !exists {
			return nil, errors.New("legacy: online historical ledger references missing account")
		}
		fields, err = onlineProjectionFields(e, map[string]string{"account_id": "source_account_id"}, nil, map[string]historyTime{"created_at": e.CreatedAt})
		return []onlineProjection{{"v3_billing", "historical_entries", []string{"entry_id"}, fields}}, err
	case "logs":
		l, decodeErr := decodeHistoryLog(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		fields, err = onlineProjectionFields(l, map[string]string{"type": "event_type", "model_name": "model", "quota": "amount", "use_time": "duration_seconds", "token_id": "key_id", "group": "group_name"}, []string{"other", "Metadata", "CachedTokens"}, map[string]historyTime{"created_at": l.CreatedAt})
		if err != nil {
			return nil, err
		}
		fields["metadata"] = l.Metadata
		out := []onlineProjection{{"v3_audit", "events", []string{"id"}, fields}}
		if l.Type != 2 {
			return out, nil
		}
		if !p.users[l.UserID] {
			return nil, errors.New("legacy: online usage log references missing user")
		}
		account, exists := p.mappings[fundingAccountKey{"user", "wallet", l.UserID}]
		if !exists {
			return nil, errors.New("legacy: online usage requires reserved wallet identity")
		}
		// Use unique qualified IDs while the baseline is incomplete. The whole
		// group is normalized after copying and after every affected delta.
		usage := map[string]any{"id": l.ID, "created_at": historyDate(l.CreatedAt), "account_id": account, "user_id": l.UserID, "key_id": l.KeyID, "channel_id": l.ChannelID, "amount": l.Amount, "prompt_tokens": l.PromptTokens, "completion_tokens": l.CompletionTokens, "cached_tokens": l.CachedTokens, "request_id": historyUsageRequestID(l, true), "model": l.Model, "terminal": "completed"}
		return append(out, onlineProjection{"v3_billing", "usage_logs", []string{"created_at", "id"}, usage}), nil
	case "request_audits":
		a, decodeErr := decodeHistoryRequestAudit(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		fields, err = onlineProjectionFields(a, map[string]string{"token_id": "key_id", "model_name": "model", "quota": "amount"}, nil, map[string]historyTime{"created_at": a.CreatedAt, "updated_at": a.UpdatedAt, "started_at": a.StartedAt, "completed_at": a.CompletedAt})
		if err != nil {
			return nil, err
		}
		fields["completed_at"] = a.completedDate()
		return []onlineProjection{{"v3_audit", "request_audits", []string{"request_id"}, fields}}, nil
	case "request_attempt_audits":
		a, decodeErr := decodeHistoryAttemptAudit(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		var parent bool
		if err = p.target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+onlineStage("v3_audit.request_audits")+" WHERE request_id=$1)", a.RequestID).Scan(&parent); err != nil {
			return nil, err
		}
		if !parent {
			return []onlineProjection{{"v3_audit", "orphan_request_attempt_history", []string{"attempt_id"}, map[string]any{"attempt_id": a.AttemptID, "request_id": a.RequestID, "source_record": raw}}}, nil
		}
		fields, err = onlineProjectionFields(a, map[string]string{"model_name": "model"}, nil, map[string]historyTime{"created_at": a.CreatedAt, "started_at": a.StartedAt, "completed_at": a.CompletedAt})
		return []onlineProjection{{"v3_audit", "request_attempt_audits", []string{"attempt_id"}, fields}}, err
	}
	return nil, errors.New("legacy: unsupported online projection")
}
