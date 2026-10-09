package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOnlineFlagsKeepOfflineAssertionExplicit(t *testing.T) {
	for _, command := range []string{"online-prepare", "online-copy", "online-sync", "online-verify", "online-seal", "online-unseal", "online-finalize", "online-backup", "online-delta", "online-restore-delta"} {
		for _, apply := range []bool{false, true} {
			for _, offline := range []bool{false, true} {
				args := []string{command}
				if apply {
					args = append(args, "-apply")
				}
				if offline {
					args = append(args, "-offline")
				}
				got, gotApply, err := parseMigrateArgs(args)
				wantValid := apply && !offline
				if command == "online-prepare" {
					wantValid = !offline
				}
				if command == "online-seal" || command == "online-finalize" {
					wantValid = apply && offline
				}
				if (err == nil) != wantValid || (err == nil && (got != command || gotApply != apply)) {
					t.Fatalf("%v: command=%q apply=%t err=%v valid=%t", args, got, gotApply, err, wantValid)
				}
			}
		}
	}
	for _, args := range [][]string{
		{"online-skip", "-apply"}, {"online-copy", "-apply", "extra"},
		{"online-finalize", "-apply", "-offline", "-ignore"},
		{"import", "-apply"}, {"files", "-apply"}, {"background", "-apply"},
	} {
		if _, _, err := parseMigrateArgs(args); err == nil {
			t.Fatalf("unsafe or unsupported arguments accepted: %v", args)
		}
	}
	if _, apply, err := parseMigrateArgs([]string{"import", "-apply", "-offline"}); err != nil || !apply {
		t.Fatalf("existing offline import contract changed: %v", err)
	}
}

func TestOnlineInputValidationPrecedesAnyConnection(t *testing.T) {
	for _, envName := range []string{"V3_SOURCE_PG_DSN", "V3_PG_DSN", "V3_ONLINE_SOURCE_ADMIN_PG_DSN", "V3_ONLINE_RESTORE_PG_DSN"} {
		t.Setenv(envName, "postgres://private-user:private-password@127.0.0.1:1/neondb")
	}
	for _, runID := range []string{"", "short", strings.Repeat("x", 65), "run'; DELETE FROM users;--"} {
		t.Setenv("V3_ONLINE_MIGRATION_ID", runID)
		var output bytes.Buffer
		err := run(context.Background(), []string{"online-copy", "-apply"}, &output)
		if err == nil || !strings.Contains(err.Error(), "V3_ONLINE_MIGRATION_ID") || output.Len() != 0 || strings.Contains(err.Error(), "private-password") {
			t.Fatalf("run ID validation did not fail before connecting: %v %s", err, output.String())
		}
	}
	t.Setenv("V3_ONLINE_MIGRATION_ID", "migration-cli-test-20261009")
	t.Setenv("V3_ONLINE_RESTORE_DATABASE", "neondb")
	if err := run(context.Background(), []string{"online-restore-delta", "-apply"}, io.Discard); err == nil || !strings.Contains(err.Error(), "isolated") {
		t.Fatalf("production restore database was not rejected: %v", err)
	}
	path := filepath.Join(t.TempDir(), "delta.jsonl")
	if err := os.WriteFile(path, []byte("not-read-before-database-validation"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("V3_ONLINE_DELTA_PATH", path)
	t.Setenv("V3_ONLINE_RESTORE_DATABASE", "online_restore_test")
	if err := run(context.Background(), []string{"online-restore-delta", "-apply"}, io.Discard); err == nil || !strings.Contains(err.Error(), "exact isolated restore database") || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("restore DSN mismatch was not rejected before connecting: %v", err)
	}
	t.Setenv("V3_ONLINE_RESTORE_PG_DSN", "invalid-private-user-private-password")
	if err := run(context.Background(), []string{"online-restore-delta", "-apply"}, io.Discard); err == nil || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("malformed restore DSN accepted or leaked: %v", err)
	}
}

func TestOnlineDeltaPublicationIsPrivateAtomicAndDoesNotOverwrite(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "delta.jsonl")
	sentinel := errors.New("interrupted export")
	err := writeOnlineDeltaFile(path, func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("export failure lost: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial archive survived: %v %v", entries, err)
	}
	if err := writeOnlineDeltaFile(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "complete")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("delta permissions=%v", info.Mode().Perm())
	}
	err = writeOnlineDeltaFile(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "replacement")
		return err
	})
	contents, readErr := os.ReadFile(path)
	if err == nil || readErr != nil || string(contents) != "complete" {
		t.Fatalf("existing archive overwritten: %v %v %q", err, readErr, contents)
	}
	entries, err = os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("private temporary file leaked: %v %v", entries, err)
	}
}

func TestOnlineDeltaPublicationRejectsConcurrentDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delta.jsonl")
	err := writeOnlineDeltaFile(path, func(w io.Writer) error {
		if err := os.WriteFile(path, []byte("concurrent archive"), 0600); err != nil {
			return err
		}
		_, err := io.WriteString(w, "new archive")
		return err
	})
	contents, readErr := os.ReadFile(path)
	if err == nil || readErr != nil || string(contents) != "concurrent archive" {
		t.Fatalf("concurrent destination overwritten: %v %v %q", err, readErr, contents)
	}
}

func TestOnlineArchivePathsRequireDedicatedAbsoluteDestination(t *testing.T) {
	t.Setenv("V3_ONLINE_DELTA_PATH", "relative.jsonl")
	if _, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", false); err == nil {
		t.Fatal("relative path accepted")
	}
	path := filepath.Join(t.TempDir(), "delta.jsonl")
	t.Setenv("V3_ONLINE_DELTA_PATH", path)
	if _, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", true); err == nil {
		t.Fatal("missing input accepted")
	}
	if _, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", false); err != nil {
		t.Fatalf("new absolute destination rejected: %v", err)
	}
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", false); err == nil {
		t.Fatal("existing output accepted")
	}
	if _, err := onlineArchivePath("V3_ONLINE_DELTA_PATH", true); err != nil {
		t.Fatalf("existing regular input rejected: %v", err)
	}
}
