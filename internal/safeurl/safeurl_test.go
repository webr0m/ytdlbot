package safeurl

import (
	"context"
	"net/netip"
	"testing"
)

func TestReservedIPs(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.96.107.123", "192.168.1.2", "::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "198.18.0.1"} {
		if Public(netip.MustParseAddr(s)) {
			t.Error("IP permitida:", s)
		}
	}
	if !Public(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("bloqueó IP pública")
	}
}
func TestRejectURLs(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com", "https://user:pass@example.com", "http://example.com:5432", "http://127.0.0.1"} {
		if Validate(context.Background(), u) == nil {
			t.Error("URL permitida:", u)
		}
	}
}
