package ratelimit

import (
	"testing"
	"time"
)

func TestAllowUnderLimit(t *testing.T) {
	l := New(5, time.Minute, nil)
	for i := 0; i < 5; i++ {
		if !l.Allow("192.168.1.1:12345") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("192.168.1.1:12345") {
		t.Error("6th request should be denied")
	}
}

func TestAllowListBypassesLimit(t *testing.T) {
	l := New(1, time.Minute, []string{"192.168.1.100"})
	// First request uses the token
	l.Allow("192.168.1.1:12345")
	// Allow-listed IP should always pass
	for i := 0; i < 10; i++ {
		if !l.Allow("192.168.1.100:12345") {
			t.Errorf("allow-listed IP should not be rate limited, request %d", i+1)
		}
	}
}

func TestCIDRAllowList(t *testing.T) {
	l := New(1, time.Minute, []string{"10.0.0.0/8"})
	// Exhaust token for a non-allowed IP
	l.Allow("192.168.1.1:12345")
	if l.Allow("192.168.1.1:12345") {
		t.Error("non-allowed IP should be limited")
	}
	// 10.x.x.x should bypass
	for i := 0; i < 5; i++ {
		if !l.Allow("10.1.2.3:12345") {
			t.Error("CIDR-allowed IP should not be limited")
		}
	}
}

func TestDisabledWhenRateZero(t *testing.T) {
	l := New(0, time.Minute, nil)
	for i := 0; i < 100; i++ {
		if !l.Allow("1.2.3.4:12345") {
			t.Error("disabled limiter should allow all requests")
		}
	}
}

func TestWindowReset(t *testing.T) {
	l := New(1, 50*time.Millisecond, nil)
	if !l.Allow("1.2.3.4:12345") {
		t.Error("first request should pass")
	}
	if l.Allow("1.2.3.4:12345") {
		t.Error("second request should fail")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("1.2.3.4:12345") {
		t.Error("request after window reset should pass")
	}
}

func TestCleanup(t *testing.T) {
	l := New(1, 10*time.Millisecond, nil)
	l.Allow("1.1.1.1:1")
	l.Allow("2.2.2.2:1")
	time.Sleep(30 * time.Millisecond)
	l.Cleanup()
	l.mu.Lock()
	count := len(l.visitors)
	l.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 visitors after cleanup, got %d", count)
	}
}
