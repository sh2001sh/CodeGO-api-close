package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Restored credentials are never stored in reports or error messages.
type restoredRecord struct {
	table, key, secretColumn, secret string
	id                               int64
	fields                           map[string]any
}

type restoredState struct {
	records []restoredRecord
	counts  map[string]int64
	issues  []Issue
}

func loadRestoredState(ctx context.Context, source pgx.Tx, sources map[string]string, users []sourceUser, catalog *catalogData) (*restoredState, error) {
	d := &restoredState{counts: map[string]int64{}}
	userIDs := map[int64]bool{}
	for _, u := range users {
		userIDs[u.ID] = true
	}
	devices := map[int64]int64{}
	factors := map[int64]bool{}
	for _, name := range []string{"two_fas", "two_fa_backup_codes", "desktop_authorized_devices", "desktop_auth_sessions"} {
		rows, err := loadRows(ctx, source, sources[name])
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			var row commerceRow
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, fmt.Errorf("legacy: invalid restored source row")
			}
			if deleted := row["deleted_at"]; len(deleted) > 0 && string(deleted) != "null" {
				if _, deletionErr := catalogDataDeleted(deleted); deletionErr != nil {
					id, _ := row.integer("id")
					d.issues = append(d.issues, Issue{name, id, "invalid_restored_state", "invalid source deletion timestamp"})
					continue
				}
				d.counts[name+".soft_deleted_excluded"]++
				continue
			}
			var record restoredRecord
			switch name {
			case "two_fas":
				record, err = projectRestoredFactor(raw)
			case "two_fa_backup_codes":
				record, err = projectRestoredBackup(raw)
			case "desktop_authorized_devices":
				record, err = projectRestoredDevice(raw)
			case "desktop_auth_sessions":
				record, err = projectRestoredSession(raw)
			}
			if err == nil {
				uid, _ := record.fields["user_id"].(int64)
				if uid > 0 && !userIDs[uid] {
					err = fmt.Errorf("restored state references absent user")
				}
				if name == "two_fa_backup_codes" && !factors[uid] {
					err = fmt.Errorf("backup code references absent active two-factor state")
				}
				if name == "desktop_auth_sessions" {
					if did, ok := record.fields["device_id"].(int64); ok && (devices[did] == 0 || devices[did] != uid) {
						err = fmt.Errorf("authorization device owner differs from session")
					}
				}
				if name == "two_fas" {
					if factors[uid] {
						err = fmt.Errorf("duplicate active two-factor state")
					}
					factors[uid] = true
				}
				if name == "desktop_authorized_devices" {
					devices[record.id] = uid
				}
			}
			if err != nil {
				d.issues = append(d.issues, Issue{name, record.id, "invalid_restored_state", err.Error()})
				continue
			}
			d.records = append(d.records, record)
			d.counts[name]++
		}
	}
	activeModels := map[int64]bool{}
	for _, row := range catalog.rows["models"] {
		id, _ := row.integer("id")
		deleted := row["deleted_at"]
		if len(deleted) == 0 || string(deleted) == "null" {
			activeModels[id] = true
		}
	}
	for _, u := range users {
		if u.Setting == "" {
			continue
		}
		var settings struct {
			FavoriteModelIDs []int64 `json:"favorite_model_ids"`
		}
		if json.Unmarshal([]byte(u.Setting), &settings) != nil {
			d.issues = append(d.issues, Issue{"favorites", u.ID, "invalid_restored_state", "favorite model IDs must be integers"})
			continue
		}
		seen := map[int64]bool{}
		for _, id := range settings.FavoriteModelIDs {
			if id <= 0 || !activeModels[id] {
				d.issues = append(d.issues, Issue{"favorites", u.ID, "invalid_restored_state", "favorite references absent or deleted model"})
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			d.records = append(d.records, restoredRecord{table: "v3_adminops.model_favorites", key: "user_id,model_id", id: u.ID, fields: map[string]any{"user_id": u.ID, "model_id": id}})
			d.counts["model_favorites"]++
		}
	}
	return d, nil
}

func (d *restoredState) validate(r *Report) {
	r.Issues = append(r.Issues, d.issues...)
	for name, count := range d.counts {
		r.Counts["restored."+name] = count
	}
}

func (m *Importer) validateRestoredSecrets(data *importData, r *Report) {
	for _, record := range data.restored.records {
		if record.secretColumn == "" {
			continue
		}
		plain, err := m.sourceSecret(record.secret)
		if err == nil && record.secretColumn == "secret_ciphertext" {
			err = validateRestoredTOTP(plain)
		}
		if err != nil {
			r.Issues = append(r.Issues, Issue{record.table, record.id, "invalid_source_secret", err.Error()})
		}
	}
	for _, value := range data.options {
		if _, err := m.sourceOption(value); err != nil {
			r.Issues = append(r.Issues, Issue{"setting", 0, "invalid_source_secret", "encrypted source setting requires valid source crypto secret"})
		}
	}
}
