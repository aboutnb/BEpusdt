package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRefreshCAppliesMerchantEnvironmentOverrides(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	Db = db
	if err := Db.AutoMigrate(&Conf{}); err != nil {
		t.Fatal(err)
	}
	if err := Db.Create(&[]Conf{
		{K: ApiHMACKeyID, V: "database-key"},
		{K: ApiHMACSecret, V: "database-secret"},
		{K: ApiNotifyHosts, V: "database.example"},
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Setenv("BEPUSDT_HMAC_KEY_ID", "sub2api")
	t.Setenv("BEPUSDT_HMAC_SECRET", "environment-secret")
	t.Setenv("BEPUSDT_NOTIFY_HOSTS", "pay.example.com")
	t.Setenv("BEPUSDT_RPC_ENDPOINT_BSC", "https://rpc-a.example,https://rpc-b.example")
	RefreshC()

	if got := MerchantKeyID(); got != "sub2api" {
		t.Fatalf("merchant key id = %q", got)
	}
	if got := MerchantSecret(); got != "environment-secret" {
		t.Fatalf("merchant secret did not use environment override")
	}
	if got := GetC(ApiNotifyHosts); got != "pay.example.com" {
		t.Fatalf("notify hosts = %q", got)
	}
	if got := Endpoint(Network("bsc")); got != "https://rpc-a.example,https://rpc-b.example" {
		t.Fatalf("BSC RPC endpoints = %q", got)
	}
}
