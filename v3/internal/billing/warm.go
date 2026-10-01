package billing

import "context"

// WarmBalances installs balances before serving traffic. Cold reload remains
// available after Redis data loss, but a normal request never needs PG reads.
func (s *Settler) WarmBalances(ctx context.Context, accountIDs []int64) error {
	for _, id := range accountIDs {
		if err := s.installBalance(ctx, id, keysFor(id, "")); err != nil {
			return err
		}
	}
	return nil
}
