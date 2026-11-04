package auth

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("via local cloudflared = %q", got)
	}
	r.RemoteAddr = "192.168.1.50:5555"
	if got := ClientIP(r); got != "192.168.1.50" {
		t.Fatalf("forged header from LAN peer accepted: %q", got)
	}
}
