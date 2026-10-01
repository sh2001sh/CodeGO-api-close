package billing

import (
	"context"
	"errors"

	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

type backgroundFactsLoader interface {
	BackgroundTaskExists(context.Context, string, int64, int64) (bool, error)
}

type backgroundTaskLoader struct {
	BalanceLoader
	repository live.BackgroundJobRepository
}

func (l *backgroundTaskLoader) BackgroundTaskExists(ctx context.Context, id string, userID, keyID int64) (bool, error) {
	job, err := l.repository.GetOwned(ctx, id, userID, keyID)
	if errors.Is(err, live.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.ID != id || job.UserID != userID || job.KeyID != keyID {
		return false, errors.New("billing: background ownership mismatch")
	}
	return true, nil
}

func (l *backgroundTaskLoader) AsyncTaskExists(ctx context.Context, id string, userID, keyID int64) (bool, error) {
	loader, ok := l.BalanceLoader.(AsyncTaskLoader)
	if !ok {
		return false, errors.New("billing: workflow task facts unavailable")
	}
	return loader.AsyncTaskExists(ctx, id, userID, keyID)
}

func (l *backgroundTaskLoader) PostingState(ctx context.Context, operationID, transactionID string) (PostingState, error) {
	loader, ok := l.BalanceLoader.(PostingStateLoader)
	if !ok {
		return PostingInProgress, errors.New("billing: business transaction facts unavailable")
	}
	return loader.PostingState(ctx, operationID, transactionID)
}
