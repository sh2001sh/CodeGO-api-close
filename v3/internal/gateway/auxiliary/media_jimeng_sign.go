package auxiliary

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func signJimengMedia(req *http.Request, secret string, now time.Time) error {
	parts := strings.Split(secret, "|")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return mediaError("jimeng", "invalid_channel_credential")
	}
	var body []byte
	if req.GetBody != nil {
		reader, err := req.GetBody()
		if err != nil {
			return err
		}
		body, err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return err
		}
	}
	hash := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(hash[:])
	date, day := now.UTC().Format("20060102T150405Z"), now.UTC().Format("20060102")
	req.Header.Set("X-Date", date)
	req.Header.Set("X-Content-Sha256", payloadHash)
	const signed = "content-type;host;x-content-sha256;x-date"
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	headers := "content-type:" + strings.TrimSpace(req.Header.Get("Content-Type")) + "\nhost:" + host + "\nx-content-sha256:" + payloadHash + "\nx-date:" + date + "\n"
	canonical := req.Method + "\n" + req.URL.EscapedPath() + "\n" + req.URL.Query().Encode() + "\n" + headers + "\n" + signed + "\n" + payloadHash
	requestHash := sha256.Sum256([]byte(canonical))
	scope := day + "/cn-north-1/cv/request"
	message := "HMAC-SHA256\n" + date + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := jimengMAC([]byte(strings.TrimSpace(parts[1])), day)
	for _, part := range []string{"cn-north-1", "cv", "request"} {
		key = jimengMAC(key, part)
	}
	signature := hex.EncodeToString(jimengMAC(key, message))
	req.Header.Set("Authorization", fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", strings.TrimSpace(parts[0]), scope, signed, signature))
	return nil
}

func jimengMAC(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}
