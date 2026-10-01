package channelmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type reclaimFilter struct {
	Owners     []int64
	Max        credits.Micro
	Start, End int64
}

func parseReclaimFilter(raw []byte) (reclaimFilter, error) {
	var out reclaimFilter
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return out, ErrInvalid
	}
	for _, pair := range []struct {
		names []string
		dst   *int64
	}{{[]string{"MaxAmount", "max_amount", "max_amount_micro"}, (*int64)(&out.Max)}, {[]string{"StartTimestamp", "start_timestamp"}, &out.Start}, {[]string{"EndTimestamp", "end_timestamp"}, &out.End}} {
		for _, name := range pair.names {
			if value, ok := fields[name]; ok {
				if json.Unmarshal(value, pair.dst) != nil {
					return out, ErrInvalid
				}
				break
			}
		}
	}
	for _, key := range []string{"OwnerUserIDs", "owner_user_ids"} {
		if value, ok := fields[key]; ok {
			if json.Unmarshal(value, &out.Owners) != nil {
				return out, ErrInvalid
			}
			break
		}
	}
	if out.Max < 0 || len(out.Owners) > 1000 {
		return out, ErrInvalid
	}
	if out.Owners == nil {
		out.Owners = []int64{}
	}
	return out, nil
}

// reclaimJob is the imported partial-reclaim job row being resumed.
type reclaimJob struct {
	id          string
	filter      reclaimFilter
	owners      map[string]json.Number
	priorAmount credits.Micro
	priorCount  int64
	batch       int64
	actor       *int64
}

// reclaimableSettlement is a released settlement with outstanding reclaimable
// balance, locked for update within the resume transaction.
type reclaimableSettlement struct {
	id            string
	owner, amount int64
}

// ResumeReclaims resumes imported partial jobs one bounded transaction at a
// time. Settlement changes, ledger postings and progress commit together.
func (s *Service) ResumeReclaims(ctx context.Context, limit int) (int, error) {
	if s.poster == nil {
		return 0, ErrUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	done := 0
	for done < limit {
		jobID, err := s.resumeOneReclaimJobTx(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			if jobID != "" {
				if _, markErr := s.pool.Exec(ctx, `UPDATE v3_channelmarket.income_reclaims SET status='failed',error_message='资金回收未完成，请核对余额和目标金额后重试',updated_at=$2 WHERE id=$1 AND status IN ('pending','running')`, jobID, s.cfg.Now()); markErr != nil {
					return done, errors.Join(err, markErr)
				}
			}
			return done, err
		}
		done++
	}
	return done, nil
}

// resumeOneReclaimJobTx locks and advances exactly one pending/running
// reclaim job by one batch, in a single transaction. The returned jobID is
// populated even on error so the caller can mark the job failed.
func (s *Service) resumeOneReclaimJobTx(ctx context.Context) (string, error) {
	var jobID string
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		job, e := loadReclaimJobTx(ctx, tx)
		if e != nil {
			return e
		}
		jobID = job.id
		items, e := loadReclaimableSettlementsTx(ctx, tx, job.filter)
		if e != nil {
			return e
		}
		progress, status, e := s.applyReclaimItemsTx(ctx, tx, job, items)
		if e != nil {
			return e
		}
		return saveReclaimProgressTx(ctx, tx, s.cfg.Now(), job, progress, status)
	})
	return jobID, err
}

// loadReclaimJobTx locks the oldest pending/running reclaim job and parses
// its filter and running owner totals.
func loadReclaimJobTx(ctx context.Context, tx pgx.Tx) (reclaimJob, error) {
	var job reclaimJob
	var raw, ownersRaw []byte
	e := tx.QueryRow(ctx, `SELECT id,filter,amount_micro,count,batch_number,owner_amounts,actor_user_id FROM v3_channelmarket.income_reclaims WHERE status IN ('pending','running') ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job.id, &raw, &job.priorAmount, &job.priorCount, &job.batch, &ownersRaw, &job.actor)
	if e != nil {
		return job, e
	}
	job.filter, e = parseReclaimFilter(raw)
	if e != nil {
		return job, e
	}
	job.owners = map[string]json.Number{}
	decoder := json.NewDecoder(bytes.NewReader(ownersRaw))
	decoder.UseNumber()
	if e = decoder.Decode(&job.owners); e != nil {
		return job, e
	}
	return job, nil
}

// loadReclaimableSettlementsTx locks up to 100 released settlements with
// outstanding reclaimable balance matching the job's filter.
func loadReclaimableSettlementsTx(ctx context.Context, tx pgx.Tx, filter reclaimFilter) ([]reclaimableSettlement, error) {
	var start, end *time.Time
	if filter.Start > 0 {
		v := time.Unix(filter.Start, 0)
		start = &v
	}
	if filter.End > 0 {
		v := time.Unix(filter.End, 0)
		end = &v
	}
	rows, e := tx.Query(ctx, `SELECT id,owner_user_id,net_micro-reclaimed_micro FROM v3_channelmarket.settlements WHERE status='released' AND net_micro>reclaimed_micro AND (cardinality($1::bigint[])=0 OR owner_user_id=ANY($1::bigint[])) AND ($2::timestamptz IS NULL OR created_at>=$2) AND ($3::timestamptz IS NULL OR created_at<$3) ORDER BY owner_user_id,id LIMIT 100 FOR UPDATE`, filter.Owners, start, end)
	if e != nil {
		return nil, e
	}
	var items []reclaimableSettlement
	for rows.Next() {
		var i reclaimableSettlement
		if e = rows.Scan(&i.id, &i.owner, &i.amount); e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, i)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return items, nil
}

// applyReclaimItemsTx posts a reclaim ledger entry and updates the
// settlement row for each item, capping the total at job.filter.Max, and
// returns the updated progress and resulting job status.
func (s *Service) applyReclaimItemsTx(ctx context.Context, tx pgx.Tx, job reclaimJob, items []reclaimableSettlement) (IncomeResult, string, error) {
	progress := IncomeResult{Count: int(job.priorCount), Amount: job.priorAmount}
	status := "running"
	for _, i := range items {
		amount := credits.Micro(i.amount)
		if job.filter.Max > 0 {
			left := job.filter.Max - progress.Amount
			if left <= 0 {
				break
			}
			if amount > left {
				amount = left
			}
		}
		account, e := accountTx(ctx, tx, i.owner, "wallet")
		if e != nil {
			return progress, status, e
		}
		metadata := map[string]any{"imported_reclaim_job": job.id}
		if job.actor != nil {
			metadata["actor_user_id"] = *job.actor
		}
		if _, e = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -amount, Kind: "marketplace_reclaim", OperationID: fmt.Sprintf("market-reclaim-job:%s:%d:%s", job.id, job.batch+1, i.id), Reason: "resume authorized channel earnings reclaim", Metadata: metadata}); e != nil {
			return progress, status, e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.settlements SET reclaimed_micro=reclaimed_micro+$2,reclaimed_at=$3,status=CASE WHEN reclaimed_micro+$2=net_micro THEN 'reclaimed' ELSE 'released' END WHERE id=$1 AND status='released'`, i.id, int64(amount), s.cfg.Now()); e != nil {
			return progress, status, e
		}
		progress.Count++
		progress.Amount, e = progress.Amount.Add(amount)
		if e != nil {
			return progress, status, e
		}
		key := strconv.FormatInt(i.owner, 10)
		var previous int64
		if value, ok := job.owners[key]; ok {
			previous, e = value.Int64()
			if e != nil {
				return progress, status, e
			}
		}
		updated, e := credits.Micro(previous).Add(amount)
		if e != nil {
			return progress, status, e
		}
		job.owners[key] = json.Number(strconv.FormatInt(int64(updated), 10))
	}
	if job.filter.Max > 0 && progress.Amount >= job.filter.Max {
		status = "completed"
	} else if len(items) == 0 {
		if job.filter.Max > 0 {
			return progress, status, ErrConflict
		}
		status = "completed"
	}
	return progress, status, nil
}

// saveReclaimProgressTx persists the job's updated progress, owner totals
// and status, advancing the batch number by one.
func saveReclaimProgressTx(ctx context.Context, tx pgx.Tx, now time.Time, job reclaimJob, progress IncomeResult, status string) error {
	body, e := json.Marshal(progress)
	if e != nil {
		return e
	}
	ownerBody, e := json.Marshal(job.owners)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE v3_channelmarket.income_reclaims SET status=$2,response=$3,count=$4,amount_micro=$5,owner_amounts=$6,batch_number=batch_number+1,updated_at=$7,error_message='' WHERE id=$1 AND status IN ('pending','running')`, job.id, status, body, progress.Count, int64(progress.Amount), ownerBody, now)
	return e
}
