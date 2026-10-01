package kling

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func authToken(secret string) (string, error) {
	if strings.HasPrefix(secret, "sk-") {
		return secret, nil
	}
	parts := strings.Split(secret, "|")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", errors.New("invalid kling credential: expected accessKey|secretKey")
	}
	now := time.Now().Unix()
	claims, err := json.Marshal(map[string]any{"iss": strings.TrimSpace(parts[0]), "exp": now + 1800, "nbf": now - 5})
	if err != nil {
		return "", err
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(parts[1])))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), nil
}
