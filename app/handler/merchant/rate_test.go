package merchant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestVerifyRequestAcceptsValidSignatureOnlyOnce(t *testing.T) {
	t.Setenv("BEPUSDT_HMAC_KEY_ID", "sub2api")
	t.Setenv("BEPUSDT_HMAC_SECRET", "test-secret")
	merchantNonces.Lock()
	merchantNonces.items = make(map[string]time.Time)
	merchantNonces.Unlock()

	body := []byte(`{"crypto":"USDT","fiat":"CNY"}`)
	request := signedMerchantRequest(t, http.MethodPost, "/api/v1/merchant/rate", body, "nonce-1234567890123456")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	if !verifyRequest(ctx, body) {
		t.Fatal("valid request was rejected")
	}
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	if verifyRequest(ctx, body) {
		t.Fatal("replayed nonce was accepted")
	}
}

func TestVerifyRequestRejectsIncorrectKeyID(t *testing.T) {
	t.Setenv("BEPUSDT_HMAC_KEY_ID", "sub2api")
	t.Setenv("BEPUSDT_HMAC_SECRET", "test-secret")
	body := []byte(`{}`)
	request := signedMerchantRequest(t, http.MethodGet, "/api/v1/merchant/readiness", body, "nonce-abcdefghijklmno")
	request.Header.Set(keyIDHeader, "wrong-key")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request
	if verifyRequest(ctx, body) {
		t.Fatal("request with wrong key id was accepted")
	}
}

func signedMerchantRequest(t *testing.T, method, path string, body []byte, nonce string) *http.Request {
	t.Helper()
	// Use a current timestamp because request verification intentionally rejects stale calls.
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	canonical := strings.Join([]string{method, path, timestamp, nonce, digest}, "\n")
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(canonical))
	request := httptest.NewRequest(method, "http://gateway.local"+path, strings.NewReader(string(body)))
	request.Header.Set(keyIDHeader, "sub2api")
	request.Header.Set(timestampHead, timestamp)
	request.Header.Set(nonceHeader, nonce)
	request.Header.Set(digestHeader, digest)
	request.Header.Set(signatureHead, hex.EncodeToString(mac.Sum(nil)))
	return request
}
