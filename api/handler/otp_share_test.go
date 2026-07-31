package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNormalizeOTPShareAPIKey(t *testing.T) {
	valid := "share_key_1234567890"
	got, err := normalizeOTPShareAPIKey(valid)
	if err != nil || got != valid {
		t.Fatalf("normalize valid api key = %q, %v", got, err)
	}
	if _, err := normalizeOTPShareAPIKey("too-short"); err == nil {
		t.Fatal("expected short api key to be rejected")
	}
	if _, err := normalizeOTPShareAPIKey("invalid key with spaces"); err == nil {
		t.Fatal("expected api key with spaces to be rejected")
	}
}

func TestOTPShareExpiry(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	permanent, err := otpShareExpiry(0, now)
	if err != nil || permanent != nil {
		t.Fatalf("permanent expiry = %v, %v", permanent, err)
	}
	expiresAt, err := otpShareExpiry(7, now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(7 * 24 * time.Hour)
	if expiresAt == nil || !expiresAt.Equal(want) {
		t.Fatalf("expiry = %v, want %v", expiresAt, want)
	}
	if _, err := otpShareExpiry(-1, now); err == nil {
		t.Fatal("expected negative expiry to be rejected")
	}
}

func TestPageTokenCannotAuthenticateAPIKeyRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/public/otp-share/latest", nil)
	ctx.Params = gin.Params{{Key: "token", Value: "page_token_123456"}}

	if got := extractOTPShareAPIKey(ctx); got != "" {
		t.Fatalf("page token authenticated api route: %q", got)
	}

	ctx.Request.Header.Set("Authorization", "Bearer share_key_1234567890")
	if got := extractOTPShareAPIKey(ctx); got != "share_key_1234567890" {
		t.Fatalf("bearer api key = %q", got)
	}
}
