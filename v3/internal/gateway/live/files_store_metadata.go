package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func (s *DiskFileStore) expiry(file *File) {
	file.ExpiresAt = nil
	if s.retention > 0 {
		expires := file.LastUsedAt.Add(s.retention)
		file.ExpiresAt = &expires
	}
}

func (s *DiskFileStore) writeMetadataLocked(file File) error {
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	suffix, err := randomFileSuffix()
	if err != nil {
		return err
	}
	name := ".metadata-" + suffix
	temp, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = s.root.Remove(name) }()
	_, writeErr := temp.Write(raw)
	if writeErr == nil {
		writeErr = temp.Sync()
	}
	closeErr := temp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return s.root.Rename(name, file.ID+".json")
}

func (s *DiskFileStore) metadataLocked(id string) (File, error) {
	if !validFileID(id) {
		return File{}, ErrNotFound
	}
	metadata, err := s.root.Open(id + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	defer func() { _ = metadata.Close() }()
	var file File
	if err := json.NewDecoder(io.LimitReader(metadata, 64<<10)).Decode(&file); err != nil {
		return File{}, fmt.Errorf("live: corrupt file metadata: %w", err)
	}
	if file.ID != id || file.OwnerID <= 0 || file.Size < 0 || file.CreatedAt.IsZero() || file.LastUsedAt.IsZero() {
		return File{}, errors.New("live: invalid persisted file metadata")
	}
	s.expiry(&file)
	if file.ExpiresAt != nil && !file.ExpiresAt.After(s.now()) {
		return file, ErrNotFound
	}
	info, err := s.root.Stat(id + ".bin")
	if errors.Is(err, os.ErrNotExist) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() || info.Size() != file.Size {
		return File{}, errors.New("live: invalid persisted file content")
	}
	return file, nil
}

func (s *DiskFileStore) recordsLocked(ctx context.Context) ([]File, error) {
	dir, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	items := make([]File, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validFileID(id) {
			continue
		}
		file, err := s.metadataLocked(id)
		if errors.Is(err, ErrNotFound) {
			if file.ID == id && file.ExpiresAt != nil && !file.ExpiresAt.After(s.now()) {
				if err := s.root.Remove(id + ".bin"); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("live: remove expired content: %w", err)
				}
				if err := s.root.Remove(id + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("live: remove expired metadata: %w", err)
				}
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, file)
	}
	return items, nil
}
