package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"tempmail/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func TestExtractOTPShareAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/public/otp-share/latest", nil)

	ctx.Request.Header.Set("Authorization", "Bearer share_key_1234567890")
	if got := extractOTPShareAPIKey(ctx); got != "share_key_1234567890" {
		t.Fatalf("bearer api key = %q", got)
	}

	ctx.Request.Header.Del("Authorization")
	ctx.Request = httptest.NewRequest("GET", "/public/otp-share/latest?api_key=query_share_key_123", nil)
	if got := extractOTPShareAPIKey(ctx); got != "query_share_key_123" {
		t.Fatalf("query api key = %q", got)
	}
}

func TestOTPShareResponseUsesSingleAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "http://mail.example/api/otp-shares", nil)
	share := &model.MailboxOTPShare{
		MailboxID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		FullAddress: "shared@example.com",
		APIKey:      "share_key_1234567890",
		Enabled:     true,
	}

	response := buildOTPShareResponse(ctx, share)
	if got := response["url"]; got != "http://mail.example/otp-share/share_key_1234567890" {
		t.Fatalf("page url = %v", got)
	}
	if _, exists := response["token"]; exists {
		t.Fatal("management response exposed a second page credential")
	}
}
