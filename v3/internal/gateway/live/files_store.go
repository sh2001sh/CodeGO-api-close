package live

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const localFilePrefix = "file-codego-"

var ErrFileTooLarge = errors.New("live: file too large")

var ErrFileStorageFull = errors.New("live: file storage limit exceeded")

var ErrInvalidFileUpload = errors.New("live: invalid file upload")

// File is persisted metadata. HTTP responses deliberately omit ownership and paths.
type File struct {
	ID         string     `json:"id"`
	OwnerID    int64      `json:"owner_id"`
	Filename   string     `json:"filename"`
	Purpose    string     `json:"purpose"`
	MIMEType   string     `json:"mime_type"`
	Size       int64      `json:"size"`
	SHA256     string     `json:"sha256"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt time.Time  `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// FileStore scopes every ordinary operation by authenticated account. Delivery
// access is separate and must only follow successful signature verification.
type FileStore interface {
	Create(context.Context, int64, string, string, string, io.Reader, int64) (File, error)
	List(context.Context, int64, int, string) ([]File, bool, error)
	Get(context.Context, int64, string) (File, error)
	Open(context.Context, int64, string) (File, *os.File, error)
	OpenDelivery(context.Context, string) (File, *os.File, error)
	Delete(context.Context, int64, string) error
}

// DiskFileStore keeps content and metadata on a persistent volume, rather than
// retaining an in-memory index that disappears across container restarts.
type DiskFileStore struct {
	root      *os.Root
	mu        sync.Mutex
	retention time.Duration
	userLimit int64
	now       func() time.Time
}

func NewDiskFileStore(directory string) (*DiskFileStore, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("live: file storage directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, fmt.Errorf("live: create file storage: %w", err)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	days, err := fileEnvInt("FILE_STORAGE_RETENTION_DAYS", 30, true)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	limit, err := fileEnvInt("FILE_STORAGE_USER_LIMIT_MB", 1024, false)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if days > 36500 || limit > 1<<40 {
		_ = root.Close()
		return nil, errors.New("live: file storage configuration is too large")
	}
	return &DiskFileStore{root: root, retention: time.Duration(days) * 24 * time.Hour, userLimit: limit << 20, now: time.Now}, nil
}

func fileEnvInt(name string, fallback int64, allowZero bool) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || (!allowZero && value == 0) {
		return 0, fmt.Errorf("live: invalid %s", name)
	}
	return value, nil
}

func (s *DiskFileStore) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.root.Close() }

func validFileID(id string) bool {
	if len(id) != len(localFilePrefix)+32 || !strings.HasPrefix(id, localFilePrefix) {
		return false
	}
	for _, char := range id[len(localFilePrefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func randomFileSuffix() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

type fileContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r fileContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *DiskFileStore) Create(ctx context.Context, owner int64, filename, purpose, mimeType string, source io.Reader, maxBytes int64) (File, error) {
	if owner <= 0 || source == nil || maxBytes <= 0 || maxBytes == int64(^uint64(0)>>1) {
		return File{}, ErrInvalidFileUpload
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	filename, purpose, mimeType, err := validateFileUploadMetadata(filename, purpose, mimeType)
	if err != nil {
		return File{}, err
	}
	suffix, err := randomFileSuffix()
	if err != nil {
		return File{}, err
	}
	tempName := ".upload-" + suffix
	defer func() { _ = s.root.Remove(tempName) }()
	written, sum, err := s.writeUploadTemp(ctx, tempName, source, maxBytes)
	if err != nil {
		return File{}, err
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	file := File{ID: localFilePrefix + suffix, OwnerID: owner, Filename: filename, Purpose: purpose, MIMEType: mimeType, Size: written, SHA256: sum, CreatedAt: s.now().UTC(), LastUsedAt: s.now().UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitUploadLocked(ctx, tempName, file)
}

// validateFileUploadMetadata normalizes and validates the client-supplied filename, purpose and
// MIME type, filling in defaults for purpose/mimeType when empty.
func validateFileUploadMetadata(filename, purpose, mimeType string) (string, string, string, error) {
	filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "" || filename == "." || filename == ".." || len(filename) > 1024 || strings.ContainsAny(filename, "\x00\r\n") {
		return "", "", "", fmt.Errorf("%w: invalid filename", ErrInvalidFileUpload)
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = "user_data"
	}
	if len(purpose) > 256 {
		return "", "", "", fmt.Errorf("%w: invalid purpose", ErrInvalidFileUpload)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if len(mimeType) > 1024 {
		return "", "", "", fmt.Errorf("%w: content type is too long", ErrInvalidFileUpload)
	}
	if _, _, err := mime.ParseMediaType(mimeType); err != nil {
		return "", "", "", fmt.Errorf("%w: invalid content type", ErrInvalidFileUpload)
	}
	return filename, purpose, mimeType, nil
}

// writeUploadTemp streams source into a new temp file under tempName, hashing as it goes and
// enforcing maxBytes. The temp file is left in place (named tempName) on both success and
// failure; the caller is responsible for removing or renaming it.
func (s *DiskFileStore) writeUploadTemp(ctx context.Context, tempName string, source io.Reader, maxBytes int64) (written int64, sha256Hex string, err error) {
	temp, err := s.root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, "", err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(fileContextReader{ctx, source}, maxBytes+1))
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return 0, "", copyErr
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	if written > maxBytes {
		return 0, "", ErrFileTooLarge
	}
	return written, hex.EncodeToString(digest.Sum(nil)), nil
}

// commitUploadLocked deduplicates against the owner's existing files by content hash, enforces
// the owner's storage quota, and otherwise commits the temp file and metadata as the new File.
// Must be called with s.mu held.
func (s *DiskFileStore) commitUploadLocked(ctx context.Context, tempName string, file File) (File, error) {
	items, err := s.recordsLocked(ctx)
	if err != nil {
		return File{}, err
	}
	var used int64
	for _, item := range items {
		if item.OwnerID != file.OwnerID {
			continue
		}
		if item.SHA256 == file.SHA256 {
			return s.touchLocked(item)
		}
	}
	for _, item := range items {
		if item.OwnerID != file.OwnerID {
			continue
		}
		if item.Size > s.userLimit-used {
			return File{}, ErrFileStorageFull
		}
		used += item.Size
	}
	if file.Size > s.userLimit-used {
		return File{}, ErrFileStorageFull
	}
	if err := s.root.Rename(tempName, file.ID+".bin"); err != nil {
		return File{}, err
	}
	s.expiry(&file)
	if err := s.writeMetadataLocked(file); err != nil {
		return File{}, errors.Join(err, s.root.Remove(file.ID+".bin"))
	}
	return file, nil
}
