package legacy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importRestoredState(ctx context.Context, target pgx.Tx, d *restoredState) error {
	for _, r := range d.records {
		fields := map[string]any{}
		for name, value := range r.fields {
			fields[name] = value
		}
		if r.secretColumn != "" {
			plain, err := m.sourceSecret(r.secret)
			if err != nil {
				return err
			}
			sealed, err := m.crypto.Encrypt([]byte(plain))
			if err != nil {
				return err
			}
			fields[r.secretColumn] = sealed
		}
		columns := make([]string, 0, len(fields))
		for name := range fields {
			columns = append(columns, name)
		}
		sort.Strings(columns)
		quoted, holders, args := make([]string, len(columns)), make([]string, len(columns)), make([]any, len(columns))
		for i, name := range columns {
			quoted[i] = pgx.Identifier{name}.Sanitize()
			holders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = fields[name]
		}
		keys := strings.Split(r.key, ",")
		for i, key := range keys {
			keys[i] = pgx.Identifier{key}.Sanitize()
		}
		query := "INSERT INTO " + pgx.Identifier(strings.Split(r.table, ".")).Sanitize() + " (" + strings.Join(quoted, ",") + ") VALUES (" + strings.Join(holders, ",") + ") ON CONFLICT (" + strings.Join(keys, ",") + ") DO NOTHING"
		if _, err := target.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("legacy: restored state insert %s: %w", r.table, err)
		}
		match, err := m.restoredRecordMatches(ctx, target, r)
		if err != nil {
			return err
		}
		if !match {
			return fmt.Errorf("legacy: restored %s conflicts with target", r.table)
		}
	}
	for _, table := range []string{"v3_identity.two_factor_backup_codes", "v3_identity.desktop_devices"} {
		if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),GREATEST(COALESCE((SELECT max(id) FROM `+table+`),0),1),EXISTS(SELECT 1 FROM `+table+`))`, table); err != nil {
			return err
		}
	}
	return nil
}

func (m *Importer) restoredRecordMatches(ctx context.Context, target pgx.Tx, r restoredRecord) (bool, error) {
	match, err := checkProjection(ctx, target, r.table, r.fields)
	if err != nil || !match || r.secretColumn == "" {
		return match, err
	}
	var sealed []byte
	query := "SELECT " + pgx.Identifier{r.secretColumn}.Sanitize() + " FROM " + pgx.Identifier(strings.Split(r.table, ".")).Sanitize() + " WHERE " + pgx.Identifier{r.key}.Sanitize() + "=$1"
	if err = target.QueryRow(ctx, query, r.fields[r.key]).Scan(&sealed); err != nil {
		return false, err
	}
	plain, err := m.decrypt(sealed)
	if err != nil {
		return false, nil
	}
	source, err := m.sourceSecret(r.secret)
	if err != nil {
		return false, err
	}
	return string(plain) == source, nil
}

func (m *Importer) checkRestoredState(ctx context.Context, target pgx.Tx, d *restoredState, r *Report) error {
	expected := map[string]int64{"v3_identity.two_factor": 0, "v3_identity.two_factor_backup_codes": 0, "v3_identity.desktop_devices": 0, "v3_identity.desktop_auth_sessions": 0, "v3_adminops.model_favorites": 0}
	for _, record := range d.records {
		expected[record.table]++
		match, err := m.restoredRecordMatches(ctx, target, record)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(r, record.table, record.id, "restored typed fields or protected credential differ from source")
		}
		r.Counts["check:restored_state"]++
	}
	for table, want := range expected {
		var count int64
		if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier(strings.Split(table, ".")).Sanitize()).Scan(&count); err != nil {
			return err
		}
		r.Counts["check:restored:"+table+":actual"] = count
		if count != want {
			checkIssue(r, table, 0, "restored source and target record counts differ")
		}
	}
	return nil
}
