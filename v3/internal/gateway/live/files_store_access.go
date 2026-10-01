package live

import (
	"context"
	"os"
	"sort"
	"time"
)

func (s *DiskFileStore) Get(ctx context.Context, owner int64, id string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.metadataLocked(id)
	if err != nil {
		return File{}, err
	}
	if owner <= 0 || owner != file.OwnerID {
		return File{}, ErrNotFound
	}
	return file, nil
}

func (s *DiskFileStore) List(ctx context.Context, owner int64, limit int, after string) ([]File, bool, error) {
	if owner <= 0 {
		return nil, false, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.recordsLocked(ctx)
	if err != nil {
		return nil, false, err
	}
	owned := make([]File, 0, len(items))
	for _, file := range items {
		if file.OwnerID == owner {
			owned = append(owned, file)
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].CreatedAt.Equal(owned[j].CreatedAt) {
			return owned[i].ID > owned[j].ID
		}
		return owned[i].CreatedAt.After(owned[j].CreatedAt)
	})
	if after != "" {
		found := false
		for i, file := range owned {
			if file.ID == after {
				owned = owned[i+1:]
				found = true
				break
			}
		}
		if !found {
			return nil, false, ErrNotFound
		}
	}
	more := len(owned) > limit
	if more {
		owned = owned[:limit]
	}
	return owned, more, nil
}

func (s *DiskFileStore) touchLocked(file File) (File, error) {
	if s.now().Sub(file.LastUsedAt) < time.Hour {
		return file, nil
	}
	file.LastUsedAt = s.now().UTC()
	s.expiry(&file)
	if err := s.writeMetadataLocked(file); err != nil {
		return File{}, err
	}
	return file, nil
}

func (s *DiskFileStore) open(ctx context.Context, owner int64, id string, delivery bool) (File, *os.File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.metadataLocked(id)
	if err != nil {
		return File{}, nil, err
	}
	if !delivery && (owner <= 0 || file.OwnerID != owner) {
		return File{}, nil, ErrNotFound
	}
	file, err = s.touchLocked(file)
	if err != nil {
		return File{}, nil, err
	}
	content, err := s.root.Open(id + ".bin")
	if err != nil {
		return File{}, nil, err
	}
	return file, content, nil
}

func (s *DiskFileStore) Open(ctx context.Context, owner int64, id string) (File, *os.File, error) {
	return s.open(ctx, owner, id, false)
}

func (s *DiskFileStore) OpenDelivery(ctx context.Context, id string) (File, *os.File, error) {
	return s.open(ctx, 0, id, true)
}

func (s *DiskFileStore) Delete(ctx context.Context, owner int64, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.metadataLocked(id)
	if err != nil {
		return err
	}
	if owner <= 0 || file.OwnerID != owner {
		return ErrNotFound
	}
	if err := s.root.Remove(id + ".bin"); err != nil {
		return err
	}
	return s.root.Remove(id + ".json")
}
