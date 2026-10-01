package live

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func filesImportMetadata(raw []byte) File {
	hash := sha256.Sum256(raw)
	created := time.Now().UTC().Add(-45 * time.Minute).Truncate(time.Microsecond)
	return File{ID: localFilePrefix + strings.Repeat("a", 32), OwnerID: 11, Filename: "旧文件.bin", Purpose: "user_data", MIMEType: "application/octet-stream",
		Size: int64(len(raw)), SHA256: hex.EncodeToString(hash[:]), CreatedAt: created, LastUsedAt: created.Add(15 * time.Minute)}
}

type filesImportCancelReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r filesImportCancelReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.cancel()
	return n, err
}

func TestFilesImportRetainsExactIdentityMetadataAndNativeAPIs(t *testing.T) {
	store := filesTestStore(t)
	raw := []byte{0, 255, 13, 10, 128, 34}
	want := filesImportMetadata(raw)
	store.userLimit = 0 // Existing source assets must survive a lower current quota.
	for range 2 {
		got, err := store.ImportFile(context.Background(), want, bytes.NewReader(raw))
		store.expiry(&want)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("import changed metadata: got=%+v want=%+v error=%v", got, want, err)
		}
	}
	duplicate := want
	duplicate.ID = localFilePrefix + strings.Repeat("b", 32)
	if got, err := store.ImportFile(context.Background(), duplicate, bytes.NewReader(raw)); err != nil || got.ID != duplicate.ID {
		t.Fatalf("source IDs were deduplicated: %+v %v", got, err)
	}
	directory := store.root.Name()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewDiskFileStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	got, content, err := restarted.Open(context.Background(), want.OwnerID, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(content)
	closeErr := content.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, raw) || !reflect.DeepEqual(got, want) {
		t.Fatalf("restart changed imported metadata/bytes: %+v %x %v %v", got, actual, readErr, closeErr)
	}
	mux := filesTestMux(t, restarted, 1024, nil)
	for _, key := range []string{"owner", "other"} {
		response := filesTestRequest(mux, http.MethodGet, "/v1/files/"+want.ID+"/content", key, nil, "")
		if key == "owner" && (response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), raw)) || key == "other" && response.Code != http.StatusNotFound {
			t.Fatalf("imported-file content authorization key=%s status=%d", key, response.Code)
		}
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"id":"file-imported-upstream"}`)
	}))
	defer up.Close()
	h := attachmentsHandler(t, restarted, &attachmentsRepository{})
	body, err := h.PrepareFileReferences(context.Background(), attachmentsRequest(want.ID, gateway.ProtocolResponses), attachmentsTarget(up.URL))
	if err != nil || gjson.GetBytes(body, "input.0.file_id").Str != "file-imported-upstream" {
		t.Fatalf("old local ID did not forward natively: %s %v", body, err)
	}
}

func TestFilesImportRejectsInvalidMetadataWithoutPublishing(t *testing.T) {
	store := filesTestStore(t)
	raw := []byte("retained")
	want := filesImportMetadata(raw)
	for _, mutate := range []func(*File){
		func(f *File) { f.ID = "../escape" },
		func(f *File) { f.OwnerID = 0 },
		func(f *File) { f.SHA256 = strings.Repeat("f", 63) },
		func(f *File) { f.SHA256 = strings.ToUpper(f.SHA256) },
		func(f *File) { f.Size = -1 },
		func(f *File) { f.Size = math.MaxInt64 },
		func(f *File) { f.Filename = "../escape" },
		func(f *File) { f.Filename = "bad\r\nname" },
		func(f *File) { f.Filename = strings.Repeat("x", 1025) },
		func(f *File) { f.Purpose = "bad\nfield" },
		func(f *File) { f.MIMEType = "invalid/type; broken" },
		func(f *File) { f.MIMEType = "text/plain; name=" + strings.Repeat("x", 1024) },
		func(f *File) { f.CreatedAt = time.Time{} },
		func(f *File) { f.LastUsedAt = time.Time{} },
	} {
		invalid := want
		mutate(&invalid)
		if _, err := store.ImportFile(context.Background(), invalid, bytes.NewReader(raw)); !errors.Is(err, ErrInvalidFileUpload) {
			t.Fatalf("invalid metadata accepted: %+v %v", invalid, err)
		}
	}
	entries, err := os.ReadDir(store.root.Name())
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid import published artifacts: %v %v", entries, err)
	}
}

func TestFilesImportVerifiesSourceAndClearsFailedTemporaryWrites(t *testing.T) {
	store := filesTestStore(t)
	raw := []byte("retained")
	want := filesImportMetadata(raw)
	for _, source := range []io.Reader{nil, bytes.NewReader(raw[:len(raw)-1]), strings.NewReader("retained+"), strings.NewReader("tampered"), iotest.ErrReader(errors.New("source failed"))} {
		if _, err := store.ImportFile(context.Background(), want, source); err == nil {
			t.Fatal("invalid source was published")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ImportFile(ctx, want, bytes.NewReader(raw)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if _, err := store.ImportFile(ctx, want, filesImportCancelReader{bytes.NewReader(raw), cancel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during source read: %v", err)
	}
	entries, err := os.ReadDir(store.root.Name())
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed imports left artifacts: %v %v", entries, err)
	}
}

func TestFilesImportRefusesTargetConflictsAndTampering(t *testing.T) {
	store := filesTestStore(t)
	raw := []byte("retained")
	want := filesImportMetadata(raw)
	if _, err := store.ImportFile(context.Background(), want, bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(store.root.Name(), want.ID+".json")
	before, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*File){
		func(f *File) { f.OwnerID++ },
		func(f *File) { f.Filename = "different.bin" },
		func(f *File) { f.Purpose = "vision" },
		func(f *File) { f.MIMEType = "text/plain" },
		func(f *File) { f.CreatedAt = f.CreatedAt.Add(time.Second) },
		func(f *File) { f.LastUsedAt = f.LastUsedAt.Add(time.Second) },
	} {
		conflict := want
		mutate(&conflict)
		if _, err := store.ImportFile(context.Background(), conflict, bytes.NewReader(raw)); !errors.Is(err, ErrFileImportConflict) {
			t.Fatalf("metadata conflict overwritten: %+v %v", conflict, err)
		}
	}
	contentPath := filepath.Join(store.root.Name(), want.ID+".bin")
	corrupt := []byte("tampered")
	if err := os.WriteFile(contentPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportFile(context.Background(), want, bytes.NewReader(raw)); !errors.Is(err, ErrFileImportConflict) {
		t.Fatalf("tampered destination silently repaired: %v", err)
	}
	after, err := os.ReadFile(metadataPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("conflict changed existing metadata")
	}
	actual, err := os.ReadFile(contentPath)
	if err != nil || !bytes.Equal(actual, corrupt) {
		t.Fatal("conflict changed existing bytes")
	}
}

func TestFilesImportResumesExactUnindexedBytesAndCoalescesConcurrentImport(t *testing.T) {
	store := filesTestStore(t)
	raw := []byte("retained")
	want := filesImportMetadata(raw)
	if err := os.WriteFile(filepath.Join(store.root.Name(), want.ID+".bin"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := store.ImportFile(context.Background(), want, bytes.NewReader(raw)); err != nil || got.ID != want.ID {
				t.Errorf("concurrent offline import: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	entries, err := os.ReadDir(store.root.Name())
	if err != nil || len(entries) != 2 {
		t.Fatalf("import left duplicates/temp artifacts: %v %v", entries, err)
	}
}

func TestFilesImportEmptyContentAndUnindexedConflict(t *testing.T) {
	store := filesTestStore(t)
	empty := filesImportMetadata(nil)
	for range 2 {
		if got, err := store.ImportFile(context.Background(), empty, bytes.NewReader(nil)); err != nil || got.Size != 0 || got.ID != empty.ID {
			t.Fatalf("zero-byte source file: %+v %v", got, err)
		}
	}
	raw := []byte("retained")
	want := filesImportMetadata(raw)
	want.ID = localFilePrefix + strings.Repeat("c", 32)
	path := filepath.Join(store.root.Name(), want.ID+".bin")
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportFile(context.Background(), want, bytes.NewReader(raw)); !errors.Is(err, ErrFileImportConflict) {
		t.Fatalf("conflicting unindexed binary replaced: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "tampered" {
		t.Fatal("unindexed content changed")
	}
	if _, err := os.Stat(filepath.Join(store.root.Name(), want.ID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified unindexed binary gained metadata: %v", err)
	}
}
