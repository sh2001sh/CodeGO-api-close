package legacy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// checkProjection compares every supplied typed target column. The table and
// column names come only from importer code; source values remain parameters.
// It catches changed balances/relations as well as missing records.
func checkProjection(ctx context.Context, target pgx.Tx, table string, fields map[string]any) (bool, error) {
	parts := strings.Split(table, ".")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "v3_") || len(fields) == 0 {
		return false, fmt.Errorf("legacy: invalid check projection table")
	}
	columns := make([]string, 0, len(fields))
	for name := range fields {
		columns = append(columns, name)
	}
	sort.Strings(columns)
	conditions, args := make([]string, len(columns)), make([]any, len(columns))
	for i, name := range columns {
		conditions[i] = pgx.Identifier{name}.Sanitize() + " IS NOT DISTINCT FROM $" + fmt.Sprint(i+1)
		args[i] = fields[name]
	}
	var equal bool
	err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier(parts).Sanitize()+" WHERE "+strings.Join(conditions, " AND ")+")", args...).Scan(&equal)
	return equal, err
}

func checkIssue(report *Report, entity string, id int64, detail string) {
	report.Issues = append(report.Issues, Issue{entity, id, "target_mismatch", detail})
}
