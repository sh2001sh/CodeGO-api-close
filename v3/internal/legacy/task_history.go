package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type taskHistoryRecord struct {
	id     int64
	fields map[string]any
}

type taskHistoryData struct{ records []taskHistoryRecord }

func loadTaskHistory(ctx context.Context, source pgx.Tx, sources map[string]string, users map[int64]bool, report *Report) (*taskHistoryData, error) {
	d := &taskHistoryData{}
	workflows, err := loadTaskWorkflowHistory(ctx, source, sources, report)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	err = walkHistory(ctx, source, sources["tasks"], func(raw json.RawMessage) error {
		var row commerceRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("legacy: invalid async task row")
		}
		id, _ := row.integer("id")
		publicID, _ := row.text("task_id")
		status, _ := row.text("status")
		// Pending rows are already reported by the source-state guard.
		if status != "SUCCESS" && status != "FAILURE" {
			return nil
		}
		record, err := projectTaskHistory(row)
		if err == nil && !users[record.fields["user_id"].(int64)] {
			err = fmt.Errorf("async task references an absent user")
		}
		if err == nil && seen[publicID] {
			err = fmt.Errorf("duplicate public async task ID")
		}
		if err != nil {
			report.Issues = append(report.Issues, Issue{"tasks", id, "invalid_task_history", err.Error()})
			return nil
		}
		seen[publicID] = true
		history := workflows[publicID]
		wantState := "succeeded"
		if status == "FAILURE" {
			wantState = "failed"
		}
		for _, metadata := range history {
			var state struct{ Source, Status string }
			if json.Unmarshal(metadata, &state) != nil {
				return fmt.Errorf("legacy: invalid projected workflow metadata")
			}
			if state.Source == "task_workflows" && state.Status != wantState && (wantState != "failed" || state.Status != "timeout") {
				report.Issues = append(report.Issues, Issue{"tasks", id, "inconsistent_task_workflow", "task status differs from finalized workflow"})
			}
		}
		if history == nil {
			history = []json.RawMessage{}
		}
		encoded, err := json.Marshal(history)
		if err != nil {
			return err
		}
		record.fields["workflow_history"] = json.RawMessage(encoded)
		d.records = append(d.records, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for publicID := range workflows {
		if !seen[publicID] {
			report.Issues = append(report.Issues, Issue{"task_workflows", 0, "missing_terminal_task", "workflow history references an absent or nonterminal public task"})
		}
	}
	report.Counts["history.tasks"] = int64(len(d.records))
	return d, nil
}

func projectTaskHistory(row commerceRow) (taskHistoryRecord, error) {
	var out taskHistoryRecord
	id, err := row.integer("id")
	if err != nil || id <= 0 {
		return out, fmt.Errorf("positive async task source ID is required")
	}
	out.id, out.fields = id, map[string]any{"source_id": id}
	for _, key := range []string{"task_id", "user_id", "channel_id", "quota", "status"} {
		if len(row[key]) == 0 || string(row[key]) == "null" {
			return out, fmt.Errorf("async task %s is required", key)
		}
	}
	publicID, err := row.text("task_id")
	if err != nil || strings.TrimSpace(publicID) == "" || len(publicID) > 191 {
		return out, fmt.Errorf("async task public ID is invalid")
	}
	out.fields["id"] = publicID
	for _, key := range []string{"user_id", "channel_id"} {
		value, err := row.integer(key)
		if err != nil || value < 0 || (key == "user_id" && value == 0) {
			return out, fmt.Errorf("async task %s is invalid", key)
		}
		out.fields[key] = value
	}
	amount, err := commerceUnits(row, "quota")
	if err != nil {
		return out, err
	}
	out.fields["actual_credits"] = amount
	for source, target := range map[string]string{"group": "group_name", "platform": "provider", "action": "action", "progress": "progress"} {
		value, err := row.text(source)
		if err != nil {
			return out, err
		}
		out.fields[target] = value
	}
	status, err := row.text("status")
	if err != nil || (status != "SUCCESS" && status != "FAILURE") {
		return out, fmt.Errorf("async task must be terminal")
	}
	out.fields["status"] = "completed"
	if status == "FAILURE" {
		out.fields["status"] = "failed"
	}
	var properties struct {
		Model    string `json:"origin_model_name"`
		Upstream string `json:"upstream_model_name"`
	}
	var private struct {
		UpstreamID string `json:"upstream_task_id"`
		URL        string `json:"result_url"`
	}
	if json.Unmarshal(historyMetadata(row["properties"]), &properties) != nil || json.Unmarshal(historyMetadata(row["private_data"]), &private) != nil {
		return out, fmt.Errorf("invalid async task model or result metadata")
	}
	if properties.Model == "" {
		properties.Model = properties.Upstream
	}
	if properties.Model == "" {
		properties.Model = "legacy:" + fmt.Sprint(out.fields["provider"])
	}
	if properties.Upstream == "" {
		properties.Upstream = properties.Model
	}
	if private.UpstreamID == "" {
		private.UpstreamID = publicID
	}
	out.fields["model"], out.fields["upstream_model"], out.fields["upstream_id"] = properties.Model, properties.Upstream, private.UpstreamID
	reason, err := row.text("fail_reason")
	if err != nil {
		return out, err
	}
	if status == "SUCCESS" {
		// Older video rows store their completed URL in fail_reason.
		if private.URL == "" && (strings.HasPrefix(reason, "https://") || strings.HasPrefix(reason, "http://")) {
			private.URL = reason
		}
		reason = ""
	}
	out.fields["result_url"], out.fields["error_message"] = private.URL, reason
	out.fields["provider_data"] = historyMetadata(row["data"])
	for _, key := range []string{"created_at", "updated_at"} {
		value, err := row.epoch(key, false)
		if err != nil {
			return out, err
		}
		out.fields[key] = value
	}
	created, _ := row.integer("created_at")
	updated, _ := row.integer("updated_at")
	if updated < created {
		return out, fmt.Errorf("async task update precedes creation")
	}
	return out, nil
}
