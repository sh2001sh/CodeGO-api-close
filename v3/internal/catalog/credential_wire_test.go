package catalog

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Regression: concurrency limits and OAuth client identities must survive the
// publication path used by production gateways, not only direct PG Compile.
func TestCredentialMetadataWireRoundTrip(t *testing.T) {
	enc, err := NewAESGCM(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cr := Credential{ID: 1, ChannelID: 2, Secret: "hidden-secret", Kind: "oauth", MaxConcurrency: 3, Fingerprint: CredentialFingerprint{UserAgent: "stable-ua", TLSProfile: "firefox"}}
	snap := &Snapshot{Channels: map[int64]*Channel{2: {ID: 2, Credentials: []Credential{cr}}}}
	w, err := sealSnapshot(snap, enc)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte(cr.Secret)) {
		t.Fatal("snapshot leaked secret")
	}
	w, err = decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := openSnapshot(w, enc)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Channels[2].Credentials[0]
	if got.MaxConcurrency != 3 || got.Fingerprint != cr.Fingerprint || got.Secret != cr.Secret {
		t.Fatalf("credential metadata lost: id=%d limit=%d fingerprint=%+v", got.ID, got.MaxConcurrency, got.Fingerprint)
	}
}
