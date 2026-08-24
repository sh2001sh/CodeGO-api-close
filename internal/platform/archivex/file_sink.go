package archivex

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

const archiveSchemaVersion = 1

var archivePathPartPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

type Record struct {
	Type string
	Data any
}

type Batch struct {
	Dataset   string
	Partition time.Time
	ID        string
	Records   []Record
}

type Sink interface {
	Store(context.Context, Batch) error
}

type FileSink struct {
	root string
}

type archiveLine struct {
	SchemaVersion int             `json:"schema_version"`
	RecordType    string          `json:"record_type"`
	Data          json.RawMessage `json:"data"`
}

type archiveManifest struct {
	SchemaVersion int       `json:"schema_version"`
	Dataset       string    `json:"dataset"`
	BatchID       string    `json:"batch_id"`
	RecordCount   int       `json:"record_count"`
	SHA256        string    `json:"sha256"`
	DataFile      string    `json:"data_file"`
	CreatedAt     time.Time `json:"created_at"`
}

func NewFileSink(root string) (*FileSink, error) {
	if root == "" {
		return nil, fmt.Errorf("archive directory is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve archive directory: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create archive directory: %w", err)
	}
	if err := os.Chmod(absRoot, 0o750); err != nil {
		return nil, fmt.Errorf("secure archive directory: %w", err)
	}
	return &FileSink{root: absRoot}, nil
}

func (sink *FileSink) Store(ctx context.Context, batch Batch) error {
	if err := validateBatch(batch); err != nil {
		return err
	}
	directory := filepath.Join(sink.root, batch.Dataset, batch.Partition.UTC().Format(time.DateOnly))
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create archive partition: %w", err)
	}
	dataPath := filepath.Join(directory, batch.ID+".jsonl.gz")
	manifestPath := filepath.Join(directory, batch.ID+".manifest.json")
	if _, err := os.Stat(dataPath); err == nil {
		return sink.validateAndManifest(dataPath, manifestPath, batch)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect archive batch: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".archive-*.tmp")
	if err != nil {
		return fmt.Errorf("create archive temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := writeArchiveRecords(ctx, temporary, batch.Records); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync archive batch: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close archive batch: %w", err)
	}
	if _, _, err := inspectArchive(temporaryPath, len(batch.Records)); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, dataPath); err != nil {
		if _, statErr := os.Stat(dataPath); statErr != nil {
			return fmt.Errorf("publish archive batch: %w", err)
		}
	}
	return sink.validateAndManifest(dataPath, manifestPath, batch)
}

func validateBatch(batch Batch) error {
	if !archivePathPartPattern.MatchString(batch.Dataset) || !archivePathPartPattern.MatchString(batch.ID) {
		return fmt.Errorf("archive dataset and batch id must be safe path components")
	}
	if batch.Partition.IsZero() || len(batch.Records) == 0 {
		return fmt.Errorf("archive partition and records are required")
	}
	for _, record := range batch.Records {
		if record.Type == "" || record.Data == nil {
			return fmt.Errorf("archive record type and data are required")
		}
	}
	return nil
}

func writeArchiveRecords(ctx context.Context, output io.Writer, records []Record) error {
	compressed := gzip.NewWriter(output)
	compressed.Header.ModTime = time.Unix(0, 0).UTC()
	writer := bufio.NewWriterSize(compressed, 64*1024)
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := platformencoding.Marshal(record.Data)
		if err != nil {
			return fmt.Errorf("encode archive record data: %w", err)
		}
		line, err := platformencoding.Marshal(archiveLine{SchemaVersion: archiveSchemaVersion, RecordType: record.Type, Data: data})
		if err != nil {
			return fmt.Errorf("encode archive record: %w", err)
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write archive record: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush archive records: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("close archive compressor: %w", err)
	}
	return nil
}

func (sink *FileSink) validateAndManifest(dataPath, manifestPath string, batch Batch) error {
	count, checksum, err := inspectArchive(dataPath, len(batch.Records))
	if err != nil {
		return err
	}
	manifest := archiveManifest{
		SchemaVersion: archiveSchemaVersion,
		Dataset:       batch.Dataset,
		BatchID:       batch.ID,
		RecordCount:   count,
		SHA256:        checksum,
		DataFile:      filepath.Base(dataPath),
		CreatedAt:     time.Now().UTC(),
	}
	return writeManifestAtomically(manifestPath, manifest)
}

func inspectArchive(path string, expectedCount int) (int, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("open archive batch: %w", err)
	}
	hash := sha256.New()
	compressed, err := gzip.NewReader(io.TeeReader(file, hash))
	if err != nil {
		file.Close()
		return 0, "", fmt.Errorf("open archive compressor: %w", err)
	}
	scanner := bufio.NewScanner(compressed)
	scanner.Buffer(make([]byte, 64*1024), 256*1024*1024)
	count := 0
	for scanner.Scan() {
		var line archiveLine
		if err := platformencoding.Unmarshal(scanner.Bytes(), &line); err != nil {
			compressed.Close()
			file.Close()
			return 0, "", fmt.Errorf("validate archive record %d: %w", count+1, err)
		}
		if line.SchemaVersion != archiveSchemaVersion || line.RecordType == "" || len(line.Data) == 0 {
			compressed.Close()
			file.Close()
			return 0, "", fmt.Errorf("archive record %d has an invalid envelope", count+1)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		compressed.Close()
		file.Close()
		return 0, "", fmt.Errorf("scan archive batch: %w", err)
	}
	if err := compressed.Close(); err != nil {
		file.Close()
		return 0, "", fmt.Errorf("validate archive compressor: %w", err)
	}
	if _, err := io.Copy(hash, file); err != nil {
		file.Close()
		return 0, "", fmt.Errorf("hash archive batch: %w", err)
	}
	if err := file.Close(); err != nil {
		return 0, "", fmt.Errorf("close validated archive batch: %w", err)
	}
	if count != expectedCount {
		return count, "", fmt.Errorf("archive record count mismatch: got %d, want %d", count, expectedCount)
	}
	return count, hex.EncodeToString(hash.Sum(nil)), nil
}

func writeManifestAtomically(path string, manifest archiveManifest) error {
	data, err := platformencoding.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode archive manifest: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create archive manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return fmt.Errorf("secure archive manifest: %w", err)
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write archive manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync archive manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close archive manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish archive manifest: %w", err)
	}
	return nil
}
