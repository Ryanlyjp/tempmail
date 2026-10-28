package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
