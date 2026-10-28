package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"tempmail/middleware"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCatchAllSettingRequiresActiveAccount(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN is not set")
	}

	ctx := context.Background()
	s, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	previousEnabled, _ := s.GetSetting(ctx, "catchall_enabled")
	previousAccountID, _ := s.GetSetting(ctx, "catchall_account_id")
	previousSiteTitle, _ := s.GetSetting(ctx, "site_title")
	account, err := s.CreateAccount(ctx, "catchall-setting-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.SetSetting(ctx, "catchall_enabled", previousEnabled)
		_ = s.SetSetting(ctx, "catchall_account_id", previousAccountID)
		_ = s.SetSetting(ctx, "site_title", previousSiteTitle)
		_ = s.DeleteAccount(ctx, account.ID)
	}()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/settings", NewSettingHandler(s).AdminUpdate)

	request := func(body map[string]string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request(map[string]string{
		"catchall_enabled": "true", "catchall_account_id": "",
	}); recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty account status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if err := s.SetSetting(ctx, "catchall_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "catchall_account_id", ""); err != nil {
		t.Fatal(err)
	}
	if recorder := request(map[string]string{"site_title": "Catch-all compatibility test"}); recorder.Code != http.StatusOK {
		t.Fatalf("unrelated legacy setting status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder := request(map[string]string{
		"catchall_enabled": "true", "catchall_account_id": uuid.NewString(),
	}); recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing account status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if recorder := request(map[string]string{
		"catchall_enabled": "true", "catchall_account_id": account.ID.String(),
	}); recorder.Code != http.StatusOK {
		t.Fatalf("valid account status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	savedEnabled, _ := s.GetSetting(ctx, "catchall_enabled")
	savedAccountID, _ := s.GetSetting(ctx, "catchall_account_id")
	if savedEnabled != "true" || savedAccountID != account.ID.String() {
		t.Fatalf("saved catch-all settings = (%q, %q)", savedEnabled, savedAccountID)
	}
}

func TestRateLimitSettingsUpdateRuntime(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN is not set")
	}

	ctx := context.Background()
	s, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	config := middleware.NewRateLimitSettings(500, 60)
	if err := config.Load(ctx, s); err != nil {
		t.Fatal(err)
	}
	keys := []string{
		middleware.RateLimitEnabledSetting,
		middleware.RateLimitLimitSetting,
		middleware.RateLimitWindowSetting,
		middleware.AdminRateLimitEnabledSetting,
		middleware.AdminRateLimitLimitSetting,
		middleware.AdminRateLimitWindowSetting,
	}
	previous := make(map[string]string, len(keys))
	for _, key := range keys {
		previous[key], _ = s.GetSetting(ctx, key)
	}
	defer func() {
		for key, value := range previous {
			_ = s.SetSetting(ctx, key, value)
		}
	}()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/settings", NewSettingHandler(s, config).AdminUpdate)
	request := func(body map[string]string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	invalid := map[string]string{
		middleware.RateLimitEnabledSetting:      "true",
		middleware.RateLimitLimitSetting:        "0",
		middleware.RateLimitWindowSetting:       "60",
		middleware.AdminRateLimitEnabledSetting: "false",
		middleware.AdminRateLimitLimitSetting:   "500",
		middleware.AdminRateLimitWindowSetting:  "60",
	}
	if recorder := request(invalid); recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid rate limit status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	valid := map[string]string{
		middleware.RateLimitEnabledSetting:      "true",
		middleware.RateLimitLimitSetting:        "1200",
		middleware.RateLimitWindowSetting:       "45",
		middleware.AdminRateLimitEnabledSetting: "true",
		middleware.AdminRateLimitLimitSetting:   "3000",
		middleware.AdminRateLimitWindowSetting:  "10",
	}
	if recorder := request(valid); recorder.Code != http.StatusOK {
		t.Fatalf("valid rate limit status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	user, admin := config.Rules()
	if user != (middleware.RateLimitRule{Enabled: true, Limit: 1200, Window: 45}) {
		t.Fatalf("runtime user rule = %+v", user)
	}
	if admin != (middleware.RateLimitRule{Enabled: true, Limit: 3000, Window: 10}) {
		t.Fatalf("runtime admin rule = %+v", admin)
	}
	for key, want := range valid {
		got, err := s.GetSetting(ctx, key)
		if err != nil || got != want {
			t.Fatalf("saved %s = %q, want %q, err=%v", key, got, want, err)
		}
	}
}
