package channelmarket

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// OwnerAnalyticsFilter uses a half-open UTC range: From <= timestamp < To.
// ChannelID is the public numeric channel ID, never the catalog ID.
type OwnerAnalyticsFilter struct {
	From      time.Time
	To        time.Time
	ChannelID string
	Model     string
}

type OwnerAnalyticsSummary struct {
	RequestCount         int64      `json:"request_count"`
	SuccessCount         int64      `json:"success_count"`
	ConsumerCount        int64      `json:"consumer_count"`
	PromptTokens         int64      `json:"prompt_tokens"`
	CompletionTokens     int64      `json:"completion_tokens"`
	ConsumerMicro        int64      `json:"consumer_micro"`
	GrossMicro           int64      `json:"gross_micro"`
	CommissionMicro      int64      `json:"commission_micro"`
	FeeMicro             int64      `json:"fee_micro"`
	NetMicro             int64      `json:"net_micro"`
	PendingIncomeMicro   int64      `json:"pending_income_micro"`
	ReleasedIncomeMicro  int64      `json:"released_income_micro"`
	ReclaimedIncomeMicro int64      `json:"reclaimed_income_micro"`
	NextAvailableAt      *time.Time `json:"next_available_at,omitempty"`
}

type OwnerAnalyticsPoint struct {
	Timestamp       time.Time `json:"timestamp"`
	RequestCount    int64     `json:"request_count"`
	SuccessCount    int64     `json:"success_count"`
	GrossMicro      int64     `json:"gross_micro"`
	CommissionMicro int64     `json:"commission_micro"`
	FeeMicro        int64     `json:"fee_micro"`
	NetMicro        int64     `json:"net_micro"`
}

type OwnerAnalyticsChannel struct {
	ChannelID            string `json:"channel_id"`
	GroupID              string `json:"group_id"`
	Name                 string `json:"name"`
	Model                string `json:"model"`
	RequestCount         int64  `json:"request_count"`
	SuccessCount         int64  `json:"success_count"`
	ConsumerCount        int64  `json:"consumer_count"`
	GrossMicro           int64  `json:"gross_micro"`
	CommissionMicro      int64  `json:"commission_micro"`
	FeeMicro             int64  `json:"fee_micro"`
	NetMicro             int64  `json:"net_micro"`
	PendingIncomeMicro   int64  `json:"pending_income_micro"`
	ReleasedIncomeMicro  int64  `json:"released_income_micro"`
	ReclaimedIncomeMicro int64  `json:"reclaimed_income_micro"`
}

type OwnerAnalyticsSettlement struct {
	ID              string    `json:"id"`
	RequestID       string    `json:"request_id"`
	CreatedAt       time.Time `json:"created_at"`
	ChannelID       string    `json:"channel_id"`
	Model           string    `json:"model"`
	BillingSource   string    `json:"billing_source"`
	ConsumerMicro   int64     `json:"consumer_micro"`
	GrossMicro      int64     `json:"gross_micro"`
	CommissionMicro int64     `json:"commission_micro"`
	FeeMicro        int64     `json:"fee_micro"`
	NetMicro        int64     `json:"net_micro"`
	AvailableAt     time.Time `json:"available_at"`
	State           string    `json:"state"`
}

type ChannelOwnerAnalytics struct {
	From                 time.Time                  `json:"from"`
	To                   time.Time                  `json:"to"`
	BucketSeconds        int64                      `json:"bucket_seconds"`
	Summary              OwnerAnalyticsSummary      `json:"summary"`
	Points               []OwnerAnalyticsPoint      `json:"points"`
	Channels             []OwnerAnalyticsChannel    `json:"channels"`
	Settlements          []OwnerAnalyticsSettlement `json:"settlements"`
	SettlementsTruncated bool                       `json:"settlements_truncated"`
}

func (f OwnerAnalyticsFilter) validate() error {
	if f.From.IsZero() || f.To.IsZero() || !f.From.Before(f.To) || f.To.Sub(f.From) > 366*24*time.Hour {
		return ErrInvalid
	}
	return validateOwnerSelection(f.ChannelID, f.Model)
}

func validateOwnerSelection(channel, model string) error {
	if channel != "" {
		id, err := strconv.ParseInt(channel, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != channel {
			return ErrInvalid
		}
	}
	if len(model) > 256 || strings.TrimSpace(model) != model || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return ErrInvalid
	}
	return nil
}

func (s *Service) checkOwnerSelection(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, a Actor, channel string) error {
	if a.UserID <= 0 && !a.Admin {
		return ErrInvalid
	}
	if channel == "" {
		return nil
	}
	var found bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.groups WHERE public_channel_id=$1 AND (owner_user_id=$2 OR $3))`, channel, a.UserID, a.Admin).Scan(&found); err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

// Requests use started_at for completed, eligible audited outcomes and
// created_at for historical usage without audits. Split-account usage is
// deduplicated by request/channel. Financial values always use settlement
// created_at; a settlement without retained usage remains visible with an
// empty (unknown) model. Current release/reclaim state is never time-rewritten.
func ownerAnalyticsChannels(ctx context.Context, tx pgx.Tx, a Actor, f OwnerAnalyticsFilter) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id FROM v3_channelmarket.groups WHERE (owner_user_id=$1 OR $2) AND ($3='' OR public_channel_id=$3)`, a.UserID, a.Admin, f.ChannelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func ownerAnalyticsQuery(a Actor, f OwnerAnalyticsFilter, channels []int64) (string, []any) {
	args := []any{pgx.QueryExecModeExec}
	bind := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args)-1)
	}
	owner, settlementOwner := "true", "true"
	if !a.Admin {
		user := bind(a.UserID)
		owner, settlementOwner = "g.owner_user_id="+user, "s.owner_user_id="+user
	}
	from, to := bind(f.From), bind(f.To)
	if f.ChannelID != "" {
		owner += " AND g.public_channel_id=" + bind(f.ChannelID)
	}
	channelIDs := bind(channels)
	usageModel, auditModel, settlementModel := "", "", ""
	if f.Model != "" {
		model := bind(f.Model)
		usageModel, auditModel = " AND l.model="+model, " AND a.model="+model
		settlementModel = " WHERE coalesce(s.audit_model,u.settlement_model,'')=" + model
	}
	usageRange := "l.created_at >= " + from + " AND l.created_at < " + to + usageModel
	// Without a model filter, the newest settlement rows can be selected before
	// resolving their historical model. A model filter must precede the limit.
	preview := `SELECT s.*,coalesce(s.audit_model,u.settlement_model,'') AS model
 FROM (SELECT * FROM raw_settlements ORDER BY created_at DESC,id DESC LIMIT 101) s
 LEFT JOIN legacy_usage u ON u.channel_id=s.channel_id AND u.request_id=s.request_id`
	if f.Model != "" {
		preview = `SELECT * FROM settlements ORDER BY created_at DESC,id DESC LIMIT 101`
	}
	// Build only applicable predicates. This query deliberately has no cached
	// prepared plan: hot channels and wide date ranges differ sharply in size.
	// Resolve channels in the same snapshot and bind their actual IDs. Joining
	// logs to a one-row owned CTE hides hot-channel cardinality from PostgreSQL,
	// causing hundreds of thousands of nested-loop audit lookups.
	// Any retained audit suppresses the legacy usage fallback, including audits
	// excluded by the current date/model/outcome filters. Aggregate that fallback
	// once: settlement models use all retained history, while request counters
	// include only rows matching the selected date and model.
	// Inline the wide financial rows and resolve both model cases in one join.
	// This avoids repeatedly spilling all settlements to disk and lets the
	// unfiltered preview use the owner/date index before resolving 101 models.
	query := `WITH owned AS MATERIALIZED (
 SELECT g.channel_id,g.id AS group_id,g.public_channel_id,g.display_name
 FROM v3_channelmarket.groups g WHERE ` + owner + `
), legacy_usage AS MATERIALIZED (
 SELECT l.channel_id,l.request_id,max(l.model) AS settlement_model,
 max(l.user_id) FILTER(WHERE ` + usageRange + `) AS user_id,
 max(l.model) FILTER(WHERE ` + usageRange + `) AS model,
 min(l.created_at) FILTER(WHERE ` + usageRange + `) AS timestamp,
 max(l.prompt_tokens) FILTER(WHERE ` + usageRange + `) AS prompt_tokens,
 max(l.completion_tokens) FILTER(WHERE ` + usageRange + `) AS completion_tokens,
 bool_or(l.terminal IN ('success','succeeded','completed','Completed','completed_no_usage','CompletedNoUsage')) FILTER(WHERE ` + usageRange + `) AS successful
 FROM v3_billing.usage_logs l
 WHERE l.channel_id=ANY(` + channelIDs + `::bigint[])
 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits a WHERE a.request_id=l.request_id)
 GROUP BY l.channel_id,l.request_id
), requests AS (
 SELECT a.final_channel_id AS channel_id,a.request_id,a.user_id,a.model,a.started_at AS timestamp,
 a.prompt_tokens,a.completion_tokens,a.status IN ('success','succeeded') AS successful
 FROM v3_audit.request_audits a
 WHERE a.final_channel_id=ANY(` + channelIDs + `::bigint[]) AND a.started_at >= ` + from + ` AND a.started_at < ` + to + ` AND a.completed_at <= ` + to + ` AND a.counted_in_success_rate` + auditModel + `
 UNION ALL
 SELECT u.channel_id,u.request_id,u.user_id,u.model,u.timestamp,u.prompt_tokens,u.completion_tokens,u.successful
 FROM legacy_usage u WHERE u.timestamp IS NOT NULL
), raw_settlements AS NOT MATERIALIZED (
 SELECT s.id,s.request_id,s.channel_id,s.billing_source,s.consumer_micro,s.gross_micro,
 s.commission_micro,s.fee_micro,s.net_micro,s.reclaimed_micro,s.status,s.available_at,s.created_at,a.model AS audit_model
 FROM v3_channelmarket.settlements s
 LEFT JOIN v3_audit.request_audits a ON a.request_id=s.request_id
 WHERE ` + settlementOwner + ` AND s.channel_id=ANY(` + channelIDs + `::bigint[]) AND s.created_at >= ` + from + ` AND s.created_at < ` + to + `
), settlements AS (
 SELECT s.*,coalesce(s.audit_model,u.settlement_model,'') AS model FROM raw_settlements s
 LEFT JOIN legacy_usage u ON s.audit_model IS NULL AND u.channel_id=s.channel_id AND u.request_id=s.request_id` + settlementModel + `
), preview_settlements AS MATERIALIZED (
 ` + preview + `
)
`
	return query, args
}

// OwnerAnalytics builds the request and settlement sets once in a single
// database snapshot so summary, series and ledger rows agree during release.
func (s *Service) OwnerAnalytics(ctx context.Context, a Actor, f OwnerAnalyticsFilter) (ChannelOwnerAnalytics, error) {
	r := ChannelOwnerAnalytics{From: f.From.UTC(), To: f.To.UTC(), BucketSeconds: 3600, Points: []OwnerAnalyticsPoint{}, Channels: []OwnerAnalyticsChannel{}, Settlements: []OwnerAnalyticsSettlement{}}
	if err := f.validate(); err != nil {
		return r, err
	}
	if s.pool == nil {
		return r, ErrUnavailable
	}
	if f.To.Sub(f.From) > 7*24*time.Hour {
		r.BucketSeconds = 86400
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := s.checkOwnerSelection(ctx, tx, a, f.ChannelID); err != nil {
			return err
		}
		// These exact reporting queries execute once per request. On large
		// histories, JIT compilation itself costs about a second of database CPU.
		if _, err := tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
			return err
		}
		channels, err := ownerAnalyticsChannels(ctx, tx, a, f)
		if err != nil {
			return err
		}
		query, args := ownerAnalyticsQuery(a, f, channels)
		args = append(args, r.BucketSeconds)
		query += strings.ReplaceAll(ownerAnalyticsResultSQL, "$BUCKET", "$"+strconv.Itoa(len(args)-1))
		if err := tx.QueryRow(ctx, query, args...).Scan(&r.Summary, &r.Points, &r.Channels, &r.Settlements); err != nil {
			return err
		}
		if len(r.Settlements) > 100 {
			r.SettlementsTruncated = true
			r.Settlements = r.Settlements[:100]
		}
		return nil
	})
	return r, err
}

const ownerAnalyticsResultSQL = `, request_stats AS MATERIALIZED (
 SELECT channel_id,model,user_id,date_bin($BUCKET::bigint*interval '1 second',timestamp,'1970-01-01 00:00:00+00'::timestamptz) AS timestamp,
 count(*) AS request_count,count(*) FILTER(WHERE successful) AS success_count,
 sum(prompt_tokens)::bigint AS prompt_tokens,sum(completion_tokens)::bigint AS completion_tokens
 FROM requests GROUP BY channel_id,model,user_id,4
), request_summary AS (
 SELECT coalesce(sum(request_count),0)::bigint AS request_count,coalesce(sum(success_count),0)::bigint AS success_count,
 count(DISTINCT user_id) FILTER(WHERE user_id>0) AS consumer_count,
 coalesce(sum(prompt_tokens),0)::bigint AS prompt_tokens,coalesce(sum(completion_tokens),0)::bigint AS completion_tokens
 FROM request_stats
), income_stats AS MATERIALIZED (
 SELECT channel_id,model,date_bin($BUCKET::bigint*interval '1 second',created_at,'1970-01-01 00:00:00+00'::timestamptz) AS timestamp,
 sum(consumer_micro)::bigint AS consumer_micro,sum(gross_micro)::bigint AS gross_micro,
 sum(commission_micro)::bigint AS commission_micro,sum(fee_micro)::bigint AS fee_micro,sum(net_micro)::bigint AS net_micro,
 coalesce(sum(net_micro-reclaimed_micro) FILTER(WHERE status='pending'),0)::bigint AS pending_income_micro,
 coalesce(sum(net_micro-reclaimed_micro) FILTER(WHERE status='released'),0)::bigint AS released_income_micro,
 sum(reclaimed_micro)::bigint AS reclaimed_income_micro,
 min(available_at) FILTER(WHERE status='pending' AND net_micro>reclaimed_micro) AS next_available_at
 FROM settlements GROUP BY channel_id,model,3
), income_summary AS (
 SELECT coalesce(sum(consumer_micro),0)::bigint AS consumer_micro,coalesce(sum(gross_micro),0)::bigint AS gross_micro,
 coalesce(sum(commission_micro),0)::bigint AS commission_micro,coalesce(sum(fee_micro),0)::bigint AS fee_micro,coalesce(sum(net_micro),0)::bigint AS net_micro,
 coalesce(sum(pending_income_micro),0)::bigint AS pending_income_micro,
 coalesce(sum(released_income_micro),0)::bigint AS released_income_micro,
 coalesce(sum(reclaimed_income_micro),0)::bigint AS reclaimed_income_micro,
 min(next_available_at) AS next_available_at
 FROM income_stats
), req_points AS (
 SELECT timestamp,sum(request_count)::bigint AS requests,sum(success_count)::bigint AS successes
 FROM request_stats GROUP BY 1
), income_points AS (
 SELECT timestamp,
 sum(gross_micro)::bigint AS gross,sum(commission_micro)::bigint AS commission,sum(fee_micro)::bigint AS fee,sum(net_micro)::bigint AS net
 FROM income_stats GROUP BY 1
), points AS (
 SELECT coalesce(r.timestamp,i.timestamp) AS timestamp,coalesce(r.requests,0) AS request_count,coalesce(r.successes,0) AS success_count,
 coalesce(i.gross,0) AS gross_micro,coalesce(i.commission,0) AS commission_micro,coalesce(i.fee,0) AS fee_micro,coalesce(i.net,0) AS net_micro
 FROM req_points r FULL JOIN income_points i ON r.timestamp=i.timestamp
), req_channels AS (
 SELECT channel_id,model,sum(request_count)::bigint AS requests,sum(success_count)::bigint AS successes,count(DISTINCT user_id) FILTER(WHERE user_id>0) AS consumers
 FROM request_stats GROUP BY channel_id,model
), income_channels AS (
 SELECT channel_id,model,sum(gross_micro)::bigint AS gross,sum(commission_micro)::bigint AS commission,sum(fee_micro)::bigint AS fee,sum(net_micro)::bigint AS net,
 sum(pending_income_micro)::bigint AS pending,
 sum(released_income_micro)::bigint AS released,sum(reclaimed_income_micro)::bigint AS reclaimed
 FROM income_stats GROUP BY channel_id,model
), combined AS (
 SELECT coalesce(r.channel_id,i.channel_id) AS channel_id,coalesce(r.model,i.model) AS model,coalesce(r.requests,0) AS requests,coalesce(r.successes,0) AS successes,coalesce(r.consumers,0) AS consumers,
 coalesce(i.gross,0) AS gross,coalesce(i.commission,0) AS commission,coalesce(i.fee,0) AS fee,coalesce(i.net,0) AS net,coalesce(i.pending,0) AS pending,coalesce(i.released,0) AS released,coalesce(i.reclaimed,0) AS reclaimed
 FROM req_channels r FULL JOIN income_channels i ON r.channel_id=i.channel_id AND r.model=i.model
), channels AS (
 SELECT o.public_channel_id AS channel_id,o.group_id,o.display_name AS name,x.model,x.requests AS request_count,x.successes AS success_count,x.consumers AS consumer_count,
 x.gross AS gross_micro,x.commission AS commission_micro,x.fee AS fee_micro,x.net AS net_micro,
 x.pending AS pending_income_micro,x.released AS released_income_micro,x.reclaimed AS reclaimed_income_micro
 FROM combined x JOIN owned o ON o.channel_id=x.channel_id
), preview AS (
 SELECT s.id,s.request_id,s.created_at,o.public_channel_id AS channel_id,s.model,s.billing_source,s.consumer_micro,s.gross_micro,
 s.commission_micro,s.fee_micro,s.net_micro,s.available_at,s.status AS state
 FROM preview_settlements s JOIN owned o ON o.channel_id=s.channel_id ORDER BY s.created_at DESC,s.id DESC LIMIT 101
)
SELECT to_jsonb(r)||to_jsonb(i),
 coalesce((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.timestamp) FROM points p),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(to_jsonb(c) ORDER BY c.net_micro DESC,c.channel_id,c.model) FROM channels c),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.created_at DESC,s.id DESC) FROM preview s),'[]'::jsonb)
FROM request_summary r CROSS JOIN income_summary i`

const ownerSettlementSelect = `SELECT s.id,s.request_id,s.created_at,o.public_channel_id,s.model,s.billing_source,s.consumer_micro,s.gross_micro,s.commission_micro,s.fee_micro,s.net_micro,s.available_at,s.status FROM settlements s JOIN owned o ON o.channel_id=s.channel_id`

func scanOwnerSettlement(row pgx.Row) (OwnerAnalyticsSettlement, error) {
	var item OwnerAnalyticsSettlement
	err := row.Scan(&item.ID, &item.RequestID, &item.CreatedAt, &item.ChannelID, &item.Model, &item.BillingSource, &item.ConsumerMicro, &item.GrossMicro, &item.CommissionMicro, &item.FeeMicro, &item.NetMicro, &item.AvailableAt, &item.State)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return item, err
}
