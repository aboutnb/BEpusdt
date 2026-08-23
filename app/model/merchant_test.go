package model

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func setupMerchantModelTest(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	Db = db
	if err := Db.AutoMigrate(&Order{}, &Wallet{}, &Rate{}, &Conf{}, &MerchantNonce{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	now := time.Now()
	rows := []Conf{
		{K: PaymentMinAmount, V: "0.01"}, {K: PaymentMaxAmount, V: "99999"}, {K: AtomUSDT, V: "0.01"},
		{K: PaymentTimeout, V: "1200"},
	}
	if err := Db.Create(&rows).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	RefreshC()
	rate := Rate{Rate: "7", RawRate: 7, Fiat: string(CNY), Crypto: string(USDT), AutoTimeAt: AutoTimeAt{CreatedAt: (*Datetime)(&now), UpdatedAt: (*Datetime)(&now)}}
	if err := Db.Create(&rate).Error; err != nil {
		t.Fatalf("seed rate: %v", err)
	}
	wallet := Wallet{Name: "test", Status: WaStatusEnable, Address: "T" + strings.Repeat("A", 33), MatchAddr: "T" + strings.Repeat("A", 33), TradeType: string(UsdtTrc20), AutoTimeAt: AutoTimeAt{CreatedAt: (*Datetime)(&now), UpdatedAt: (*Datetime)(&now)}}
	if err := Db.Create(&wallet).Error; err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}

func merchantOrderParams() OrderParams {
	return OrderParams{
		Money: decimal.RequireFromString("102.00"), ApiType: OrderApiTypeMerchant,
		OrderId: "sub2-order-1", TradeType: UsdtTrc20, RedirectUrl: "https://sub2.example.com/result",
		NotifyUrl: "https://sub2.example.com/webhook", Name: "balance recharge", Timeout: 1200, Fiat: CNY,
	}
}

func TestStartBuildMerchantOrderIsIdempotent(t *testing.T) {
	setupMerchantModelTest(t)
	params := merchantOrderParams()
	first, err := StartBuildMerchantOrder(params)
	if err != nil {
		t.Fatalf("create first order: %v", err)
	}
	second, err := StartBuildMerchantOrder(params)
	if err != nil {
		t.Fatalf("repeat identical order: %v", err)
	}
	if first.TradeId != second.TradeId || first.Amount != second.Amount {
		t.Fatalf("idempotent create changed quote: first=%+v second=%+v", first, second)
	}
	params.Money = decimal.RequireFromString("103.00")
	if _, err := StartBuildMerchantOrder(params); err == nil {
		t.Fatal("same order_id with a different amount was accepted")
	}
	var count int64
	Db.Model(&Order{}).Where("order_id = ?", params.OrderId).Count(&count)
	if count != 1 {
		t.Fatalf("expected one stored order, got %d", count)
	}
}

func TestStartBuildMerchantOrderConcurrentCreate(t *testing.T) {
	setupMerchantModelTest(t)
	params := merchantOrderParams()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := StartBuildMerchantOrder(params)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create failed: %v", err)
		}
	}
	var count int64
	Db.Model(&Order{}).Where("order_id = ?", params.OrderId).Count(&count)
	if count != 1 {
		t.Fatalf("expected one stored order, got %d", count)
	}
}

func TestRegisterMerchantNonceRejectsReplay(t *testing.T) {
	setupMerchantModelTest(t)
	expires := time.Now().Add(5 * time.Minute)
	if err := RegisterMerchantNonce("default", "nonce-1234567890", expires); err != nil {
		t.Fatalf("register nonce: %v", err)
	}
	if err := RegisterMerchantNonce("default", "nonce-1234567890", expires); err == nil {
		t.Fatal("replayed nonce was accepted")
	}
}
