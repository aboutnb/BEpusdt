package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	HMACKeyIDHeader     = "X-BEPUSDT-Key-Id"
	HMACTimestampHeader = "X-BEPUSDT-Timestamp"
	HMACNonceHeader     = "X-BEPUSDT-Nonce"
	HMACDigestHeader    = "X-BEPUSDT-Content-SHA256"
	HMACSignatureHeader = "X-BEPUSDT-Signature"
)

func SHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func HMACV2Canonical(method, path, timestamp, nonce, digest string) string {
	return strings.Join([]string{strings.ToUpper(method), path, timestamp, nonce, strings.ToLower(digest)}, "\n")
}

func HMACV2Sign(secret, method, path, timestamp, nonce, digest string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(HMACV2Canonical(method, path, timestamp, nonce, digest)))
	return hex.EncodeToString(mac.Sum(nil))
}

func HMACV2Verify(signature, secret, method, path, timestamp, nonce, digest string) bool {
	expected, err := hex.DecodeString(HMACV2Sign(secret, method, path, timestamp, nonce, digest))
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(actual, expected)
}

func HMACSHA256Hex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func HMACSHA256Verify(signature, secret string, body []byte) bool {
	expected, err := hex.DecodeString(HMACSHA256Hex(secret, body))
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(actual, expected)
}
