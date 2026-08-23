package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	applog "github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/utils"
	"gorm.io/gorm"
)

func newNotifyTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "notify-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(1000)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := db.AutoMigrate(&model.Order{}); err != nil {
		t.Fatalf("auto migrate order: %v", err)
	}

	return db
}

func seedMerchantNotifyConfig(t *testing.T, db *gorm.DB) {
	t.Helper()
	model.Db = db
	if err := db.AutoMigrate(&model.Conf{}); err != nil {
		t.Fatalf("migrate config: %v", err)
	}
	if err := db.Create(&[]model.Conf{
		{K: model.ApiHMACKeyID, V: "sub2api"},
		{K: model.ApiHMACSecret, V: "merchant-secret"},
	}).Error; err != nil {
		t.Fatalf("seed merchant config: %v", err)
	}
	model.RefreshC()
}

func initNotifyTestLog(t *testing.T) {
	t.Helper()

	if err := applog.Init(filepath.Join(t.TempDir(), "logs")); err != nil {
		t.Fatalf("init log: %v", err)
	}
	t.Cleanup(func() {
		applog.Close()
	})
}

func newWaitingOrder(notifyURL string) model.Order {
	now := time.Now()
	confirmedAt := now

	return model.Order{
		OrderId:     "merchant-order-1",
		TradeId:     "trade-order-1",
		TradeType:   model.UsdtTrc20,
		Fiat:        "CNY",
		Crypto:      "USDT",
		Rate:        "7.00",
		Amount:      "1.00",
		Money:       "7.00",
		Address:     "TTestAddress1234567890",
		Status:      model.OrderStatusWaiting,
		ApiType:     model.OrderApiTypeEpusdt,
		NotifyUrl:   notifyURL,
		ExpiredAt:   now.Add(10 * time.Minute),
		ConfirmedAt: &confirmedAt,
		AutoTimeAt:  model.AutoTimeAt{CreatedAt: (*model.Datetime)(&now), UpdatedAt: (*model.Datetime)(&now)},
	}
}

func TestDeliverBepusdtStatusUpdateDoesNotHoldDBWhileHTTPIsPending(t *testing.T) {
	db := newNotifyTestDB(t)
	initNotifyTestLog(t)

	requestStarted := make(chan struct{}, 1)
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-releaseResponse
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	order := newWaitingOrder(server.URL)
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- deliverBepusdtStatusUpdate(db, &http.Client{Timeout: 2 * time.Second}, "test-auth-token", order)
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("notification request never reached test server")
	}

	queryDone := make(chan error, 1)
	go func() {
		var count int64
		queryDone <- db.Model(&model.Order{}).Where("status = ?", model.OrderStatusWaiting).Count(&count).Error
	}()

	select {
	case err := <-queryDone:
		if err != nil {
			t.Fatalf("concurrent query failed: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		close(releaseResponse)
		<-errCh
		t.Fatal("database query was blocked while notification HTTP request was in flight")
	}

	close(releaseResponse)
	if err := <-errCh; err != nil {
		t.Fatalf("deliver notification: %v", err)
	}
}

func TestMerchantV2RequiresExactSuccessAndPersistsRetry(t *testing.T) {
	db := newNotifyTestDB(t)
	initNotifyTestLog(t)
	seedMerchantNotifyConfig(t, db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	order := newWaitingOrder(server.URL)
	order.Status = model.OrderStatusSuccess
	order.ApiType = model.OrderApiTypeMerchant
	order.RefHash = "0xmerchant-transaction-hash"
	order.RefBlockNum = 123
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}

	before := time.Now()
	if err := merchantV2(order); err == nil {
		t.Fatal("callback response 'ok' was accepted")
	}
	var stored model.Order
	if err := db.Where("trade_id = ?", order.TradeId).First(&stored).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if stored.NotifyState != model.OrderNotifyStateFail || stored.NotifyNum != 1 {
		t.Fatalf("unexpected retry state: state=%d attempts=%d", stored.NotifyState, stored.NotifyNum)
	}
	if stored.NotifyNextAt == nil || stored.NotifyNextAt.Before(before.Add(900*time.Millisecond)) || stored.NotifyNextAt.After(before.Add(2*time.Second)) {
		t.Fatalf("first retry was not scheduled at about 1 second: %v", stored.NotifyNextAt)
	}
	if stored.NotifyLastResponse != "ok" || stored.NotifyLastStatus != http.StatusOK {
		t.Fatalf("callback result was not persisted: %+v", stored)
	}
}

func TestMerchantV2SignsPayloadAndRequest(t *testing.T) {
	db := newNotifyTestDB(t)
	initNotifyTestLog(t)
	seedMerchantNotifyConfig(t, db)
	verified := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			verified <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		digest := r.Header.Get(utils.HMACDigestHeader)
		if digest != utils.SHA256Hex(body) {
			verified <- fmt.Errorf("request digest mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !utils.HMACV2Verify(r.Header.Get(utils.HMACSignatureHeader), "merchant-secret", r.Method, r.URL.EscapedPath(), r.Header.Get(utils.HMACTimestampHeader), r.Header.Get(utils.HMACNonceHeader), digest) {
			verified <- fmt.Errorf("request HMAC mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var payload MerchantNotify
		if err := json.Unmarshal(body, &payload); err != nil {
			verified <- err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		signature := payload.Signature
		payload.Signature = ""
		unsigned, _ := json.Marshal(payload)
		if !utils.HMACSHA256Verify(signature, "merchant-secret", unsigned) {
			verified <- fmt.Errorf("payload HMAC mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		verified <- nil
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	order := newWaitingOrder(server.URL + "/api/v1/payment/webhook/bepusdt")
	order.Status = model.OrderStatusSuccess
	order.ApiType = model.OrderApiTypeMerchant
	order.RefHash = "0xmerchant-transaction-hash"
	order.RefBlockNum = 123
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if err := merchantV2(order); err != nil {
		t.Fatalf("deliver callback: %v", err)
	}
	if err := <-verified; err != nil {
		t.Fatal(err)
	}
	var stored model.Order
	_ = db.Where("trade_id = ?", order.TradeId).First(&stored).Error
	if stored.NotifyState != model.OrderNotifyStateSucc || stored.NotifyNum != 1 || stored.NotifyNextAt != nil {
		t.Fatalf("successful callback state was not persisted: %+v", stored)
	}
}
