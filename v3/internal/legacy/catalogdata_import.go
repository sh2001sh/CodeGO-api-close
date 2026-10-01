package legacy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importCatalogData(ctx context.Context, target pgx.Tx, data *catalogData) error {
	for _, name := range catalogDataSourceNames {
		for _, row := range data.rows[name] {
			record, err := data.project(name, row)
			if err != nil {
				return err
			}
			if name == "route_pools" {
				if _, err = target.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES($1) ON CONFLICT(name) DO NOTHING`, record.fields["group_name"]); err != nil {
					return err
				}
			}
			if err = insertCatalogData(ctx, target, record); err != nil {
				return err
			}
		}
		if len(data.rows[name]) > 0 && name != "route_pool_members" {
			table := pgx.Identifier{"v3_catalog", name}.Sanitize()
			if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),
				GREATEST(COALESCE((SELECT max(id) FROM `+table+`),0),1),EXISTS(SELECT 1 FROM `+table+`))`, "v3_catalog."+name); err != nil {
				return fmt.Errorf("legacy: reset catalog %s identity: %w", name, err)
			}
		}
	}
	return nil
}

func insertCatalogData(ctx context.Context, target pgx.Tx, record catalogDataRecord) error {
	columns := make([]string, 0, len(record.fields))
	for column := range record.fields {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	quoted, placeholders, args := make([]string, len(columns)), make([]string, len(columns)), make([]any, len(columns))
	for i, column := range columns {
		quoted[i], placeholders[i], args[i] = pgx.Identifier{column}.Sanitize(), fmt.Sprintf("$%d", i+1), record.fields[column]
	}
	table := pgx.Identifier{"v3_catalog", record.table}.Sanitize()
	query := "INSERT INTO " + table + "(" + strings.Join(quoted, ",") + ") VALUES(" + strings.Join(placeholders, ",") + ") ON CONFLICT(" + pgx.Identifier{record.key}.Sanitize() + ") DO NOTHING"
	if _, err := target.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("legacy: insert catalog %s ID %d: %w", record.table, record.id, err)
	}
	equal, err := checkProjection(ctx, target, "v3_catalog."+record.table, record.fields)
	if err != nil {
		return err
	}
	if !equal {
		return fmt.Errorf("legacy: catalog %s ID %d conflicts with existing target values", record.table, record.id)
	}
	return nil
}

func (m *Importer) checkCatalogData(ctx context.Context, target pgx.Tx, data *catalogData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for _, name := range catalogDataSourceNames {
		var matched int64
		for _, row := range data.rows[name] {
			record, err := data.project(name, row)
			if err != nil {
				return err
			}
			equal, err := checkProjection(ctx, target, "v3_catalog."+record.table, record.fields)
			if err != nil {
				return err
			}
			if equal {
				matched++
			} else {
				checkIssue(report, name, record.id, "catalog typed values or references differ from source")
			}
		}
		report.Counts["check:catalog:"+name+":expected"] = int64(len(data.rows[name]))
		report.Counts["check:catalog:"+name+":matched"] = matched
		if len(data.rows[name]) > 0 {
			var total int64
			if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"v3_catalog", name}.Sanitize()).Scan(&total); err != nil {
				return err
			}
			report.Counts["check:catalog:"+name+":target"] = total
		}
	}
	return nil
}
