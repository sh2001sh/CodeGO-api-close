package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

// ImportFiles is a separate, resumable asset step before the database import.
// Per-file publication is atomic; copied assets cannot authenticate a user until
// the separate database import succeeds. The original source stays read-only.
func (m *Importer) ImportFiles(ctx context.Context, sourceDir, targetDir string, apply bool) (Report, error) {
	report := Report{Counts: map[string]int64{}, Amounts: map[string]string{}, Issues: []Issue{}}
	if m.source == nil {
		return report, errors.New("legacy: source database is required")
	}
	if apply {
		if err := independentFileVolumes(sourceDir, targetDir); err != nil {
			return report, err
		}
	}
	tx, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		return report, err
	}
	files, err := loadSourceFiles(ctx, tx, sources, &report)
	if err != nil {
		return report, err
	}
	if len(report.Issues) > 0 {
		return report, errors.New("legacy: file metadata validation failed")
	}
	if len(files) == 0 {
		report.Applied = apply
		return report, tx.Commit(ctx)
	}
	root, err := os.OpenRoot(sourceDir)
	if err != nil {
		return report, fmt.Errorf("legacy: open read-only source file volume: %w", err)
	}
	defer func() { _ = root.Close() }()
	// Verify all bytes before publishing any asset.
	for _, file := range files {
		if err = verifyFileBytes(ctx, root, file.StoragePath, file.File); err != nil {
			return report, err
		}
	}
	if apply {
		if sourceDir == "" || targetDir == "" {
			return report, errors.New("legacy: source and target file volumes are required")
		}
		store, openErr := live.NewDiskFileStore(targetDir)
		if openErr != nil {
			return report, openErr
		}
		defer func() { _ = store.Close() }()
		for _, file := range files {
			content, openErr := root.Open(file.StoragePath)
			if openErr != nil {
				return report, openErr
			}
			_, importErr := store.ImportFile(ctx, file.File, content)
			closeErr := content.Close()
			if importErr != nil {
				return report, fmt.Errorf("legacy: import file %s: %w", file.ID, importErr)
			}
			if closeErr != nil {
				return report, closeErr
			}
		}
		if err = checkFileAssets(ctx, targetDir, files, &report); err != nil {
			return report, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return report, err
	}
	report.Applied = apply
	return report, nil
}

func independentFileVolumes(sourceDir, targetDir string) error {
	if sourceDir == "" || targetDir == "" {
		return errors.New("legacy: source and target file volumes are required")
	}
	source, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(source); resolveErr == nil {
		source = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}
	for _, pair := range [][2]string{{source, target}, {target, source}} {
		relative, relErr := filepath.Rel(pair[0], pair[1])
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("legacy: source and target file volumes must be separate nonoverlapping directories")
		}
	}
	return nil
}

func verifyFileBytes(ctx context.Context, root *os.Root, path string, file live.File) error {
	content, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("legacy: open file %s: %w", file.ID, err)
	}
	defer func() { _ = content.Close() }()
	info, err := content.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != file.Size {
		return fmt.Errorf("legacy: file %s size or type mismatch", file.ID)
	}
	hash := sha256.New()
	reader := contextFileReader{ctx: ctx, reader: content}
	read, err := io.Copy(hash, io.LimitReader(reader, file.Size+1))
	if err != nil {
		return err
	}
	if read != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("legacy: file %s checksum mismatch", file.ID)
	}
	return nil
}

type contextFileReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextFileReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func checkFileAssets(ctx context.Context, targetDir string, files []sourceFile, report *Report) error {
	root, err := os.OpenRoot(targetDir)
	if err != nil {
		return fmt.Errorf("legacy: open target file volume: %w", err)
	}
	defer func() { _ = root.Close() }()
	for _, source := range files {
		metadata, err := root.Open(source.ID + ".json")
		if err != nil {
			return fmt.Errorf("legacy: missing migrated file %s: %w", source.ID, err)
		}
		var target live.File
		decodeErr := json.NewDecoder(io.LimitReader(metadata, 64<<10)).Decode(&target)
		closeErr := metadata.Close()
		if decodeErr != nil {
			return decodeErr
		}
		if closeErr != nil {
			return closeErr
		}
		want := source.File
		if target.ID != want.ID || target.OwnerID != want.OwnerID || target.Filename != want.Filename || target.Purpose != want.Purpose ||
			target.MIMEType != want.MIMEType || target.Size != want.Size || target.SHA256 != want.SHA256 || !target.CreatedAt.Equal(want.CreatedAt) || !target.LastUsedAt.Equal(want.LastUsedAt) {
			return fmt.Errorf("legacy: file %s metadata differs from source", source.ID)
		}
		if err = verifyFileBytes(ctx, root, source.ID+".bin", target); err != nil {
			return err
		}
		report.Counts["check:gateway_user_files"]++
	}
	return nil
}

func validateFileCoverage(ctx context.Context, tx pgx.Tx, sources map[string]string, report *Report) error {
	files, err := loadSourceFiles(ctx, tx, sources, report)
	if err != nil || len(files) == 0 {
		return err
	}
	if err = checkFileAssets(ctx, os.Getenv("V3_FILES_DIR"), files, report); err != nil {
		report.Issues = append(report.Issues, Issue{"gateway_user_files", 0, "file_assets_not_migrated", "run migrate files with source and target volumes before database application: " + err.Error()})
	}
	return nil
}
