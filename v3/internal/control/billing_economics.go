package control

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

var errInvalidFundingEconomicsDay = errors.New("control: invalid report day")

func (b *Billing) fundingEconomics(w http.ResponseWriter, r *http.Request) {
	principal, ok := r.Context().Value(principalContextKey{}).(Principal)
	if !ok || !principal.Root {
		Fail(w, http.StatusForbidden, "root_required", "需要根管理员权限")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		Fail(w, http.StatusBadRequest, "invalid_day", "日期查询格式无效")
		return
	}
	day, err := fundingEconomicsDay(query, time.Now())
	if err != nil {
		if errors.Is(err, errInvalidFundingEconomicsDay) {
			Fail(w, http.StatusBadRequest, "invalid_day", "日期必须是有效的 YYYY-MM-DD 格式")
		} else {
			b.fail(w, err)
		}
		return
	}
	report, err := ledger.DailyFundingEconomics(r.Context(), b.pool, day)
	if err != nil {
		b.fail(w, err)
		return
	}
	Reply(w, http.StatusOK, report)
}

// Both explicit dates and the default day follow the ledger's Shanghai boundary.
func fundingEconomicsDay(query url.Values, now time.Time) (time.Time, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, err
	}
	values, supplied := query["day"]
	if !supplied {
		local := now.In(location)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location), nil
	}
	if len(values) != 1 || len(values[0]) != 10 {
		return time.Time{}, errInvalidFundingEconomicsDay
	}
	day, err := time.ParseInLocation("2006-01-02", values[0], location)
	if err != nil || day.Year() < 1 || day.Format("2006-01-02") != values[0] {
		return time.Time{}, errInvalidFundingEconomicsDay
	}
	return day, nil
}
