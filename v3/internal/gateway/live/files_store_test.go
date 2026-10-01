package live

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFilesDiskPersistsAcrossRestartAndDeduplicatesOnlyPerOwner(t *testing.T) {
	store := filesTestStore(t)
	directory := store.root.Name()
	file := filesTestCreate(t, store, 11, []byte{0, 255, 1, 128})
	duplicate := filesTestCreate(t, store, 11, []byte{0, 255, 1, 128})
	if duplicate.ID != file.ID {
		t.Fatal("same owner content was not deduplicated")
	}
	foreign := filesTestCreate(t, store, 22, []byte{0, 255, 1, 128})
	if foreign.ID == file.ID {
		t.Fatal("cross-owner file was deduplicated")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewDiskFileStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	got, content, err := restarted.Open(context.Background(), 11, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(content)
	_ = content.Close()
	if err != nil || got.ID != file.ID || !bytes.Equal(raw, []byte{0, 255, 1, 128}) {
		t.Fatalf("restart content: %+v %x %v", got, raw, err)
	}
	items, _, err := restarted.List(context.Background(), 11, 20, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("restart list=%+v err=%v", items, err)
	}
}

func TestFilesDiskRejectsTraversal(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("owned"))
	for _, id := range []string{"../secret", file.ID + "/../../secret", file.ID + "\\..\\secret", file.ID + "%2f..", strings.ToUpper(file.ID), "file-codego-" + strings.Repeat("g", 32)} {
		if _, err := store.Get(context.Background(), 11, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("get %q: %v", id, err)
		}
		if _, _, err := store.Open(context.Background(), 11, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("open %q: %v", id, err)
		}
		if err := store.Delete(context.Background(), 11, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("delete %q: %v", id, err)
		}
	}
}

func TestFilesDiskRejectsExternalSymlink(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("owned"))
	out := filepath.Join(t.TempDir(), "secret.bin")
	if err := os.WriteFile(out, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.root.Name(), file.ID+".bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(store.root.Name(), file.ID+".bin")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, content, err := store.Open(context.Background(), 11, file.ID); err == nil {
		_ = content.Close()
		t.Fatal("external symlink was followed")
	}
	if raw, err := os.ReadFile(out); err != nil || string(raw) != "owned" {
		t.Fatalf("external file changed: %s %v", raw, err)
	}
}

func TestFilesDiskStorageLimitCancellationAndFailedReads(t *testing.T) {
	store := filesTestStore(t)
	store.userLimit = 4
	file := filesTestCreate(t, store, 11, []byte("abc"))
	if _, err := store.Create(context.Background(), 11, "x.bin", "", "", strings.NewReader("de"), 10); !errors.Is(err, ErrFileStorageFull) {
		t.Fatalf("limit err=%v", err)
	}
	if duplicate := filesTestCreate(t, store, 11, []byte("abc")); duplicate.ID != file.ID {
		t.Fatal("dedup at quota failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Create(ctx, 11, "x.bin", "", "", strings.NewReader("f"), 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	if _, err := store.Create(context.Background(), 11, "x.bin", "", "", filesTestFailReader{}, 10); err == nil {
		t.Fatal("failed reader accepted")
	}
	entries, err := os.ReadDir(store.root.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("failed uploads left temporary files: %+v", entries)
	}
}

func TestFilesDiskPaginationAndSlidingExpiration(t *testing.T) {
	store := filesTestStore(t)
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	first := filesTestCreate(t, store, 11, []byte("first"))
	now = now.Add(time.Second)
	second := filesTestCreate(t, store, 11, []byte("second"))
	now = now.Add(time.Second)
	third := filesTestCreate(t, store, 11, []byte("third"))
	foreign := filesTestCreate(t, store, 22, []byte("other"))
	items, more, err := store.List(context.Background(), 11, 2, "")
	if err != nil || !more || len(items) != 2 || items[0].ID != third.ID || items[1].ID != second.ID {
		t.Fatalf("first page=%+v more=%v err=%v", items, more, err)
	}
	items, more, err = store.List(context.Background(), 11, 2, second.ID)
	if err != nil || more || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("second page=%+v more=%v err=%v", items, more, err)
	}
	if _, _, err := store.List(context.Background(), 11, 2, foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign cursor err=%v", err)
	}
	now = now.Add(29 * 24 * time.Hour)
	_, content, err := store.Open(context.Background(), 11, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = content.Close()
	now = now.Add(2 * 24 * time.Hour)
	if _, err := store.Get(context.Background(), 11, first.ID); err != nil {
		t.Fatalf("refreshed file expired: %v", err)
	}
	if _, err := store.Get(context.Background(), 11, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unused file did not expire: %v", err)
	}
	items, _, err = store.List(context.Background(), 11, 20, "")
	if err != nil || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("expired list=%+v err=%v", items, err)
	}
	if _, err := os.Stat(filepath.Join(store.root.Name(), second.ID+".bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired content was not cleaned: %v", err)
	}
}

func TestFilesCorruptPersistenceFailsClosed(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("data"))
	if err := os.WriteFile(filepath.Join(store.root.Name(), file.ID+".json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := filesTestMux(t, store, 64, nil)
	w := filesTestRequest(mux, "GET", "/v1/files/"+file.ID, "owner", nil, "")
	if w.Code != 500 || strings.Contains(w.Body.String(), store.root.Name()) {
		t.Fatalf("corrupt metadata status=%d body=%s", w.Code, w.Body)
	}
	if _, _, err := store.Open(context.Background(), 11, file.ID); err == nil {
		t.Fatal("corrupt metadata content accessible")
	}
}

func TestFilesConcurrentDedupAndOwnerScoping(t *testing.T) {
	store := filesTestStore(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file, err := store.Create(context.Background(), 11, "x.bin", "", "", strings.NewReader("same bytes"), 32)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			ids[file.ID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ids) != 1 {
		t.Fatalf("concurrent duplicate IDs=%v", ids)
	}
	items, _, err := store.List(context.Background(), 11, 20, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("concurrent files=%+v err=%v", items, err)
	}
}
