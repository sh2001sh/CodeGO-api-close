package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

type sourceFile struct {
	live.File
	UserID      int64      `json:"user_id"`
	StoragePath string     `json:"storage_path"`
	LastUsed    *time.Time `json:"last_used_at"`
}

var importedFileID = regexp.MustCompile(`^file-codego-[a-f0-9]{32}$`)
var importedFileDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func loadSourceFiles(ctx context.Context, tx pgx.Tx, sources map[string]string, report *Report) ([]sourceFile, error) {
	users := map[int64]bool{}
	if err := walkHistory(ctx, tx, sources["users"], func(raw json.RawMessage) error {
		var row struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		users[row.ID] = true
		return nil
	}); err != nil {
		return nil, err
	}
	days := int64(30)
	if raw := os.Getenv("FILE_STORAGE_RETENTION_DAYS"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 || value > 36500 {
			return nil, fmt.Errorf("legacy: invalid file retention days")
		}
		days = value
	}
	var files []sourceFile
	ids := map[string]bool{}
	err := walkHistory(ctx, tx, sources["gateway_user_files"], func(raw json.RawMessage) error {
		var file sourceFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return fmt.Errorf("legacy: invalid file metadata")
		}
		file.OwnerID = file.UserID
		file.LastUsedAt = file.CreatedAt
		if file.LastUsed != nil {
			file.LastUsedAt = *file.LastUsed
		}
		if !importedFileID.MatchString(file.ID) || !importedFileDigest.MatchString(file.SHA256) || !users[file.UserID] ||
			file.UserID <= 0 || file.Size < 0 || file.Size == math.MaxInt64 || file.CreatedAt.IsZero() || file.StoragePath == "" || ids[file.ID] || !validSourceFileLabels(file.File) {
			report.Issues = append(report.Issues, Issue{"gateway_user_files", file.UserID, "invalid_file_metadata", "file identity, owner, digest, size, timestamp or source path is invalid"})
			return nil
		}
		ids[file.ID] = true
		if days > 0 {
			expires := file.LastUsedAt.Add(time.Duration(days) * 24 * time.Hour)
			file.ExpiresAt = &expires
			if !expires.After(time.Now()) {
				report.Counts["excluded_expired.gateway_user_files"]++
				return nil
			}
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}
	report.Counts["gateway_user_files"] = int64(len(files))
	if table := sources["gateway_upstream_file_mappings"]; table != "" {
		var count int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return nil, err
		}
		// Local IDs are retained. Hashed v2 credential/base fingerprints are
		// intentionally rebuilt by the live attachment upload repository.
		report.Counts["rebuilt_lazily.gateway_upstream_file_mappings"] = count
	}
	return files, nil
}

func validSourceFileLabels(file live.File) bool {
	if file.Filename == "" || file.Filename == "." || file.Filename == ".." || len(file.Filename) > 1024 || strings.ContainsAny(file.Filename, "\\/\x00\r\n") || filepath.Base(file.Filename) != file.Filename {
		return false
	}
	if file.Purpose == "" || len(file.Purpose) > 256 || strings.ContainsAny(file.Purpose, "\x00\r\n") || len(file.MIMEType) > 1024 {
		return false
	}
	_, _, err := mime.ParseMediaType(file.MIMEType)
	return err == nil
}
