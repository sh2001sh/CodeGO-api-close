package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// replay runs after the user lock. A request's payload fingerprint is persisted
// with its response so retries cannot mint inventory or replace their arguments.
func replay(ctx context.Context, tx pgx.Tx, userID int64, kind, requestID string, input, output any) (bool, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return false, err
	}
	fingerprint := sha256.Sum256(payload)
	var stored, response []byte
	err = tx.QueryRow(ctx, `SELECT fingerprint, response FROM v3_marketplace.operations WHERE user_id=$1 AND kind=$2 AND request_id=$3`, userID, kind, requestID).Scan(&stored, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(stored) != string(fingerprint[:]) {
		return false, ErrConflict
	}
	return true, json.Unmarshal(response, output)
}

func remember(ctx context.Context, tx pgx.Tx, userID int64, kind, requestID string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(payload)
	response, err := json.Marshal(output)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.operations(user_id,kind,request_id,fingerprint,response) VALUES($1,$2,$3,$4,$5)`, userID, kind, requestID, fingerprint[:], response)
	return err
}

func operation(kind string, userID int64, requestID string, index int) string {
	return fmt.Sprintf("marketplace:%s:%d:%s:%d", kind, userID, requestID, index)
}

func stateError(err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return fmt.Errorf("%w: duplicate business operation", ErrConflict)
	}
	return err
}
