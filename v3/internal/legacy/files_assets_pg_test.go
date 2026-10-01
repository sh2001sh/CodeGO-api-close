//go:build pgintegration

package legacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

func TestOfflineFileAssetsRetainIDsOwnershipAndExactBytes(t *testing.T) {
	source, target, crypto := importTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "30")
	sourceDir, targetDir := t.TempDir(), t.TempDir()
	t.Setenv("V3_FILES_DIR", targetDir)
	id := "file-codego-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	content := []byte("kept v2 file content")
	digest := sha256.Sum256(content)
	if err := os.WriteFile(filepath.Join(sourceDir, id), content, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	lastUse := created.Add(30 * time.Minute)
	_, err := source.Exec(ctx, `CREATE TABLE migration_source.gateway_user_files(id text,user_id bigint,sha256 text,purpose text,filename text,mime_type text,size bigint,storage_path text,created_at timestamptz,last_used_at timestamptz);
		CREATE TABLE migration_source.gateway_upstream_file_mappings(id bigint,local_file_id text);
		INSERT INTO migration_source.gateway_upstream_file_mappings VALUES(1,'file-codego-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(ctx, `INSERT INTO migration_source.gateway_user_files VALUES($1,7,$2,'user_data','kept.txt','text/plain',$3,$1,$4,$5)`, id, hex.EncodeToString(digest[:]), len(content), created, lastUse)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(readonlySource(t, source), target, crypto)
	preview, err := importer.ImportFiles(ctx, sourceDir, targetDir, false)
	if err != nil || preview.Applied || preview.Counts["gateway_user_files"] != 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("dry-run published assets")
	}
	for range 2 {
		report, copyErr := importer.ImportFiles(ctx, sourceDir, targetDir, true)
		if copyErr != nil || !report.Applied || report.Counts["check:gateway_user_files"] != 1 || report.Counts["rebuilt_lazily.gateway_upstream_file_mappings"] != 1 {
			t.Fatalf("asset apply=%+v err=%v", report, copyErr)
		}
	}
	store, err := live.NewDiskFileStore(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	metadata, file, err := store.Open(ctx, 7, id)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(content, actual) || !metadata.CreatedAt.Equal(created) || !metadata.LastUsedAt.Equal(lastUse) {
		t.Fatal("old ID bytes/time changed")
	}
	if _, err = store.Get(ctx, 8, id); err == nil {
		t.Fatal("old file owner bypass")
	}
	if report, previewErr := importer.Import(ctx, false); previewErr != nil || len(report.Issues) != 0 {
		t.Fatalf("DB preview rejects migrated assets=%+v err=%v", report, previewErr)
	}
	if err = os.WriteFile(filepath.Join(sourceDir, id), bytes.Repeat([]byte{'x'}, len(content)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = importer.ImportFiles(ctx, sourceDir, targetDir, true); err == nil {
		t.Fatal("corrupt source accepted")
	}
}
