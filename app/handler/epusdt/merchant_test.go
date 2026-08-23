package epusdt

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/utils"
	"gorm.io/gorm"
)

func setupMerchantHandlerTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	model.Db = db
	if err := db.AutoMigrate(&model.Conf{}, &model.MerchantNonce{}, &model.Wallet{}, &model.Order{}, &model.Rate{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if err := db.Create(&[]model.Conf{
		{K: model.ApiHMACKeyID, V: "sub2api"}, {K: model.ApiHMACSecret, V: "merchant-secret"},
		{K: model.ApiHMACClockSkew, V: "300"}, {K: model.ScannerMaxAgeSeconds, V: "120"},
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	model.RefreshC()
	now := time.Now()
	if err := db.Create(&model.Rate{Rate: "7.25", RawRate: 7.25, Fiat: string(model.CNY), Crypto: string(model.USDT), AutoTimeAt: model.AutoTimeAt{CreatedAt: (*model.Datetime)(&now), UpdatedAt: (*model.Datetime)(&now)}}).Error; err != nil {
		t.Fatalf("seed rate: %v", err)
	}
	h := Epusdt{}
	engine := gin.New()
	engine.POST("/api/v1/merchant/capabilities", h.MerchantSignVerify, h.MerchantCapabilities)
	engine.POST("/api/v1/merchant/rate", h.MerchantSignVerify, h.MerchantRate)
	return engine
}

func signedMerchantRequest(t *testing.T, body []byte, nonce string) *http.Request {
	return signedMerchantRequestForPath(t, "/api/v1/merchant/capabilities", body, nonce)
}

func signedMerchantRequestForPath(t *testing.T, path string, body []byte, nonce string) *http.Request {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := utils.SHA256Hex(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(utils.HMACKeyIDHeader, "sub2api")
	req.Header.Set(utils.HMACTimestampHeader, timestamp)
	req.Header.Set(utils.HMACNonceHeader, nonce)
	req.Header.Set(utils.HMACDigestHeader, digest)
	req.Header.Set(utils.HMACSignatureHeader, utils.HMACV2Sign("merchant-secret", req.Method, path, timestamp, nonce, digest))
	return req
}

func TestMerchantRateReturnsLatestUSDTQuote(t *testing.T) {
	engine := setupMerchantHandlerTest(t)
	body := []byte(`{"crypto":"USDT","fiat":"CNY"}`)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, signedMerchantRequestForPath(t, "/api/v1/merchant/rate", body, "nonce-rate-123456"))
	if response.Code != http.StatusOK {
		t.Fatalf("rate request failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
		Data struct {
			Rate string `json:"rate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rate response: %v", err)
	}
	if payload.Code != "ok" || payload.Data.Rate != decimal.NewFromFloat(7.25).String() {
		t.Fatalf("unexpected rate response: %+v", payload)
	}
}

func TestMerchantSignVerifyAcceptsValidRequestAndRejectsReplay(t *testing.T) {
	engine := setupMerchantHandlerTest(t)
	body := []byte(`{"networks":["tron","bsc"]}`)
	nonce := "nonce-1234567890"

	first := httptest.NewRecorder()
	engine.ServeHTTP(first, signedMerchantRequest(t, body, nonce))
	if first.Code != http.StatusOK {
		t.Fatalf("valid request rejected: status=%d body=%s", first.Code, first.Body.String())
	}

	replay := httptest.NewRecorder()
	engine.ServeHTTP(replay, signedMerchantRequest(t, body, nonce))
	if replay.Code != http.StatusConflict {
		t.Fatalf("replayed nonce status=%d body=%s", replay.Code, replay.Body.String())
	}
}

func TestMerchantSignVerifyRejectsTamperedBody(t *testing.T) {
	engine := setupMerchantHandlerTest(t)
	req := signedMerchantRequest(t, []byte(`{"networks":["tron"]}`), "nonce-abcdefghij")
	req.Body = io.NopCloser(bytes.NewReader([]byte(`{"networks":["bsc"]}`)))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body status=%d body=%s", response.Code, response.Body.String())
	}
}
