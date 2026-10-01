package channelmarket

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Update(ctx context.Context, a Actor, channel int64, patch json.RawMessage) (ChannelView, error) {
	var result ChannelView
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil || fields == nil {
		return result, ErrInvalid
	}
	if err := validateUpdateFields(a, fields); err != nil {
		return result, err
	}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		id, e := owned(ctx, tx, a, channel)
		if e != nil {
			return e
		}
		if e = applyModelConsistencyStatusTx(ctx, tx, a, channel, fields); e != nil {
			return e
		}
		r, previousSource, e := loadCurrentChannelRequestTx(ctx, tx, channel)
		if e != nil {
			return e
		}
		r, factor, e := mergeAndValidateUpdate(r, fields)
		if e != nil {
			return e
		}
		sourceChanged := r.SourceLabel != previousSource
		configChanged := updateChangesConfig(fields, sourceChanged)
		if e = s.persistChannelUpdateTx(ctx, tx, channel, r, fields, sourceChanged, configChanged); e != nil {
			return e
		}
		if e = persistUpdateModelsAndGroupTx(ctx, tx, id, channel, r, factor, configChanged); e != nil {
			return e
		}
		result, e = scanChannel(tx.QueryRow(ctx, `SELECT `+channelColumns+channelFrom+` WHERE g.id=$1`, id))
		return e
	})
	return result, err
}

// updatableFields lists the patch keys any owner may set via Update.
var updatableFields = []string{"name", "provider_type", "source_label", "base_url", "api_key", "declared_models", "model_prices", "multiplier", "visibility", "max_concurrency", "user_max_concurrency", "qps", "maintenance_window", "sensitive_word_interception_enabled", "multiplier_card_supported", "multiplier_card_user_enabled", "auto_probe_enabled", "auto_probe_interval_minutes", "auto_probe_model"}

// validateUpdateFields rejects any patch key outside updatableFields, except
// model_consistency_status which is admin-only.
func validateUpdateFields(a Actor, fields map[string]json.RawMessage) error {
	allowed := map[string]bool{}
	for _, key := range updatableFields {
		allowed[key] = true
	}
	for key := range fields {
		if key == "model_consistency_status" && a.Admin {
			allowed[key] = true
		}
		if !allowed[key] {
			return ErrInvalid
		}
	}
	return nil
}

// applyModelConsistencyStatusTx handles the admin-only model_consistency_status
// field: validates it, records it on the channel, logs a security event, and
// removes it from fields so it isn't merged into the CreateRequest patch.
func applyModelConsistencyStatusTx(ctx context.Context, tx pgx.Tx, a Actor, channel int64, fields map[string]json.RawMessage) error {
	raw, ok := fields["model_consistency_status"]
	if !ok {
		return nil
	}
	var status string
	if json.Unmarshal(raw, &status) != nil || (status != "" && status != "passed" && status != "failed" && status != "questionable") {
		return ErrInvalid
	}
	if _, e := tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('model_consistency_status',$2::text)) WHERE id=$1`, channel, status); e != nil {
		return e
	}
	if e := securityTx(ctx, tx, a, channel, "model_consistency_review", map[string]any{"status": status}); e != nil {
		return e
	}
	delete(fields, "model_consistency_status")
	return nil
}

// loadCurrentChannelRequestTx loads the channel's current config as a
// CreateRequest (the merge base for the patch) along with its previous
// source label.
func loadCurrentChannelRequestTx(ctx context.Context, tx pgx.Tx, channel int64) (CreateRequest, string, error) {
	var r CreateRequest
	var factor int64
	e := tx.QueryRow(ctx, `SELECT c.name,c.provider,c.base_url,c.max_concurrency,c.max_user_concurrency,c.multiplier_card_supported,g.source_label,g.visibility,g.multiplier_ppm,g.model_prices,ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model) FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=$1`, channel).Scan(&r.Name, &r.Provider, &r.BaseURL, &r.MaxConcurrency, &r.UserMaxConcurrency, &r.MultiplierCardSupported, &r.SourceLabel, &r.Visibility, &factor, &r.Prices, &r.Models)
	if e != nil {
		return r, "", e
	}
	r.Multiplier = json.Number(formatFactor(factor))
	r.APIKey = "unchanged"
	r.MultiplierCardUserEnabled = r.MultiplierCardSupported
	var current json.RawMessage
	if e = tx.QueryRow(ctx, `SELECT coalesce(settings->'market','{}') FROM v3_catalog.channels WHERE id=$1`, channel).Scan(&current); e != nil {
		return r, "", e
	}
	if e = json.Unmarshal(current, &r); e != nil {
		return r, "", e
	}
	return r, r.SourceLabel, nil
}

// mergeAndValidateUpdate overlays fields onto the JSON form of r, re-decodes
// it, and validates the merged request, returning the resolved multiplier factor.
func mergeAndValidateUpdate(r CreateRequest, fields map[string]json.RawMessage) (CreateRequest, int64, error) {
	original, e := json.Marshal(r)
	if e != nil {
		return r, 0, e
	}
	var merged map[string]json.RawMessage
	if e = json.Unmarshal(original, &merged); e != nil {
		return r, 0, e
	}
	for key, value := range fields {
		merged[key] = value
	}
	payload, e := json.Marshal(merged)
	if e != nil {
		return r, 0, e
	}
	if e = json.Unmarshal(payload, &r); e != nil {
		return r, 0, ErrInvalid
	}
	factor, e := r.validate()
	if e != nil {
		return r, 0, e
	}
	return r, factor, nil
}

// updateChangesConfig reports whether the patch touches any field that
// requires re-verification (source label, provider config, credentials, or
// declared models), in addition to a label change detected separately.
func updateChangesConfig(fields map[string]json.RawMessage, sourceChanged bool) bool {
	configChanged := sourceChanged
	for _, key := range []string{"provider_type", "base_url", "api_key", "declared_models"} {
		if _, ok := fields[key]; ok {
			configChanged = true
		}
	}
	return configChanged
}

// persistChannelUpdateTx writes the merged request back onto the channel row
// (and credentials, if the api_key field was supplied).
func (s *Service) persistChannelUpdateTx(ctx context.Context, tx pgx.Tx, channel int64, r CreateRequest, fields map[string]json.RawMessage, sourceChanged, configChanged bool) error {
	settings, e := safeSettings(r)
	if e != nil {
		return e
	}
	if sourceChanged {
		if _, e = tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('submitted_source_label',$2::text,'source_label_status','pending','source_label_review_reason','')) WHERE id=$1`, channel, r.SourceLabel); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE v3_catalog.channels SET name=$2,provider=$3,base_url=$4,max_concurrency=$5,max_user_concurrency=$6,multiplier_card_supported=$7,status=CASE WHEN $8 THEN 'disabled' ELSE status END,settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||$9::jsonb) WHERE id=$1`, channel, r.Name, nativeProvider(r.Provider), r.BaseURL, r.MaxConcurrency, r.UserMaxConcurrency, r.MultiplierCardSupported, configChanged, settings); e != nil {
		return e
	}
	if _, keyChanged := fields["api_key"]; keyChanged {
		if s.enc == nil {
			return ErrUnavailable
		}
		secret, e := s.enc.Encrypt([]byte(r.APIKey))
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM v3_catalog.channel_credentials WHERE channel_id=$1`, channel); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES($1,$2)`, channel, secret); e != nil {
			return e
		}
	}
	return nil
}

// persistUpdateModelsAndGroupTx replaces the channel's declared models and
// updates the market group and its backing catalog group to match r.
func persistUpdateModelsAndGroupTx(ctx context.Context, tx pgx.Tx, id string, channel int64, r CreateRequest, factor int64, configChanged bool) error {
	if _, e := tx.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=$1`, channel); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) SELECT $1,unnest($2::text[])`, channel, r.Models); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET display_name=$2,source_label=$3,visibility=$4,multiplier_ppm=$5,model_prices=$6,lifecycle_status=CASE WHEN $7 THEN 'draft' ELSE lifecycle_status END,verification_status=CASE WHEN $7 THEN 'pending' ELSE verification_status END WHERE id=$1`, id, r.Name, r.SourceLabel, r.Visibility, factor, r.Prices, configChanged); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `UPDATE v3_catalog.groups SET multiplier=$2::numeric/1000000 WHERE name=(SELECT internal_group_name FROM v3_channelmarket.groups WHERE id=$1)`, id, factor); e != nil {
		return e
	}
	return syncCommunity(ctx, tx, channel)
}

func syncCommunity(ctx context.Context, tx pgx.Tx, channel int64) error {
	_, err := tx.Exec(ctx, `UPDATE v3_catalog.channels c SET settings=jsonb_set(c.settings,'{community}',jsonb_build_object('id',g.public_channel_id,'slug',g.public_slug,'name',g.display_name,'visibility',g.visibility,'lifecycle_status',g.lifecycle_status,'verification_status',g.verification_status)) FROM v3_channelmarket.groups g WHERE g.channel_id=c.id AND c.id=$1`, channel)
	return err
}

func (s *Service) Feedback(ctx context.Context, user int64, group string) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if e := accessible(ctx, tx, user, group); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.channel_feedback(channel_id,user_id) SELECT channel_id,$2 FROM v3_channelmarket.groups WHERE id=$1`, group, user)
		return e
	})
}

func (s *Service) RemoveFailedModel(ctx context.Context, a Actor, channel int64, model string) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, channel); e != nil {
			return e
		}
		var failed bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.verification_runs v, jsonb_array_elements(v.results) r WHERE v.channel_id=$1 AND r->>'model'=$2 AND r->>'status'='failed')`, channel, model).Scan(&failed)
		if e != nil {
			return e
		}
		if !failed {
			return ErrConflict
		}
		tag, e := tx.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=$1 AND model=$2`, channel, model)
		if e == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return e
	})
}
