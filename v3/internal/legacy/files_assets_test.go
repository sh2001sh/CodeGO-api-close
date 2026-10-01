package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

func TestFileVolumeContainmentAndChecksums(t *testing.T) {
	directory := t.TempDir()
	other := t.TempDir()
	for _, pair := range [][2]string{{directory, directory}, {directory, filepath.Join(directory, "target")}, {filepath.Join(directory, "source"), directory}} {
		if err := independentFileVolumes(pair[0], pair[1]); err == nil {
			t.Fatal("overlapping volumes accepted")
		}
	}
	if err := independentFileVolumes(directory, other); err != nil {
		t.Fatal(err)
	}
	content := []byte("old retained content")
	if err := os.WriteFile(filepath.Join(directory, "file"), content, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	digest := sha256.Sum256(content)
	file := live.File{ID: "file-codego-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:])}
	if err = verifyFileBytes(context.Background(), root, "file", file); err != nil {
		t.Fatal(err)
	}
	file.SHA256 = string(make([]byte, 64))
	if err = verifyFileBytes(context.Background(), root, "file", file); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	file.SHA256 = hex.EncodeToString(digest[:])
	file.Size++
	if err = verifyFileBytes(context.Background(), root, "file", file); err == nil {
		t.Fatal("size mismatch accepted")
	}
	if err = verifyFileBytes(context.Background(), root, "../outside", file); err == nil {
		t.Fatal("path escape accepted")
	}
	file.Size--
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = verifyFileBytes(ctx, root, "file", file); err == nil {
		t.Fatal("cancelled checksum still ran")
	}
}
