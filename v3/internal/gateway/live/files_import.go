package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

var ErrFileImportConflict = errors.New("live: imported file conflicts with target")

// ImportFile is an offline, single-writer migration operation. It preserves
// source identity and metadata, without quota or digest deduplication changing
// existing IDs. Runtime upload callers must use Create instead.
func (s *DiskFileStore) ImportFile(ctx context.Context, file File, source io.Reader) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if source == nil || !validImportedFile(file) {
		return File{}, ErrInvalidFileUpload
	}
	tempName, err := s.writeImportTemp(ctx, file, source)
	if tempName != "" {
		defer func() { _ = s.root.Remove(tempName) }()
	}
	if err != nil {
		return File{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	s.expiry(&file)
	return s.commitImportLocked(ctx, tempName, file)
}

// writeImportTemp streams source into a new temp file, verifying its size and checksum match
// file's declared metadata exactly (import never deduplicates or rewrites metadata, unlike
// Create). It returns the temp file's name; the caller is responsible for removing it.
func (s *DiskFileStore) writeImportTemp(ctx context.Context, file File, source io.Reader) (string, error) {
	suffix, err := randomFileSuffix()
	if err != nil {
		return "", err
	}
	tempName := ".import-" + suffix
	temp, err := s.root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	read, readErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(fileContextReader{ctx, source}, file.Size+1))
	if readErr == nil && (read != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256) {
		readErr = fmt.Errorf("%w: imported content size or checksum differs", ErrInvalidFileUpload)
	}
	if readErr == nil {
		readErr = temp.Sync()
	}
	closeErr := temp.Close()
	if err := errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return tempName, err
	}
	return tempName, nil
}

// commitImportLocked links the verified temp content into place and writes metadata, tolerating
// a prior crash that already published either artifact: existing metadata matching file is
// treated as success (after re-verifying content), and an existing content file at the
// destination is re-verified rather than overwritten. Must be called with s.mu held.
func (s *DiskFileStore) commitImportLocked(ctx context.Context, tempName string, file File) (File, error) {
	exists, err := s.importMetadataExistsLocked(file)
	if err != nil {
		return File{}, err
	}
	if exists {
		if err := s.verifyImportedContentLocked(ctx, file); err != nil {
			return File{}, err
		}
		return file, nil
	}
	// A previous crash may have published verified bytes before the metadata.
	// Linking creates the destination atomically without overwriting any file.
	created := false
	if err := s.root.Link(tempName, file.ID+".bin"); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return File{}, err
		}
		if err := s.verifyImportedContentLocked(ctx, file); err != nil {
			return File{}, err
		}
	} else {
		created = true
	}
	if err := s.writeMetadataLocked(file); err != nil {
		if created {
			return File{}, errors.Join(err, s.root.Remove(file.ID+".bin"))
		}
		return File{}, err
	}
	return file, nil
}

func validImportedFile(file File) bool {
	if !validFileID(file.ID) || file.OwnerID <= 0 || file.Size < 0 || file.Size == math.MaxInt64 || file.CreatedAt.IsZero() || file.LastUsedAt.IsZero() {
		return false
	}
	digest, err := hex.DecodeString(file.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != file.SHA256 {
		return false
	}
	if file.Filename == "" || file.Filename == "." || file.Filename == ".." || len(file.Filename) > 1024 || strings.ContainsAny(file.Filename, "\\/\x00\r\n") || filepath.Base(file.Filename) != file.Filename {
		return false
	}
	if file.Purpose == "" || len(file.Purpose) > 256 || strings.ContainsAny(file.Purpose, "\x00\r\n") || len(file.MIMEType) > 1024 {
		return false
	}
	_, _, err = mime.ParseMediaType(file.MIMEType)
	return err == nil
}

func (s *DiskFileStore) importMetadataExistsLocked(want File) (bool, error) {
	content, err := s.root.Open(want.ID + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = content.Close() }()
	raw, err := io.ReadAll(io.LimitReader(content, (64<<10)+1))
	if err != nil {
		return false, err
	}
	var got File
	if len(raw) > 64<<10 || json.Unmarshal(raw, &got) != nil || got.ID != want.ID || got.OwnerID != want.OwnerID || got.Filename != want.Filename || got.Purpose != want.Purpose ||
		got.MIMEType != want.MIMEType || got.Size != want.Size || got.SHA256 != want.SHA256 || !got.CreatedAt.Equal(want.CreatedAt) || !got.LastUsedAt.Equal(want.LastUsedAt) {
		return false, ErrFileImportConflict
	}
	return true, nil
}

func (s *DiskFileStore) verifyImportedContentLocked(ctx context.Context, want File) error {
	content, err := s.root.Open(want.ID + ".bin")
	if err != nil {
		return errors.Join(ErrFileImportConflict, err)
	}
	defer func() { _ = content.Close() }()
	info, err := content.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != want.Size {
		return ErrFileImportConflict
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(fileContextReader{ctx, content}, want.Size+1))
	if err != nil {
		return err
	}
	if read != want.Size || hex.EncodeToString(hash.Sum(nil)) != want.SHA256 {
		return ErrFileImportConflict
	}
	return nil
}
