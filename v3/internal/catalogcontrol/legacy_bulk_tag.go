package catalogcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type legacyTagEdit struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *int64  `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	Groups         *string `json:"groups"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func legacyJSONConfig(value *string, stringValues bool) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(*value)
	if raw == "" {
		raw = "{}"
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("configuration values must be JSON objects")
	}
	if stringValues {
		var values map[string]string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("mapping values must be strings")
		}
	}
	return []byte(raw), nil
}

// legacyValidateTagEdit checks the basic field constraints on req and parses
// its group/model membership lists.
func legacyValidateTagEdit(req legacyTagEdit) (groups, models []string, errCode, errMsg string) {
	if !validLegacyTag(req.Tag) || (req.NewTag != nil && len(*req.NewTag) > 255) ||
		(req.Priority != nil && (*req.Priority < -2147483648 || *req.Priority > 2147483647)) ||
		(req.Weight != nil && (*req.Weight < 0 || *req.Weight > 2147483647)) {
		return nil, nil, "invalid_tag_update", "Invalid tag, priority or weight"
	}
	groups, err := validateLegacyMemberships(req.Groups)
	if err != nil {
		return nil, nil, "invalid_groups", err.Error()
	}
	models, err = validateLegacyMemberships(req.Models)
	if err != nil {
		return nil, nil, "invalid_models", err.Error()
	}
	return groups, models, "", ""
}

// legacyBuildTagAssignments builds the SQL column assignments and
// positional args (args[0] is reserved for the tag/id filter) for the fields
// present on req.
func legacyBuildTagAssignments(req legacyTagEdit) (assignments []string, args []any, errCode, errMsg string) {
	assignments = make([]string, 0, 6)
	args = []any{req.Tag}
	add := func(column string, value any) {
		args = append(args, value)
		assignments = append(assignments, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	for _, item := range []struct {
		column       string
		value        *string
		stringValues bool
	}{{"model_mapping", req.ModelMapping, true}, {"param_override", req.ParamOverride, false}, {"header_override", req.HeaderOverride, true}} {
		if item.value == nil {
			continue
		}
		raw, e := legacyJSONConfig(item.value, item.stringValues)
		if e != nil {
			return nil, nil, "invalid_configuration", e.Error()
		}
		add(item.column, json.RawMessage(raw))
	}
	if req.NewTag != nil {
		add("tag", *req.NewTag)
	}
	if req.Priority != nil {
		add("priority", *req.Priority)
	}
	if req.Weight != nil {
		add("weight", *req.Weight)
	}
	return assignments, args, "", ""
}

// legacyLockTagChannelIDs locks and returns the ids of channels currently
// carrying tag, in id order, within tx.
func legacyLockTagChannelIDs(ctx context.Context, tx pgx.Tx, tag string) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM v3_catalog.channels WHERE tag=$1 ORDER BY id FOR UPDATE`, tag)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	return ids, err
}

// legacyApplyTagMemberships replaces channel_groups/channel_models membership
// rows for ids with groups/models, when non-nil. err is the error already
// accumulated by the caller; it is honored and returned unchanged if non-nil
// on entry, matching the original inline loop's short-circuit behavior.
func legacyApplyTagMemberships(ctx context.Context, tx pgx.Tx, ids []int64, groups, models []string, err error) error {
	for _, membership := range []struct {
		table, column string
		values        []string
	}{{"channel_groups", "group_name", groups}, {"channel_models", "model", models}} {
		if err != nil || membership.values == nil {
			continue
		}
		_, err = tx.Exec(ctx, `DELETE FROM v3_catalog.`+membership.table+` WHERE channel_id=ANY($1::bigint[])`, ids)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO v3_catalog.`+membership.table+`(channel_id,`+membership.column+`) SELECT i,v FROM unnest($1::bigint[]) i CROSS JOIN unnest($2::text[]) v ON CONFLICT DO NOTHING`, ids, membership.values)
		}
	}
	return err
}

func (s *Server) legacyEditTagChannels(w http.ResponseWriter, r *http.Request) {
	var req legacyTagEdit
	if !decode(w, r, &req) {
		return
	}
	groups, models, errCode, errMsg := legacyValidateTagEdit(req)
	if errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	assignments, args, errCode, errMsg := legacyBuildTagAssignments(req)
	if errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// Lock and capture exactly the original tag members, including when the tag
	// is renamed to a tag that already belongs to other channels.
	ids, err := legacyLockTagChannelIDs(r.Context(), tx, req.Tag)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if len(assignments) > 0 {
		args[0] = ids
		_, err = tx.Exec(r.Context(), `UPDATE v3_catalog.channels SET `+strings.Join(assignments, ",")+` WHERE id=ANY($1::bigint[])`, args...)
	}
	err = legacyApplyTagMemberships(r.Context(), tx, ids, groups, models, err)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}
