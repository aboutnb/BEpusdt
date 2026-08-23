package task

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/v03413/bepusdt/app/model"
	"gorm.io/gorm"
)

func setupSyncBreakTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	model.Db = db
	if err := db.AutoMigrate(&model.Wallet{}, &model.Order{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
}

func TestPaymentScannerRunsForEnabledReceivingWallet(t *testing.T) {
	setupSyncBreakTestDB(t)

	if !syncBreak("bsc", 0) {
		t.Fatal("scanner should be idle when no receiving wallet exists")
	}
	if err := model.Db.Create(&model.Wallet{
		Status:    model.WaStatusEnable,
		TradeType: string(model.UsdtBep20),
	}).Error; err != nil {
		t.Fatalf("seed enabled wallet: %v", err)
	}
	if syncBreak("bsc", 0) {
		t.Fatal("scanner stayed stopped for an enabled receiving wallet")
	}
}

func TestTronPaymentScannerRunsForEnabledReceivingWallet(t *testing.T) {
	setupSyncBreakTestDB(t)
	if err := model.Db.Create(&model.Wallet{
		Status:    model.WaStatusEnable,
		TradeType: string(model.UsdtTrc20),
	}).Error; err != nil {
		t.Fatalf("seed enabled wallet: %v", err)
	}
	tr := newTron()
	if tr.syncBreak() {
		t.Fatal("TRON scanner stayed stopped for an enabled receiving wallet")
	}
}

func TestLookbackOrderIsMarkedOnlyAfterScanCompletes(t *testing.T) {
	setupSyncBreakTestDB(t)
	now := time.Now()
	createdAt := model.Datetime(now.Add(-time.Hour))
	updatedAt := model.Datetime(now.Add(-time.Hour))
	confirmedAt := time.Time{}
	order := model.Order{
		OrderId: "lookback-order", TradeId: "lookback-trade", TradeType: model.UsdtBep20,
		Fiat: model.CNY, Crypto: model.USDT, Rate: "7", Amount: "1", Money: "7",
		Address: "0x1111111111111111111111111111111111111111", Status: model.OrderStatusWaiting,
		ExpiredAt: now.Add(time.Hour), ConfirmedAt: &confirmedAt,
		AutoTimeAt: model.AutoTimeAt{CreatedAt: &createdAt, UpdatedAt: &updatedAt},
	}
	if err := model.Db.Create(&order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
	_, _, firstIDs, ok := getLookbackUnix(model.Network("bsc"))
	if !ok || len(firstIDs) != 1 {
		t.Fatalf("first lookback = ids:%v ok:%v", firstIDs, ok)
	}
	_, _, secondIDs, ok := getLookbackUnix(model.Network("bsc"))
	if !ok || len(secondIDs) != 1 {
		t.Fatalf("order was marked before scanning: ids:%v ok:%v", secondIDs, ok)
	}
	markLookbackDone(firstIDs)
	if _, _, _, ok := getLookbackUnix(model.Network("bsc")); ok {
		t.Fatal("completed lookback order was returned again")
	}
}
