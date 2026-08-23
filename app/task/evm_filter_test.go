package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	appconf "github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/model"
	"gorm.io/gorm"
)

func TestEVMLogQueryFiltersRegisteredNetworkContracts(t *testing.T) {
	requestBodies := make([]map[string]any, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requestBodies = append(requestBodies, requestBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	model.Db = db
	if err := db.AutoMigrate(&model.Conf{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Wallet{}, &model.Order{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Conf{K: model.RpcEndpointBsc, V: server.URL}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Wallet{TradeType: string(model.UsdtBep20), Address: "0x1111111111111111111111111111111111111111"}).Error; err != nil {
		t.Fatal(err)
	}
	model.RefreshC()

	scanner := &evm{Network: string(appconf.Bsc), Client: server.Client()}
	if _, err := scanner.parseEventTransfer(evmBlock{From: 100, To: 109}, map[string]time.Time{}); err != nil {
		t.Fatalf("parse event transfer: %v", err)
	}
	if len(requestBodies) < 2 {
		t.Fatalf("expected one request per contract, got %d", len(requestBodies))
	}
	addresses := make(map[string]bool)
	for _, requestBody := range requestBodies {
		params, ok := requestBody["params"].([]any)
		if !ok || len(params) != 1 {
			t.Fatalf("unexpected params: %#v", requestBody["params"])
		}
		filter, ok := params[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected filter: %#v", params[0])
		}
		address, ok := filter["address"].(string)
		if !ok || address == "" {
			t.Fatalf("contract address filter missing: %#v", filter["address"])
		}
		addresses[address] = true
		if filter["fromBlock"] != "0x64" || filter["toBlock"] != "0x6d" {
			t.Fatalf("unexpected block range: %#v", filter)
		}
		topics, ok := filter["topics"].([]any)
		if !ok || len(topics) != 3 || topics[1] != nil {
			t.Fatalf("unexpected topics filter: %#v", filter["topics"])
		}
		recipients, ok := topics[2].([]any)
		if !ok || len(recipients) != 1 || recipients[0] != "0x0000000000000000000000001111111111111111111111111111111111111111" {
			t.Fatalf("receiving wallet topic missing: %#v", topics[2])
		}
	}
	if len(addresses) < 2 {
		t.Fatalf("expected distinct contract filters: %#v", addresses)
	}
}

func TestEVMLogQueryFailsOverToSecondaryEndpoint(t *testing.T) {
	primaryCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls++
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32005,"message":"limit exceeded"}}`))
	}))
	defer primary.Close()
	secondaryCalls := 0
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondaryCalls++
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	defer secondary.Close()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	model.Db = db
	if err := db.AutoMigrate(&model.Conf{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Wallet{}, &model.Order{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Conf{K: model.RpcEndpointBsc, V: primary.URL + "," + secondary.URL}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Wallet{TradeType: string(model.UsdtBep20), Address: "0x2222222222222222222222222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	model.RefreshC()
	scanner := &evm{Network: string(appconf.Bsc), Client: primary.Client()}
	if _, err := scanner.parseEventTransfer(evmBlock{From: 100, To: 100}, map[string]time.Time{}); err == nil {
		t.Fatal("expected primary endpoint error")
	}
	if _, err := scanner.parseEventTransfer(evmBlock{From: 100, To: 100}, map[string]time.Time{}); err != nil {
		t.Fatalf("secondary endpoint failed: %v", err)
	}
	if primaryCalls != 1 || secondaryCalls < 2 {
		t.Fatalf("calls primary=%d secondary=%d", primaryCalls, secondaryCalls)
	}
}
