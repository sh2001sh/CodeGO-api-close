package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/sh2001sh/new-api/v3/internal/legacy"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
)

func runDrain(ctx context.Context, output io.Writer) error {
	options := legacy.SourceDrainOptions{ExportedSnapshot: os.Getenv("V3_DRAIN_SOURCE_SNAPSHOT"),
		FrozenManifestSHA256: os.Getenv("V3_DRAIN_FROZEN_MANIFEST_SHA256")}
	report, drainErr := legacy.NewSourceDrainReport(options)
	if drainErr == nil {
		if os.Getenv("V3_SOURCE_PG_DSN") == "" {
			report.ErrorCode = "source_database_required"
			drainErr = errors.New("V3_SOURCE_PG_DSN must identify the read-only v2 source database")
		} else {
			source, err := pg.Connect(ctx, pg.Config{DSN: os.Getenv("V3_SOURCE_PG_DSN"), MaxConns: 1})
			if err != nil {
				report.ErrorCode = "source_connection_failed"
				// DSN parse diagnostics can contain credentials; the binding report
				// intentionally exposes only the connection failure classification.
				drainErr = errors.New("source postgres connection failed")
			} else {
				defer source.Close()
				report, drainErr = legacy.DrainSource(ctx, source.Pool, options)
			}
		}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}
	return drainErr
}
