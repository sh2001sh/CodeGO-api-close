package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func taskWorkflowSource(sources map[string]string, name string) string {
	if table := sources["workflow_"+name]; table != "" {
		return table
	}
	return sources[name]
}

// Preserve only the task workflow's documented result/identity fields. In
// particular, the task's private_data credentials never enter this projection.
func loadTaskWorkflowHistory(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) (map[string][]json.RawMessage, error) {
	type workflowRecord struct{ publicID, status string }
	workflows := map[string]workflowRecord{}
	out := map[string][]json.RawMessage{}
	terminal := map[string]bool{}
	publicIDs, detailIDs := map[string]bool{}, map[string]bool{}
	for _, name := range []string{"task_workflows", "task_snapshots", "task_terminal_results"} {
		rows, err := loadRows(ctx, source, taskWorkflowSource(sources, name))
		if err != nil {
			return nil, err
		}
		report.Counts["history."+name] = int64(len(rows))
		for _, raw := range rows {
			var row commerceRow
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, fmt.Errorf("legacy: invalid async workflow metadata")
			}
			workflowID, e := row.text("workflow_id")
			if e != nil || workflowID == "" {
				report.Issues = append(report.Issues, Issue{name, 0, "invalid_task_workflow", "workflow ID is required"})
				continue
			}
			fields := ""
			if name == "task_workflows" {
				publicID, _ := row.text("public_task_id")
				status, _ := row.text("status")
				if status != "succeeded" && status != "failed" && status != "timeout" {
					continue // source-state validation reports the pending workflow
				}
				if publicID == "" || workflows[workflowID].publicID != "" || publicIDs[publicID] {
					report.Issues = append(report.Issues, Issue{name, 0, "invalid_task_workflow", "public task ID is missing or workflow ID is duplicated"})
					continue
				}
				workflows[workflowID] = workflowRecord{publicID, status}
				publicIDs[publicID] = true
				fields = "workflow_id public_task_id request_id account_id provider_code channel_id reservation_id task_kind temporal_workflow_id temporal_run_id status terminal_state timeout_at result_url result_meta created_at updated_at"
			} else if workflows[workflowID].publicID == "" {
				report.Issues = append(report.Issues, Issue{name, 0, "missing_terminal_workflow", "workflow detail references an absent or nonterminal workflow"})
				continue
			} else if name == "task_snapshots" {
				id, _ := row.text("snapshot_id")
				if id == "" || detailIDs[name+":"+id] {
					report.Issues = append(report.Issues, Issue{name, 0, "invalid_task_snapshot", "snapshot ID is missing or duplicated"})
					continue
				}
				detailIDs[name+":"+id] = true
				fields = "snapshot_id workflow_id provider_state provider_progress raw_payload result_url failure_reason created_at"
			} else {
				id, _ := row.text("terminal_result_id")
				state, _ := row.text("terminal_state")
				settlement, _ := row.text("settlement_status")
				want := "refunded"
				if state == "succeeded" {
					want = "settled"
				}
				if id == "" || detailIDs[name+":"+id] || terminal[workflowID] || state != workflows[workflowID].status || settlement != want {
					report.Issues = append(report.Issues, Issue{name, 0, "unsettled_task_workflow", "terminal result is duplicated or status/settlement is inconsistent"})
					continue
				}
				terminal[workflowID] = true
				detailIDs[name+":"+id] = true
				fields = "terminal_result_id workflow_id terminal_state settlement_status result_url result_meta finalized_at"
			}
			projection := commerceRow{"source": json.RawMessage(fmt.Sprintf("%q", name))}
			for _, field := range strings.Fields(fields) {
				if value := row[field]; len(value) > 0 {
					projection[field] = value
				}
			}
			encoded, err := json.Marshal(projection)
			if err != nil {
				return nil, err
			}
			publicID := workflows[workflowID].publicID
			out[publicID] = append(out[publicID], encoded)
		}
	}
	for id := range workflows {
		if !terminal[id] {
			report.Issues = append(report.Issues, Issue{"task_workflows", 0, "missing_task_settlement", "terminal workflow has no finalized settlement result"})
		}
	}
	for publicID := range out {
		sort.Slice(out[publicID], func(i, j int) bool { return string(out[publicID][i]) < string(out[publicID][j]) })
	}
	return out, nil
}
