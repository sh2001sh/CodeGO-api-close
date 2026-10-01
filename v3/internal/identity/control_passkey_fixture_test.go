//go:build pgintegration

package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

type virtualPasskey struct {
	key *ecdsa.PrivateKey
	id  []byte
}

func newVirtualPasskey(t *testing.T) virtualPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return virtualPasskey{key, []byte("virtual-credential")}
}
func b64(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }
func passkeyAuthData(flags byte, counter uint32) []byte {
	rp := sha256.Sum256([]byte("codego.test"))
	b := append(rp[:], flags)
	b = binary.BigEndian.AppendUint32(b, counter)
	return b
}
func (v virtualPasskey) registerResponse(t *testing.T, challenge string) []byte {
	t.Helper()
	client := []byte(`{"type":"webauthn.create","challenge":"` + challenge + `","origin":"https://codego.test"}`)
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: v.key.X.FillBytes(make([]byte, 32)), -3: v.key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	data := passkeyAuthData(0x45, 0)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(v.id)))
	data = append(data, v.id...)
	data = append(data, cose...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "authData": data, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	response := map[string]any{"id": b64(v.id), "rawId": b64(v.id), "type": "public-key", "response": map[string]any{"attestationObject": b64(attestation), "clientDataJSON": b64(client), "transports": []string{"internal"}}, "clientExtensionResults": map[string]any{}}
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (v virtualPasskey) loginResponse(t *testing.T, challenge string, handle []byte, counter uint32) []byte {
	t.Helper()
	client := []byte(`{"type":"webauthn.get","challenge":"` + challenge + `","origin":"https://codego.test"}`)
	data := passkeyAuthData(0x05, counter)
	clientHash := sha256.Sum256(client)
	signed := append(append([]byte{}, data...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, v.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	response := map[string]any{"id": b64(v.id), "rawId": b64(v.id), "type": "public-key", "response": map[string]any{"authenticatorData": b64(data), "clientDataJSON": b64(client), "signature": b64(signature), "userHandle": b64(handle)}, "clientExtensionResults": map[string]any{}}
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
