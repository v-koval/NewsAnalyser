package images

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.10.10", false},
		{"224.0.0.1", false},
		{"0.0.0.0", false},
		{"::1", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
		{"100.64.0.1", false},
		{"64:ff9b::7f00:1", false},
	}
	for _, c := range cases {
		if got := isPublicIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("isPublicIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
	if isPublicIP(nil) {
		t.Error("isPublicIP(nil) = true, want false")
	}
}

func TestAllowedURL(t *testing.T) {
	for _, ok := range []string{"http://example.com/a.jpg", "https://example.com/a.jpg"} {
		if err := allowedURL(ok); err != nil {
			t.Errorf("allowedURL(%s) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"ftp://example.com/a", "file:///etc/passwd", "data:image/png;base64,x", "://bad"} {
		if err := allowedURL(bad); err == nil {
			t.Errorf("allowedURL(%s) = nil, want error", bad)
		}
	}
}

func TestNewSafeClientBlocksPrivateDial(t *testing.T) {
	tr := newSafeClient(time.Second).Transport.(*http.Transport)
	for _, addr := range []string{"127.0.0.1:80", "[64:ff9b::7f00:1]:80"} {
		_, err := tr.DialContext(context.Background(), "tcp", addr)
		if err == nil || !strings.Contains(err.Error(), "blocked non-public address") {
			t.Fatalf("DialContext(%s) err = %v, want blocked error", addr, err)
		}
	}
}
