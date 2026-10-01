package security

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"time"
)

// ExportCSV returns only actor-scoped rows and fails before writing on overflow.
// IDs are written as exact decimal text, including values above JavaScript's limit.
func (g *Guard) ExportCSV(ctx context.Context, a Actor, q Query, out io.Writer) error {
	where, args, err := auditWhere(a, q)
	if err != nil {
		return err
	}
	if g.pool == nil {
		return ErrInvalid
	}
	rows, err := g.pool.Query(ctx, `SELECT `+eventSelect+` FROM v3_security.security_audit_events`+where+` ORDER BY created_at DESC NULLS LAST,id LIMIT 20001`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		e, err := scanEvent(rows, a.Admin)
		if err != nil {
			return err
		}
		events = append(events, e)
		if len(events) > 20000 {
			return ErrExportLimit
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	w := csv.NewWriter(out)
	if err = w.Write([]string{"id", "request_id", "source", "decision", "risk_code", "severity", "user_id", "token_id", "token_name", "channel_id", "marketplace_channel_id", "marketplace_group_id", "owner_user_id", "model", "protocol", "http_status", "upstream_error_type", "upstream_error_code", "upstream_error_message", "upstream_error_body", "prompt_hash", "prompt_preview", "prompt_length", "message_count", "billing_result", "notification_status", "notification_targets", "notification_success", "notified_at", "review_status", "review_note", "reviewed_by", "reviewed_at", "created_at", "updated_at"}); err != nil {
		return err
	}
	number := func(v int64) string { return strconv.FormatInt(v, 10) }
	stamp := func(v *time.Time) string {
		if v == nil {
			return ""
		}
		return v.UTC().Format(time.RFC3339Nano)
	}
	for _, e := range events {
		row := []string{e.ID, e.RequestID, e.Source, e.Decision, e.RiskCode, e.Severity, number(e.UserID), number(e.TokenID), e.TokenName, number(e.ChannelID), e.MarketplaceChannelID, e.MarketplaceGroupID, number(e.OwnerUserID), e.Model, e.Protocol, strconv.Itoa(e.HTTPStatus), e.UpstreamErrorType, e.UpstreamErrorCode, e.UpstreamErrorMessage, e.UpstreamErrorBody, e.PromptHash, e.PromptPreview, strconv.Itoa(e.PromptLength), strconv.Itoa(e.MessageCount), e.BillingResult, e.NotificationStatus, strconv.Itoa(e.NotificationTargets), strconv.Itoa(e.NotificationSuccess), stamp(e.NotifiedAt), e.ReviewStatus, e.ReviewNote, number(e.ReviewedBy), stamp(e.ReviewedAt), stamp(e.CreatedAt), stamp(e.UpdatedAt)}
		for _, column := range []int{0, 1, 2, 3, 4, 5, 8, 10, 11, 13, 14, 16, 17, 18, 19, 20, 21, 24, 25, 29, 30} {
			row[column] = protectCSVText(row[column])
		}
		if err = w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// Spreadsheet readers can execute formula-looking text even inside CSV quotes.
// Preserve numeric columns as exact decimals; escape only untrusted text cells.
func protectCSVText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
