package jimeng

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func finalBody(req *http.Request) ([]byte, error) {
	if req.GetBody == nil {
		return nil, errors.New("jimeng final body unavailable")
	}
	reader, err := req.GetBody()
	if err != nil {
		return nil, errors.New("jimeng final body unavailable")
	}
	body, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("jimeng final body unreadable")
	}
	return body, nil
}

func signRequest(req *http.Request, body []byte, access, secret string, now time.Time) error {
	if access == "" || secret == "" {
		return errors.New("jimeng signing credentials required")
	}
	xDate, shortDate := now.UTC().Format("20060102T150405Z"), now.UTC().Format("20060102")
	payload := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payload[:])
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", payloadHash)
	values := req.URL.Query()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	escape := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	var query []string
	for _, k := range keys {
		v := append([]string(nil), values[k]...)
		sort.Strings(v)
		for _, item := range v {
			query = append(query, escape(k)+"="+escape(item))
		}
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host, "x-date": xDate, "x-content-sha256": payloadHash}
	if value := req.Header.Get("Content-Type"); value != "" {
		headers["content-type"] = value
	}
	keys = keys[:0]
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonicalHeaders strings.Builder
	for _, k := range keys {
		canonicalHeaders.WriteString(k + ":" + strings.Join(strings.Fields(headers[k]), " ") + "\n")
	}
	signed := strings.Join(keys, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, path, strings.Join(query, "&"), canonicalHeaders.String(), signed, payloadHash}, "\n")
	hash := sha256.Sum256([]byte(canonical))
	scope := shortDate + "/cn-north-1/cv/request"
	toSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	key := mac([]byte(secret), shortDate)
	key = mac(key, "cn-north-1")
	key = mac(key, "cv")
	key = mac(key, "request")
	signature := hex.EncodeToString(mac(key, toSign))
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signed+", Signature="+signature)
	return nil
}

func mac(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(s))
	return h.Sum(nil)
}
