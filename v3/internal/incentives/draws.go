package incentives

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func loadLocation(name string) (*time.Location, error) { return time.LoadLocation(name) }

// RecoverLuckyRewards completes rewards frozen before the daily lucky number
// feature was retired. It never creates draws, participants or new entitlements.
func (s *Service) RecoverLuckyRewards(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id FROM v3_commerce.subscription_lucky_draws WHERE status<>'completed' ORDER BY id LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if err = s.RetryDraw(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// RetryDraw never changes the stored winning number, eligibility or reward.
func (s *Service) RetryDraw(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_lucky_draws WHERE id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM v3_commerce.subscription_lucky_rewards WHERE draw_id=$1 AND credit_status<>'credited' ORDER BY id`, id)
	if err != nil {
		return err
	}
	var rewards []int64
	for rows.Next() {
		var reward int64
		if err = rows.Scan(&reward); err != nil {
			rows.Close()
			return err
		}
		rewards = append(rewards, reward)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, reward := range rewards {
		if err = s.settleReward(ctx, reward); err != nil {
			_, markErr := s.pool.Exec(ctx, `UPDATE v3_commerce.subscription_lucky_draws SET status='failed',error_message='reward settlement failed' WHERE id=$1 AND status<>'completed'`, id)
			if markErr != nil {
				return fmt.Errorf("settle: %w; record failure: %v", err, markErr)
			}
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_commerce.subscription_lucky_draws SET status='completed',error_message='',completed_at=COALESCE(completed_at,$2) WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_lucky_rewards WHERE draw_id=$1 AND credit_status<>'credited')`, id, s.now())
	return err
}
