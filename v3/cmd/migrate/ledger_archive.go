package main

import (
	"errors"
	"os"
)

func migrationLedgerArchive() (bool, error) {
	switch os.Getenv("V3_MIGRATION_LEDGER_HISTORY") {
	case "", "copy":
		return false, nil
	case "archive":
		return true, nil
	default:
		return false, errors.New("V3_MIGRATION_LEDGER_HISTORY must be copy or archive")
	}
}
