package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsAdvertisableIPv4(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"192.168.0.2", true},
		{"10.0.0.5", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"169.254.10.1", false},
		{"172.17.0.2", false},
		{"172.19.96.1", false},
		{"8.8.8.8", false},
	}
	for _, tt := range cases {
		got := isAdvertisableIPv4(net.ParseIP(tt.ip))
		if got != tt.want {
			t.Errorf("isAdvertisableIPv4(%s)=%v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestNetworkInfoHandler(t *testing.T) {
	// Create token
	token := generateToken(32)
	putTestAccessToken(token, &TokenData{
		Username:  "testuser",
		UserID:    "test-user-id",
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	})

	req := httptest.NewRequest("GET", "/api/network-info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	networkInfoHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if addresses, ok := response["addresses"].([]interface{}); ok {
		if len(addresses) == 0 {
			t.Log("No network addresses found (may be normal in test environment)")
		}
	} else {
		t.Error("Response should contain 'addresses' field")
	}

	stale := "192.0.2.1"
	onThisMachine := false
	for _, ip := range lanIPv4Addresses() {
		if ip == stale {
			onThisMachine = true
			break
		}
	}
	if !onThisMachine {
		for _, addr := range response["addresses"].([]interface{}) {
			item := addr.(map[string]interface{})
			if item["address"] == stale {
				t.Fatalf("network-info must not hardcode stale IP %s", stale)
			}
		}
	}
}

func TestAdvertisedIPv4AddressesUsesHostLanIP(t *testing.T) {
	t.Setenv("HOST_LAN_IP", "192.168.0.2, 10.1.2.3")
	got := advertisedIPv4Addresses()
	if len(got) != 2 || got[0] != "192.168.0.2" || got[1] != "10.1.2.3" {
		t.Fatalf("got %v", got)
	}
}
