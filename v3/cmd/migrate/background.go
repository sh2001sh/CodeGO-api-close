package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/internal/legacy"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
)

func runBackground(ctx context.Context, apply bool, output io.Writer) error {
	if os.Getenv("V3_SOURCE_PG_DSN") == "" {
		return errors.New("V3_SOURCE_PG_DSN must identify the read-only v2 source database")
	}
	source, err := pg.Connect(ctx, pg.Config{DSN: os.Getenv("V3_SOURCE_PG_DSN"), MaxConns: 2})
	if err != nil {
		return err
	}
	defer source.Close()
	var repository *live.RedisBackgroundRepository
	if apply {
		var closeClient func()
		repository, closeClient, err = legacy.OpenBackgroundMigrationRepository(ctx)
		if err != nil {
			return err
		}
		defer closeClient()
	}
	importer := legacy.NewImporter(source.Pool, nil, nil)
	report, importErr := importer.ImportBackground(ctx, repository, apply)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		return err
	}
	return importErr
}
