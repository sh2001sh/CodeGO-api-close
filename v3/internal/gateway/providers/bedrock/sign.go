package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Bedrock uses the SigV4 double-escaped canonical path, unlike S3. Signed
// headers are exactly those sent, including an optional STS token.
func signRequest(req *http.Request, body []byte, cred credential, at time.Time) error {
	if req.URL.Host == "" || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return fmt.Errorf("request URL must be absolute HTTP")
	}
	at = at.UTC()
	date, timestamp := at.Format("20060102"), at.Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", timestamp)
	if cred.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", cred.SessionToken)
	}
	canonicalHeaders, signedHeaders := canonicalRequestHeaders(req)
	queryValues := req.URL.Query()
	for _, values := range queryValues {
		sort.Strings(values)
	}
	query := strings.ReplaceAll(queryValues.Encode(), "+", "%20")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, awsEncode(path, true), query,
		canonicalHeaders, signedHeaders, sha256Hex(body)}, "\n")
	scope := date + "/" + cred.Region + "/bedrock/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	key := hmacSHA256([]byte("AWS4"+cred.SecretAccessKey), date)
	key = hmacSHA256(key, cred.Region)
	key = hmacSHA256(key, "bedrock")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cred.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

// canonicalRequestHeaders builds SigV4's canonical header block and
// semicolon-joined signed header list from req's headers (excluding
// Authorization, User-Agent and Content-Length, which SigV4 never signs).
func canonicalRequestHeaders(req *http.Request) (canonicalHeaders, signedHeaders string) {
	headers := map[string]string{"host": req.URL.Host}
	if req.ContentLength > 0 {
		headers["content-length"] = strconv.FormatInt(req.ContentLength, 10)
	}
	if req.Host != "" {
		headers["host"] = req.Host
	}
	for key, values := range req.Header {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "user-agent" || lower == "content-length" {
			continue
		}
		clean := make([]string, len(values))
		for i, value := range values {
			clean[i] = strings.Join(strings.Fields(value), " ")
		}
		headers[lower] = strings.Join(clean, ",")
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		fmt.Fprintf(&builder, "%s:%s\n", name, headers[name])
	}
	return builder.String(), strings.Join(names, ";")
}

func awsEncode(value string, preserveSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == '/' && preserveSlash {
			out.WriteByte(c)
		} else {
			out.WriteByte('%')
			out.WriteByte(hexDigits[c>>4])
			out.WriteByte(hexDigits[c&15])
		}
	}
	return out.String()
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
