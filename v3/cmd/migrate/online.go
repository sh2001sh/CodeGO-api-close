package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/legacy"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
)

func isOnlineCommand(command string) bool {
	switch command {
	case "online-prepare", "online-copy", "online-sync", "online-verify", "online-empty-schema-upgrade", "online-seal", "online-unseal", "online-finalize", "online-backup", "online-delta", "online-restore-delta":
		return true
	}
	return false
}

func validateOnlineFlags(command string, apply, offline bool) error {
	if command == "online-finalize" || command == "online-seal" {
		if !apply || !offline {
			return fmt.Errorf("%s requires -apply -offline after stopping all v2 writers", command)
		}
		return nil
	}
	if offline {
		return fmt.Errorf("%s does not accept -offline", command)
	}
	if command != "online-prepare" && !apply {
		return fmt.Errorf("%s requires -apply; only online-prepare supports preview", command)
	}
	return nil
}

var onlineRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var onlineRestoreDatabasePattern = regexp.MustCompile(`^online_restore_[A-Za-z0-9_]{1,48}$`)

func onlineRunID() (string, error) {
	runID := os.Getenv("V3_ONLINE_MIGRATION_ID")
	if !onlineRunIDPattern.MatchString(runID) {
		return "", errors.New("V3_ONLINE_MIGRATION_ID must contain 16-64 letters, digits, hyphens or underscores")
	}
	return runID, nil
}

// Do not attach connection errors: DSN parse diagnostics may contain secrets.
// The failure remains explicit while the CLI output contains no credentials.
func onlineConnect(ctx context.Context, envName, role string) (*pg.Pool, error) {
	if os.Getenv(envName) == "" {
		return nil, fmt.Errorf("%s is required for %s", envName, role)
	}
	pool, err := pg.Connect(ctx, pg.Config{DSN: os.Getenv(envName), MaxConns: 2})
	if err != nil {
		return nil, fmt.Errorf("%s postgres connection failed", role)
	}
	return pool, nil
}

func encodeOnlineReport(output io.Writer, report any, operationErr error) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}
	return operationErr
}

func runOnline(ctx context.Context, command string, apply bool, output io.Writer) error {
	archiveLedger, err := migrationLedgerArchive()
	if err != nil {
		return err
	}
	runID, err := onlineRunID()
	if err != nil {
		return err
	}
	if command == "online-restore-delta" {
		return runOnlineRestoreDelta(ctx, runID, output)
	}
	if command == "online-seal" || command == "online-unseal" {
		admin, err := onlineConnect(ctx, "V3_ONLINE_SOURCE_ADMIN_PG_DSN", "source capture administrator")
		if err != nil {
			return err
		}
		defer admin.Close()
		var report legacy.OnlineCaptureReport
		if command == "online-seal" {
			report, err = legacy.SealOnlineCapture(ctx, admin.Pool, runID)
		} else {
			report, err = legacy.UnsealOnlineCapture(ctx, admin.Pool, runID)
		}
		return encodeOnlineReport(output, report, err)
	}
	if command == "online-backup" || command == "online-delta" {
		return runOnlineArchive(ctx, command, runID, output)
	}
	upgrade := legacy.OnlineEmptySchemaUpgradeOptions{
		OnlineOptions:           legacy.OnlineOptions{RunID: runID},
		ExpectedTargetShape:     os.Getenv("V3_ONLINE_EXPECTED_TARGET_SHAPE"),
		ExpectedCaptureHash:     os.Getenv("V3_ONLINE_EXPECTED_CAPTURE_HASH"),
		ExpectedMigrationSHA256: os.Getenv("V3_ONLINE_EXPECTED_MIGRATION_SHA256"),
	}
	if command == "online-empty-schema-upgrade" {
		if err := legacy.ValidateOnlineEmptySchemaUpgradeOptions(upgrade); err != nil {
			return err
		}
	}
	crypto, err := catalog.NewAESGCMFromBase64(os.Getenv("V3_SECRET_KEY"))
	if err != nil {
		return errors.New("V3_SECRET_KEY must be a valid base64 AES encryption key")
	}
	// Validate every required input before opening any database connection.
	adminRequired := (command == "online-prepare" && apply) || command == "online-sync" || command == "online-finalize"
	for _, envName := range []string{"V3_SOURCE_PG_DSN", "V3_PG_DSN"} {
		if os.Getenv(envName) == "" {
			return fmt.Errorf("%s is required", envName)
		}
	}
	if adminRequired && os.Getenv("V3_ONLINE_SOURCE_ADMIN_PG_DSN") == "" {
		return errors.New("V3_ONLINE_SOURCE_ADMIN_PG_DSN is required for capture setup or acknowledgement")
	}
	source, err := onlineConnect(ctx, "V3_SOURCE_PG_DSN", "source reader")
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := onlineConnect(ctx, "V3_PG_DSN", "target")
	if err != nil {
		return err
	}
	defer target.Close()
	options := legacy.OnlineOptions{RunID: runID}
	if adminRequired {
		admin, err := onlineConnect(ctx, "V3_ONLINE_SOURCE_ADMIN_PG_DSN", "source capture administrator")
		if err != nil {
			return err
		}
		defer admin.Close()
		options.SourceAdmin = admin.Pool
	}
	secret := os.Getenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET")
	if secret == "" {
		secret = os.Getenv("LEGACY_CRYPTO_SECRET")
	}
	importer := legacy.NewImporter(source.Pool, target.Pool, crypto).WithSourceCryptoSecret(secret).WithLedgerHistoryArchive(archiveLedger)
	if command == "online-empty-schema-upgrade" {
		report, err := importer.UpgradeEmptyOnlineSchema(ctx, upgrade)
		return encodeOnlineReport(output, report, err)
	}
	if command == "online-finalize" {
		report, err := importer.FinalizeOnline(ctx, options)
		return encodeOnlineReport(output, report, err)
	}
	var report legacy.OnlineReport
	switch command {
	case "online-prepare":
		report, err = importer.PrepareOnline(ctx, options, apply)
	case "online-copy":
		report, err = importer.CopyOnline(ctx, options)
	case "online-sync":
		report, err = importer.SyncOnline(ctx, options)
	case "online-verify":
		report, err = importer.VerifyOnline(ctx, options)
	default:
		return errors.New("unsupported online command")
	}
	return encodeOnlineReport(output, report, err)
}

func onlineArchivePath(envName string, existing bool) (string, error) {
	path := os.Getenv(envName)
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s must be an absolute path on the designated backup disk", envName)
	}
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return "", fmt.Errorf("%s parent must exist and must not contain symlinks", envName)
	}
	info, err := os.Lstat(path)
	if existing {
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s must name an existing regular file", envName)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%s destination must not already exist", envName)
	}
	return path, nil
}

func runOnlineArchive(ctx context.Context, command, runID string, output io.Writer) error {
	pathEnv := "V3_ONLINE_BACKUP_PATH"
	if command == "online-delta" {
		pathEnv = "V3_ONLINE_DELTA_PATH"
	}
	path, err := onlineArchivePath(pathEnv, false)
	if err != nil {
		return err
	}
	source, err := onlineConnect(ctx, "V3_SOURCE_PG_DSN", "source reader")
	if err != nil {
		return err
	}
	defer source.Close()
	if command == "online-backup" {
		report, err := legacy.OnlineBaseBackup(ctx, source.Pool, runID, path)
		return encodeOnlineReport(output, report, err)
	}
	var report legacy.OnlineDeltaManifest
	err = writeOnlineDeltaFile(path, func(writer io.Writer) error {
		var exportErr error
		report, exportErr = legacy.ExportOnlineDelta(ctx, source.Pool, runID, writer)
		return exportErr
	})
	return encodeOnlineReport(output, report, err)
}

// A private temporary file is published with a hard link. Unlike Rename on
// Unix, Link cannot replace an existing destination, including in a race.
func writeOnlineDeltaFile(path string, write func(io.Writer) error) (resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".codego-online-delta-*")
	if err != nil {
		return errors.New("cannot create private delta temporary file")
	}
	closed := false
	defer func() {
		if !closed {
			if err := file.Close(); err != nil {
				resultErr = errors.Join(resultErr, errors.New("cannot close delta temporary file"))
			}
		}
		if err := os.Remove(file.Name()); err != nil {
			resultErr = errors.Join(resultErr, errors.New("cannot remove private delta temporary file"))
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return errors.New("cannot protect delta temporary file")
	}
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return errors.New("cannot flush delta archive")
	}
	if err := file.Close(); err != nil {
		closed = true
		return errors.New("cannot close delta archive")
	}
	closed = true
	if err := os.Link(file.Name(), path); err != nil {
		return errors.New("cannot publish delta archive without overwriting an existing file")
	}
	return nil
}

func runOnlineRestoreDelta(ctx context.Context, runID string, output io.Writer) (resultErr error) {
	database := os.Getenv("V3_ONLINE_RESTORE_DATABASE")
	if !onlineRestoreDatabasePattern.MatchString(database) {
		return errors.New("V3_ONLINE_RESTORE_DATABASE must be an isolated online_restore_ database name")
	}
	path, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", true)
	if err != nil {
		return err
	}
	// Parse before connecting, so an arbitrary production DSN cannot be probed.
	config, err := pgxpool.ParseConfig(os.Getenv("V3_ONLINE_RESTORE_PG_DSN"))
	if err != nil || os.Getenv("V3_ONLINE_RESTORE_PG_DSN") == "" || config.ConnConfig.Database != database {
		return errors.New("V3_ONLINE_RESTORE_PG_DSN must explicitly identify the exact isolated restore database")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open delta archive")
	}
	defer func() {
		if err := file.Close(); err != nil {
			resultErr = errors.Join(resultErr, errors.New("cannot close delta archive"))
		}
	}()
	target, err := onlineConnect(ctx, "V3_ONLINE_RESTORE_PG_DSN", "isolated restore")
	if err != nil {
		return err
	}
	defer target.Close()
	report, err := legacy.ApplyOnlineDelta(ctx, target.Pool, file, legacy.OnlineDeltaApplyOptions{
		RunID: runID, Database: database, SpoolDirectory: filepath.Dir(path),
	})
	return encodeOnlineReport(output, report, err)
}
