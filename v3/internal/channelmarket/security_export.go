package channelmarket

import (
	"context"
	"encoding/csv"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
)

func (s *Service) SecurityPage(ctx context.Context, a Actor, beforeID int64, limit int) ([]SecurityEvent, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	if beforeID < 0 || a.UserID <= 0 && !a.Admin {
		return nil, ErrInvalid
	}
	if beforeID == 0 {
		beforeID = math.MaxInt64
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.channel_id,e.actor_user_id,e.kind,e.status,e.details,e.created_at
FROM v3_channelmarket.security_audit_events e LEFT JOIN v3_catalog.channels c ON c.id=e.channel_id
WHERE (c.owner_user_id=$1 OR $2) AND e.id<$3 ORDER BY e.id DESC LIMIT $4`, a.UserID, a.Admin, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SecurityEvent{}
	for rows.Next() {
		var e SecurityEvent
		if err = rows.Scan(&e.ID, &e.ChannelID, &e.ActorUserID, &e.Kind, &e.Status, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// ExportSecurity omits details, which may contain raw upstream error bodies.
func (s *Service) ExportSecurity(ctx context.Context, a Actor, writer *csv.Writer) error {
	if err := writer.Write([]string{"id", "channel_id", "actor_user_id", "kind", "status", "created_at"}); err != nil {
		return err
	}
	var beforeID int64
	for {
		items, err := s.SecurityPage(ctx, a, beforeID, 1000)
		if err != nil {
			return err
		}
		for _, e := range items {
			channel := ""
			if e.ChannelID != nil {
				channel = strconv.FormatInt(*e.ChannelID, 10)
			}
			if err = writer.Write([]string{strconv.FormatInt(e.ID, 10), channel, strconv.FormatInt(e.ActorUserID, 10), csvText(e.Kind), csvText(e.Status), e.CreatedAt.Format(time.RFC3339Nano)}); err != nil {
				return err
			}
		}
		if len(items) < 1000 {
			writer.Flush()
			return writer.Error()
		}
		beforeID = items[len(items)-1].ID
	}
}

func (s *Service) httpExportSecurity(w http.ResponseWriter, r *http.Request, a Actor) {
	if s.delegateSecurityAudit(w, r) {
		return
	}
	s.exportCSV(w, r, "channel-security.csv", func(writer *csv.Writer) error {
		return s.ExportSecurity(r.Context(), a, writer)
	})
}

// Spool before sending HTTP headers, so a database or CSV failure is returned
// as an explicit error instead of a successful but silently truncated file.
func (s *Service) exportCSV(w http.ResponseWriter, r *http.Request, filename string, write func(*csv.Writer) error) {
	file, err := os.CreateTemp("", "channelmarket-export-*.csv")
	if err != nil {
		s.result(w, nil, err)
		return
	}
	defer func() {
		if e := file.Close(); e != nil {
			s.log.Warn("close channel market export", "err", e)
		}
		if e := os.Remove(file.Name()); e != nil {
			s.log.Warn("remove channel market export", "err", e)
		}
	}()
	writer := csv.NewWriter(file)
	err = write(writer)
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		s.result(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if _, err = io.Copy(w, file); err != nil {
		s.log.WarnContext(r.Context(), "channel market export disconnected", "err", err)
	}
}
