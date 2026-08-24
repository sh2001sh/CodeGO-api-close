package archivex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/stretchr/testify/require"
)

func TestFileSinkStoresValidatedBatchAndManifestIdempotently(t *testing.T) {
	root := t.TempDir()
	sink, err := NewFileSink(root)
	require.NoError(t, err)
	batch := Batch{
		Dataset:   "logs",
		Partition: time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
		ID:        "1-2",
		Records: []Record{
			{Type: "log", Data: map[string]any{"id": 1, "content": "first"}},
			{Type: "log", Data: map[string]any{"id": 2, "content": "second"}},
		},
	}
	require.NoError(t, sink.Store(context.Background(), batch))
	require.NoError(t, sink.Store(context.Background(), batch))

	directory := filepath.Join(root, "logs", "2026-08-19")
	dataPath := filepath.Join(directory, "1-2.jsonl.gz")
	manifestPath := filepath.Join(directory, "1-2.manifest.json")
	count, checksum, err := inspectArchive(dataPath, 2)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, checksum, sha256HexLength)

	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var manifest archiveManifest
	require.NoError(t, platformencoding.Unmarshal(manifestData, &manifest))
	require.Equal(t, 2, manifest.RecordCount)
	require.Equal(t, checksum, manifest.SHA256)
	require.Equal(t, "1-2.jsonl.gz", manifest.DataFile)
}

func TestFileSinkRejectsCorruptExistingBatch(t *testing.T) {
	root := t.TempDir()
	sink, err := NewFileSink(root)
	require.NoError(t, err)
	directory := filepath.Join(root, "logs", "2026-08-19")
	require.NoError(t, os.MkdirAll(directory, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "1-1.jsonl.gz"), []byte("not-gzip"), 0o600))

	err = sink.Store(context.Background(), Batch{
		Dataset:   "logs",
		Partition: time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
		ID:        "1-1",
		Records:   []Record{{Type: "log", Data: map[string]any{"id": 1}}},
	})
	require.ErrorContains(t, err, "archive compressor")
}

func TestFileSinkRejectsUnsafePaths(t *testing.T) {
	sink, err := NewFileSink(t.TempDir())
	require.NoError(t, err)
	err = sink.Store(context.Background(), Batch{
		Dataset:   "../logs",
		Partition: time.Now(),
		ID:        "unsafe",
		Records:   []Record{{Type: "log", Data: map[string]any{"id": 1}}},
	})
	require.ErrorContains(t, err, "safe path components")
}

const sha256HexLength = 64
