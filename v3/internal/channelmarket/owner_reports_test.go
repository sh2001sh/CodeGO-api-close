package channelmarket

import (
	"encoding/csv"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogCursorRejectsIncompleteOrMalformedCursor(t *testing.T) {
	for _, query := range []string{"before_id=1", "before=invalid", "before=2026-09-30T12:00:00Z&before_id=-1", "before=2026-09-30T12:00:00Z&before_id=9223372036854775808"} {
		r := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
		if _, _, err := logCursor(r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("malformed cursor accepted %q: %v", query, err)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/?before=2026-09-30T12:00:00.123456Z&before_id=4", nil)
	stamp, id, err := logCursor(r)
	if err != nil || id != 4 || stamp.Nanosecond() != 123456000 {
		t.Fatalf("complete cursor lost: %v %d %v", stamp, id, err)
	}
}

func TestCSVExportFailureDoesNotSendPartialSuccessfulDownload(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	w := httptest.NewRecorder()
	s.exportCSV(w, httptest.NewRequest(http.MethodGet, "/", nil), "test.csv", func(writer *csv.Writer) error {
		if err := writer.Write([]string{"partial-export-must-not-escape"}); err != nil {
			return err
		}
		return errors.New("injected database failure")
	})
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "partial-export-must-not-escape") || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("partial CSV returned as success: %d %s", w.Code, w.Body.String())
	}
}

func TestCSVFormulaEscapingHandlesLeadingWhitespace(t *testing.T) {
	for _, value := range []string{"=1+1", " \t=1+1", "\r\n@SUM(A1)", "+cmd", "-2+3"} {
		if got := csvText(value); got != "'"+value {
			t.Fatalf("unsafe CSV formula %q -> %q", value, got)
		}
	}
	if got := csvText(" ordinary model"); got != " ordinary model" {
		t.Fatalf("ordinary text changed: %q", got)
	}
}

func TestUsageSeriesRejectsUserIDOverflowEvenWithValidHours(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	r := httptest.NewRequest(http.MethodGet, "/?range_hours=24", nil)
	r.SetPathValue("userId", "9223372036854775808")
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	s.httpUsageSeries(w, r, Actor{UserID: 1})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overflowed user ID reached query: status %d body %s", w.Code, w.Body.String())
	}
}
