package files

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDeliveryTTLMinutes = 15
	deliveryExpiryBucket      = 5 * time.Minute
)

var ErrDeliveryUnavailable = errors.New("signed file delivery is unavailable")
var ErrInvalidDeliveryToken = errors.New("invalid file delivery token")

// BuildSignedDeliveryURL creates a cache-friendly URL whose expiry is shared
// by requests in the same short time bucket.
func BuildSignedDeliveryURL(fileID string, now time.Time) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FILE_DELIVERY_BASE_URL")), "/")
	secret := strings.TrimSpace(os.Getenv("FILE_DELIVERY_SIGNING_SECRET"))
	if baseURL == "" || secret == "" {
		return "", ErrDeliveryUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: invalid base URL", ErrDeliveryUnavailable)
	}
	expires := deliveryExpiry(now)
	pathPrefix := "/v1/files/"
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		pathPrefix = "/files/"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + pathPrefix + url.PathEscape(fileID) + "/delivery"
	query := parsed.Query()
	query.Set("expires", strconv.FormatInt(expires.Unix(), 10))
	query.Set("signature", signDelivery(fileID, expires.Unix(), secret))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func VerifyDeliveryToken(fileID, expiresRaw, signature string, now time.Time) error {
	secret := strings.TrimSpace(os.Getenv("FILE_DELIVERY_SIGNING_SECRET"))
	if secret == "" || !strings.HasPrefix(fileID, "file-codego-") {
		return ErrInvalidDeliveryToken
	}
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil || expires < now.Unix() || expires > now.Add(deliveryTTL()+deliveryExpiryBucket+time.Minute).Unix() {
		return ErrInvalidDeliveryToken
	}
	expected, err := hex.DecodeString(signDelivery(fileID, expires, secret))
	if err != nil {
		return ErrInvalidDeliveryToken
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, provided) {
		return ErrInvalidDeliveryToken
	}
	return nil
}

func deliveryTTL() time.Duration {
	return time.Duration(positiveEnvInt("FILE_DELIVERY_TTL_MINUTES", defaultDeliveryTTLMinutes)) * time.Minute
}

func deliveryExpiry(now time.Time) time.Time {
	return now.UTC().Truncate(deliveryExpiryBucket).Add(deliveryTTL() + deliveryExpiryBucket)
}

func signDelivery(fileID string, expires int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s\n%d", fileID, expires)
	return hex.EncodeToString(mac.Sum(nil))
}
