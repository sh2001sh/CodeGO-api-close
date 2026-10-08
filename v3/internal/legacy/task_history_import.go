package legacy

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importTaskHistory(ctx context.Context, target pgx.Tx, data *taskHistoryData) error {
	for _, row := range data.records {
		var collision bool
		if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_workflow.tasks WHERE id=$1)`, row.fields["id"]).Scan(&collision); err != nil {
			return err
		}
		if collision {
			return fmt.Errorf("legacy: historical task conflicts with a native task")
		}
		columns := make([]string, 0, len(row.fields))
		for key := range row.fields {
			columns = append(columns, key)
		}
		sort.Strings(columns)
		values := make([]any, len(columns))
		for i, key := range columns {
			values[i] = row.fields[key]
		}
		if err := insertHistoryExact(ctx, target, "v3_workflow", "legacy_tasks", "id", columns, values); err != nil {
			return err
		}
	}
	return nil
}

func (m *Importer) checkTaskHistory(ctx context.Context, target pgx.Tx, data *taskHistoryData, report *Report) error {
	for _, row := range data.records {
		equal, err := checkProjection(ctx, target, "v3_workflow.legacy_tasks", row.fields)
		if err != nil {
			return err
		}
		var collision bool
		if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_workflow.tasks WHERE id=$1)`, row.fields["id"]).Scan(&collision); err != nil {
			return err
		}
		equal = equal && !collision
		if !equal {
			checkIssue(report, "tasks", row.id, "historical task owner, result, amount or workflow history differs from source")
		} else {
			report.Counts["check:history:tasks:matched"]++
		}
	}
	report.Counts["check:history:tasks:expected"] = int64(len(data.records))
	return nil
}
