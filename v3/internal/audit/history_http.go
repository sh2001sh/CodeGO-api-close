package audit

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"
)

func parseEventQuery(r *http.Request) (EventQuery, error) {
	q, err := parseQuery(r)
	if err != nil {
		return EventQuery{}, err
	}
	e := EventQuery{Query: q}
	if value := r.URL.Query().Get("event_type"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 6 {
			return e, ErrInvalid
		}
		e.EventType = &n
	}
	return e, nil
}

func (s *Service) eventsHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseEventQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	page, err := s.ListEvents(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, page)
}

func (s *Service) eventsExportHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseEventQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	page, err := s.ListEvents(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="events.csv"`)
	if page.HasMore {
		w.Header().Set("X-Next-Cursor", page.NextCursor)
	}
	c := csv.NewWriter(w)
	if err := c.Write([]string{"id", "created_at", "event_type", "content", "model", "amount_micro_credits", "request_id"}); err != nil {
		return
	}
	for _, e := range page.Items {
		if err := c.Write([]string{strconv.FormatInt(e.ID, 10), e.CreatedAt.UTC().Format(time.RFC3339Nano),
			strconv.Itoa(e.EventType), safeCSV(e.Content), safeCSV(e.Model),
			strconv.FormatInt(int64(e.Amount), 10), safeCSV(e.RequestID)}); err != nil {
			return
		}
	}
	c.Flush()
}

func (s *Service) requestsHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	page, err := s.ListRequests(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, page)
}

func (s *Service) attemptsHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	// Attempts are selected by their already-authorized parent request; list
	// filters cannot change that ownership check.
	page, err := s.ListAttempts(r.Context(), p, r.PathValue("request"), q.Cursor, q.Limit)
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, page)
}
