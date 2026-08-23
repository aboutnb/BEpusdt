package utils

import "testing"

func TestHMACV2SignAndVerify(t *testing.T) {
	body := []byte(`{"amount":"102.00"}`)
	digest := SHA256Hex(body)
	signature := HMACV2Sign("secret", "post", "/api/v1/merchant/order/create", "1700000000", "nonce-1234567890", digest)
	if !HMACV2Verify(signature, "secret", "POST", "/api/v1/merchant/order/create", "1700000000", "nonce-1234567890", digest) {
		t.Fatal("valid signature was rejected")
	}
	if HMACV2Verify(signature, "secret", "POST", "/api/v1/merchant/order/query", "1700000000", "nonce-1234567890", digest) {
		t.Fatal("signature was accepted for a different path")
	}
}

func TestHMACSHA256PayloadSignature(t *testing.T) {
	body := []byte(`{"event_id":"evt_1"}`)
	signature := HMACSHA256Hex("secret", body)
	if !HMACSHA256Verify(signature, "secret", body) {
		t.Fatal("valid payload signature was rejected")
	}
	if HMACSHA256Verify(signature, "secret", []byte(`{"event_id":"evt_2"}`)) {
		t.Fatal("tampered payload was accepted")
	}
}

func TestAllowedCallbackURLForHosts(t *testing.T) {
	if !IsAllowedCallbackURLForHosts("https://pay.example.com/webhook", "pay.example.com,*.internal.example.com") {
		t.Fatal("exact allowlisted host was rejected")
	}
	if !IsAllowedCallbackURLForHosts("https://sub.internal.example.com/webhook", "*.internal.example.com") {
		t.Fatal("wildcard allowlisted host was rejected")
	}
	if IsAllowedCallbackURLForHosts("https://example.com.attacker.invalid/webhook", "example.com") {
		t.Fatal("host suffix bypass was accepted")
	}
	if IsAllowedCallbackURLForHosts("http://127.0.0.1/webhook", "") {
		t.Fatal("empty allowlist accepted a host")
	}
}
