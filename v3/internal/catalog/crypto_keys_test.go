package catalog

import (
	"bytes"
	"testing"
)

func TestDerivedKeysAreStableSeparatedAndDetached(t *testing.T) {
	master := bytes.Repeat([]byte{7}, 32)
	a, err := NewAESGCM(master)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAESGCM(master)
	if err != nil {
		t.Fatal(err)
	}
	jobs := a.DeriveKey("background-jobs")
	if len(jobs) != 32 || !bytes.Equal(jobs, b.DeriveKey("background-jobs")) {
		t.Fatal("cross-process key derivation differs")
	}
	if bytes.Equal(jobs, master) || bytes.Equal(jobs, a.DeriveKey("file-delivery")) {
		t.Fatal("purposes share a secret key")
	}
	master[0] = 8
	jobs[0] ^= 1
	if !bytes.Equal(a.DeriveKey("background-jobs"), b.DeriveKey("background-jobs")) {
		t.Fatal("caller mutation altered retained key")
	}
}
