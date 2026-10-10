package main

import (
	"errors"
	"os"
	"time"
)

func migrationHistoryCutoff() (time.Time, error) {
	raw := os.Getenv("V3_MIGRATION_HISTORY_CUTOFF")
	if raw == "" {
		return time.Time{}, nil
	}
	cutoff, err := time.Parse(time.RFC3339, raw)
	if err != nil || cutoff.Year() < 1970 || cutoff.After(time.Now()) || cutoff.Nanosecond() != 0 {
		return time.Time{}, errors.New("V3_MIGRATION_HISTORY_CUTOFF must be a past RFC3339 timestamp with whole seconds")
	}
	return cutoff.UTC(), nil
}
