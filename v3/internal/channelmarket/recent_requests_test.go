package channelmarket

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestRecentRequestSlotsAreHourlyOldestFirstAndEmpty(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 42, 0, 0, time.UTC)
	slots := emptyRecentRequests(now)
	if len(slots) != 6 || slots[0].Ts != time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC).Unix() || slots[5].Ts != time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("slots do not match last six hours: %v", slots)
	}
	for i, slot := range slots {
		if slot.RequestCount != 0 || slot.SuccessRate != 0 || (i > 0 && slot.Ts-slots[i-1].Ts != 3600) {
			t.Fatalf("empty slot looks measured: %+v", slot)
		}
	}
}

type failedRecentQuery struct {
	calls int
	ids   []int64
}

func (q *failedRecentQuery) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	q.calls++
	q.ids = args[1].([]int64)
	return nil, errors.New("request observations unavailable")
}

func TestRecentRequestQueryIsBatchedAndFailureIsReported(t *testing.T) {
	q := &failedRecentQuery{}
	if err := attachRecentRequests(context.Background(), q, nil, time.Now()); err != nil || q.calls != 0 {
		t.Fatalf("empty authorized list queried data: %v %d", err, q.calls)
	}
	groups := []ChannelView{{InternalChannelID: 7}, {InternalChannelID: 9}}
	if err := attachRecentRequests(context.Background(), q, groups, time.Now()); err == nil || q.calls != 1 || len(q.ids) != 2 || q.ids[0] != 7 || q.ids[1] != 9 {
		t.Fatalf("N+1 query or swallowed observation error: %v %d %v", err, q.calls, q.ids)
	}
}
