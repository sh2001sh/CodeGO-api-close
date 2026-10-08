package channelmarket

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) RegisterOwnerAnalyticsHTTP(mux *http.ServeMux, auth Authenticate) {
	mux.Handle("GET /api/marketplace/channels/mine/analytics", s.protect(auth, false, false, s.httpOwnerAnalytics))
	mux.Handle("GET /api/marketplace/channels/mine/analytics/export", s.protect(auth, false, false, s.httpOwnerAnalyticsExport))
}

func (s *Service) ownerAnalyticsFilter(r *http.Request) (OwnerAnalyticsFilter, error) {
	now := s.cfg.Now().UTC()
	f := OwnerAnalyticsFilter{From: now.Add(-7 * 24 * time.Hour), To: now, ChannelID: r.URL.Query().Get("channel_id"), Model: r.URL.Query().Get("model")}
	var err error
	if value := r.URL.Query().Get("to"); value != "" {
		f.To, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return f, ErrInvalid
		}
		// A lone end date selects the seven days preceding that date.
		f.From = f.To.Add(-7 * 24 * time.Hour)
	}
	if value := r.URL.Query().Get("from"); value != "" {
		f.From, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return f, ErrInvalid
		}
	}
	f.From, f.To = f.From.UTC(), f.To.UTC()
	return f, f.validate()
}

func ownerReportFilter(r *http.Request) (OwnerAnalyticsFilter, error) {
	f := OwnerAnalyticsFilter{ChannelID: r.URL.Query().Get("channel_id"), Model: r.URL.Query().Get("model")}
	var err error
	if value := r.URL.Query().Get("from"); value != "" {
		f.From, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return f, ErrInvalid
		}
	}
	if value := r.URL.Query().Get("to"); value != "" {
		f.To, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return f, ErrInvalid
		}
	}
	return f, validateOwnerReportFilter(f)
}

func validateOwnerReportFilter(f OwnerAnalyticsFilter) error {
	if !f.From.IsZero() && !f.To.IsZero() && (!f.From.Before(f.To) || f.To.Sub(f.From) > 366*24*time.Hour) {
		return ErrInvalid
	}
	return validateOwnerSelection(f.ChannelID, f.Model)
}

func optionalOwnerBounds(f OwnerAnalyticsFilter) (any, any) {
	var from, to any
	if !f.From.IsZero() {
		from = f.From
	}
	if !f.To.IsZero() {
		to = f.To
	}
	return from, to
}

func (s *Service) httpOwnerAnalytics(w http.ResponseWriter, r *http.Request, a Actor) {
	f, err := s.ownerAnalyticsFilter(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	data, err := s.OwnerAnalytics(r.Context(), a, f)
	s.result(w, data, err)
}

func (s *Service) httpOwnerAnalyticsExport(w http.ResponseWriter, r *http.Request, a Actor) {
	f, err := s.ownerAnalyticsFilter(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	s.exportCSV(w, r, "channel-settlements.csv", func(writer *csv.Writer) error {
		return s.ExportOwnerAnalytics(r.Context(), a, f, writer)
	})
}

// ExportOwnerAnalytics streams every matching settlement in a stable snapshot,
// without the 100-row preview limit. Missing historical usage does not remove
// financial records. Export failure is spooled before HTTP headers are sent.
func (s *Service) ExportOwnerAnalytics(ctx context.Context, a Actor, f OwnerAnalyticsFilter, writer *csv.Writer) error {
	if err := f.validate(); err != nil {
		return err
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := s.checkOwnerSelection(ctx, tx, a, f.ChannelID); err != nil {
			return err
		}
		channels, err := ownerAnalyticsChannels(ctx, tx, a, f)
		if err != nil {
			return err
		}
		query, args := ownerAnalyticsQuery(a, f, channels)
		rows, err := tx.Query(ctx, query+ownerSettlementSelect+` ORDER BY s.created_at DESC,s.id DESC`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if err = writer.Write([]string{"settlement_id", "request_id", "created_at", "channel_id", "model", "billing_source", "consumer_micro", "gross_micro", "commission_micro", "fee_micro", "net_micro", "available_at", "state"}); err != nil {
			return err
		}
		for rows.Next() {
			item, err := scanOwnerSettlement(rows)
			if err != nil {
				return err
			}
			if err = writer.Write([]string{csvText(item.ID), csvText(item.RequestID), item.CreatedAt.UTC().Format(time.RFC3339Nano), item.ChannelID, csvText(item.Model), csvText(item.BillingSource), strconv.FormatInt(item.ConsumerMicro, 10), strconv.FormatInt(item.GrossMicro, 10), strconv.FormatInt(item.CommissionMicro, 10), strconv.FormatInt(item.FeeMicro, 10), strconv.FormatInt(item.NetMicro, 10), item.AvailableAt.UTC().Format(time.RFC3339Nano), csvText(item.State)}); err != nil {
				return err
			}
		}
		if err = rows.Err(); err != nil {
			return err
		}
		writer.Flush()
		return writer.Error()
	})
}
