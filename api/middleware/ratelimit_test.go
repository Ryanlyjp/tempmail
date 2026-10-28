package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestRateLimitSettingsLoadPersistsEnvironmentDefaults(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	keys := []string{
		RateLimitEnabledSetting,
		RateLimitLimitSetting,
		RateLimitWindowSetting,
		AdminRateLimitEnabledSetting,
		AdminRateLimitLimitSetting,
		AdminRateLimitWindowSetting,
	}
	previous := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT key, value FROM app_settings WHERE key = ANY($1)`, keys)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		previous[key] = value
	}
	rows.Close()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM app_settings WHERE key = ANY($1)`, keys)
		for key, value := range previous {
			_, _ = pool.Exec(ctx,
				`INSERT INTO app_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
				key, value,
			)
		}
	}()
	if _, err := pool.Exec(ctx, `DELETE FROM app_settings WHERE key = ANY($1)`, keys); err != nil {
		t.Fatal(err)
	}

	db, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := NewRateLimitSettings(777, 45)
	if err := settings.Load(ctx, db); err != nil {
		t.Fatal(err)
	}
	user, admin := settings.Rules()
	if user != (RateLimitRule{Enabled: true, Limit: 777, Window: 45}) {
		t.Fatalf("loaded user defaults = %+v", user)
	}
	if admin != (RateLimitRule{Enabled: false, Limit: 777, Window: 45}) {
		t.Fatalf("loaded admin defaults = %+v", admin)
	}
	for key, want := range map[string]string{
		RateLimitEnabledSetting:      "true",
		RateLimitLimitSetting:        "777",
		RateLimitWindowSetting:       "45",
		AdminRateLimitEnabledSetting: "false",
		AdminRateLimitLimitSetting:   "777",
		AdminRateLimitWindowSetting:  "45",
	} {
		got, err := db.GetSetting(ctx, key)
		if err != nil || got != want {
			t.Fatalf("persisted %s = %q, want %q, err=%v", key, got, want, err)
		}
	}
}

func TestRateLimitSettingsApplyAndValidate(t *testing.T) {
	settings := NewRateLimitSettings(700, 90)
	user, admin := settings.Rules()
	if !user.Enabled || user.Limit != 700 || user.Window != 90 {
		t.Fatalf("user defaults = %+v", user)
	}
	if admin.Enabled || admin.Limit != 700 || admin.Window != 90 {
		t.Fatalf("admin defaults = %+v", admin)
	}

	values := map[string]string{
		RateLimitEnabledSetting:      "true",
		RateLimitLimitSetting:        "1200",
		RateLimitWindowSetting:       "30",
		AdminRateLimitEnabledSetting: "true",
		AdminRateLimitLimitSetting:   "3000",
		AdminRateLimitWindowSetting:  "10",
	}
	if err := settings.Apply(values); err != nil {
		t.Fatal(err)
	}
	user, admin = settings.Rules()
	if user != (RateLimitRule{Enabled: true, Limit: 1200, Window: 30}) {
		t.Fatalf("user rule = %+v", user)
	}
	if admin != (RateLimitRule{Enabled: true, Limit: 3000, Window: 10}) {
		t.Fatalf("admin rule = %+v", admin)
	}

	values[RateLimitLimitSetting] = "0"
	if err := ValidateRateLimitSettings(values); err == nil {
		t.Fatal("zero user limit was accepted")
	}
	values[RateLimitLimitSetting] = "500"
	values[AdminRateLimitWindowSetting] = "86401"
	if err := ValidateRateLimitSettings(values); err == nil {
		t.Fatal("oversized admin window was accepted")
	}
}

func TestRateLimitFixedWindowAndRoleRules(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("TEST_REDIS_PASSWORD"),
	})
	defer rdb.Close()
	ctx := context.Background()
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	settings := NewRateLimitSettings(2, 2)
	if err := settings.Apply(map[string]string{
		RateLimitEnabledSetting:      "true",
		RateLimitLimitSetting:        "2",
		RateLimitWindowSetting:       "2",
		AdminRateLimitEnabledSetting: "false",
		AdminRateLimitLimitSetting:   "1",
		AdminRateLimitWindowSetting:  "2",
	}); err != nil {
		t.Fatal(err)
	}

	userRouter := rateLimitTestRouter(rdb, settings, false, uuid.New())
	if status := rateLimitRequest(userRouter).Code; status != http.StatusOK {
		t.Fatalf("first user request status = %d", status)
	}
	if status := rateLimitRequest(userRouter).Code; status != http.StatusOK {
		t.Fatalf("second user request status = %d", status)
	}
	if status := rateLimitRequest(userRouter).Code; status != http.StatusTooManyRequests {
		t.Fatalf("third user request status = %d", status)
	}

	time.Sleep(1200 * time.Millisecond)
	retry := rateLimitRequest(userRouter)
	if retry.Code != http.StatusTooManyRequests {
		t.Fatalf("retry during window status = %d", retry.Code)
	}
	var body struct {
		RetryAfter int64 `json:"retry_after"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RetryAfter > 1 {
		t.Fatalf("retry extended window: retry_after=%d", body.RetryAfter)
	}
	time.Sleep(1100 * time.Millisecond)
	if status := rateLimitRequest(userRouter).Code; status != http.StatusOK {
		t.Fatalf("request after original window status = %d", status)
	}

	adminRouter := rateLimitTestRouter(rdb, settings, true, uuid.New())
	for i := 0; i < 3; i++ {
		if status := rateLimitRequest(adminRouter).Code; status != http.StatusOK {
			t.Fatalf("unlimited admin request %d status = %d", i+1, status)
		}
	}
	if err := settings.Apply(map[string]string{
		RateLimitEnabledSetting:      "true",
		RateLimitLimitSetting:        "2",
		RateLimitWindowSetting:       "2",
		AdminRateLimitEnabledSetting: "true",
		AdminRateLimitLimitSetting:   "1",
		AdminRateLimitWindowSetting:  "2",
	}); err != nil {
		t.Fatal(err)
	}
	if status := rateLimitRequest(adminRouter).Code; status != http.StatusOK {
		t.Fatalf("first limited admin request status = %d", status)
	}
	if status := rateLimitRequest(adminRouter).Code; status != http.StatusTooManyRequests {
		t.Fatalf("second limited admin request status = %d", status)
	}
}

func rateLimitTestRouter(rdb *redis.Client, settings *RateLimitSettings, admin bool, accountID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(AccountKey, &model.Account{ID: accountID, IsAdmin: admin})
		c.Next()
	})
	router.Use(RateLimit(rdb, settings))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func rateLimitRequest(router http.Handler) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))
	return recorder
}
