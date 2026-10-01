package tencent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// signature signs the exact serialized body that will be sent, including zero
// valued sampling options. The application ID belongs to the legacy credential
// format but is not part of Tencent's TC3 signature or request body.
func signature(req *http.Request, body []byte, secretID, secretKey string, timestamp int64) string {
	const signedHeaders = "content-type;host;x-tc-action"
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := "content-type:" + strings.TrimSpace(req.Header.Get("Content-Type")) + "\n" +
		"host:" + strings.ToLower(strings.TrimSpace(host)) + "\n" +
		"x-tc-action:" + strings.ToLower(strings.TrimSpace(req.Header.Get("X-TC-Action"))) + "\n"
	canonical := req.Method + "\n" + req.URL.EscapedPath() + "\n" + req.URL.RawQuery + "\n" + headers + "\n" + signedHeaders + "\n" + hash(body)
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	scope := date + "/hunyuan/tc3_request"
	toSign := fmt.Sprintf("TC3-HMAC-SHA256\n%d\n%s\n%s", timestamp, scope, hash([]byte(canonical)))
	dateKey := hmacSHA256([]byte("TC3"+secretKey), date)
	serviceKey := hmacSHA256(dateKey, "hunyuan")
	signingKey := hmacSHA256(serviceKey, "tc3_request")
	sig := hex.EncodeToString(hmacSHA256(signingKey, toSign))
	return "TC3-HMAC-SHA256 Credential=" + secretID + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + sig
}

func hash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, input string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(input))
	return h.Sum(nil)
}
