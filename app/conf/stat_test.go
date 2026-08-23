package conf

import "testing"

func TestGetNetworkStatReturnsLiveSuccessRate(t *testing.T) {
	network := "test-live-rate"
	RecordSuccess(network, "100")
	RecordFailure(network)
	block, success, _, ok := GetNetworkStat(network)
	if !ok || block != "100" || success != "50.00%" {
		t.Fatalf("stat = block:%q success:%q ok:%v", block, success, ok)
	}
}

func TestNetworkStatBlockHeightDoesNotRegressDuringLookback(t *testing.T) {
	network := "test-monotonic-block"
	RecordSuccess(network, "200")
	RecordSuccess(network, "150")
	block, success, _, ok := GetNetworkStat(network)
	if !ok || block != "200" || success != "100.00%" {
		t.Fatalf("stat = block:%q success:%q ok:%v", block, success, ok)
	}
}
