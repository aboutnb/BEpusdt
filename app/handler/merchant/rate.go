package merchant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/model"
)

const (
	keyIDHeader   = "X-BEPUSDT-Key-Id"
	timestampHead = "X-BEPUSDT-Timestamp"
	nonceHeader   = "X-BEPUSDT-Nonce"
	digestHeader  = "X-BEPUSDT-Content-SHA256"
	signatureHead = "X-BEPUSDT-Signature"
	maxBodySize   = 1 << 20
)

var merchantNonces = struct {
	sync.Mutex
	items map[string]time.Time
}{items: make(map[string]time.Time)}

type Rate struct{}

type rateRequest struct {
	Crypto string `json:"crypto"`
	Fiat   string `json:"fiat"`
}

func (Rate) Quote(ctx *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxBodySize+1))
	if err != nil || len(raw) > maxBodySize || !verifyRequest(ctx, raw) {
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": "unauthorized", "message": "invalid merchant signature"})
		return
	}
	var req rateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid JSON body"})
		return
	}
	crypto := model.Crypto(strings.ToUpper(strings.TrimSpace(req.Crypto)))
	fiat := model.Fiat(strings.ToUpper(strings.TrimSpace(req.Fiat)))
	if crypto != model.USDT || fiat != model.CNY {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "unsupported_pair", "message": "only USDT/CNY is supported"})
		return
	}
	rate, err := model.GetOrderRate(crypto, fiat, "")
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "rate_unavailable", "message": "USDT/CNY rate is unavailable"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"code": "ok",
		"data": gin.H{"crypto": string(crypto), "fiat": string(fiat), "rate": rate.String(), "updated_at": time.Now().Unix()},
	})
}

func verifyRequest(ctx *gin.Context, body []byte) bool {
	keyID := strings.TrimSpace(ctx.GetHeader(keyIDHeader))
	timestamp := ctx.GetHeader(timestampHead)
	nonce := ctx.GetHeader(nonceHeader)
	digest := strings.ToLower(ctx.GetHeader(digestHeader))
	signature := strings.ToLower(ctx.GetHeader(signatureHead))
	configuredKeyID := strings.TrimSpace(os.Getenv("BEPUSDT_HMAC_KEY_ID"))
	secret := strings.TrimSpace(os.Getenv("BEPUSDT_HMAC_SECRET"))
	if configuredKeyID == "" || secret == "" || keyID != configuredKeyID || timestamp == "" || len(nonce) < 16 || digest == "" || signature == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || abs(time.Now().Unix()-ts) > 300 || digest != sha256Hex(body) {
		return false
	}
	canonical := strings.Join([]string{strings.ToUpper(ctx.Request.Method), ctx.Request.URL.EscapedPath(), timestamp, nonce, digest}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	actual, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(expected)
	if !hmac.Equal(actual, want) {
		return false
	}
	return consumeNonce(keyID + ":" + nonce)
}

func consumeNonce(value string) bool {
	now := time.Now()
	expiresAt := now.Add(5 * time.Minute)
	merchantNonces.Lock()
	defer merchantNonces.Unlock()
	for nonce, expires := range merchantNonces.items {
		if !expires.After(now) {
			delete(merchantNonces.items, nonce)
		}
	}
	if _, exists := merchantNonces.items[value]; exists {
		return false
	}
	merchantNonces.items[value] = expiresAt
	return true
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
