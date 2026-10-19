package http

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAndResetsAfterSuccess(t *testing.T) {
	limiter := newLoginLimiter(2, time.Minute)
	now := time.Now()
	limiter.failed("192.0.2.1", now)
	if allowed, _ := limiter.allow("192.0.2.1", now.Add(time.Second)); !allowed {
		t.Fatal("first failure should not block a login attempt")
	}
	limiter.failed("192.0.2.1", now.Add(time.Second))
	if allowed, retry := limiter.allow("192.0.2.1", now.Add(2*time.Second)); allowed || retry <= 0 {
		t.Fatalf("expected blocked attempt with retry delay, got allowed=%v retry=%v", allowed, retry)
	}
	limiter.succeeded("192.0.2.1")
	if allowed, _ := limiter.allow("192.0.2.1", now.Add(2*time.Second)); !allowed {
		t.Fatal("successful login should clear the failure bucket")
	}
}
