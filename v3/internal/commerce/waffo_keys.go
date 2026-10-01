package commerce

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"strings"
)

// Both Waffo APIs accept PEM or base64 DER credentials in existing settings.
func waffoKeyBytes(raw string) ([]byte, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))
	if strings.HasPrefix(raw, "-----BEGIN ") {
		block, rest := pem.Decode([]byte(raw))
		if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, ErrProviderUnavailable
		}
		return block.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
	if err != nil || len(der) == 0 {
		return nil, ErrProviderUnavailable
	}
	return der, nil
}

func waffoPrivateKey(raw string) (*rsa.PrivateKey, error) {
	der, err := waffoKeyBytes(raw)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, ErrProviderUnavailable
}

func waffoPublicKey(raw string) (*rsa.PublicKey, error) {
	der, err := waffoKeyBytes(raw)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	if cert, err := x509.ParseCertificate(der); err == nil {
		if key, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return key, nil
		}
	}
	return nil, ErrProviderUnavailable
}

func waffoSign(body []byte, rawKey string) (string, error) {
	key, err := waffoPrivateKey(rawKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", ErrProviderUnavailable
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func waffoVerify(body []byte, signature, rawKey string) error {
	key, err := waffoPublicKey(rawKey)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrInvalid
	}
	digest := sha256.Sum256(body)
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return ErrInvalid
	}
	return nil
}

func waffoPaymentURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
