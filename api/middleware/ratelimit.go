package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	RateLimitEnabledSetting      = "rate_limit_enabled"
	RateLimitLimitSetting        = "rate_limit_limit"
	RateLimitWindowSetting       = "rate_limit_window"
	AdminRateLimitEnabledSetting = "admin_rate_limit_enabled"
	AdminRateLimitLimitSetting   = "admin_rate_limit_limit"
	AdminRateLimitWindowSetting  = "admin_rate_limit_window"

	minRateLimit  = 1
	maxRateLimit  = 1000000
	minRateWindow = 1
	maxRateWindow = 86400
)

var fixedWindowScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

type RateLimitRule struct {
	Enabled bool
	Limit   int
	Window  int
}

type RateLimitSettings struct {
	mu    sync.RWMutex
	user  RateLimitRule
	admin RateLimitRule
}

func NewRateLimitSettings(defaultLimit, defaultWindow int) *RateLimitSettings {
	if defaultLimit < minRateLimit || defaultLimit > maxRateLimit {
		defaultLimit = 500
	}
	if defaultWindow < minRateWindow || defaultWindow > maxRateWindow {
		defaultWindow = 60
	}
	return &RateLimitSettings{
		user:  RateLimitRule{Enabled: true, Limit: defaultLimit, Window: defaultWindow},
		admin: RateLimitRule{Enabled: false, Limit: defaultLimit, Window: defaultWindow},
	}
}

func IsRateLimitSetting(key string) bool {
	switch key {
	case RateLimitEnabledSetting, RateLimitLimitSetting, RateLimitWindowSetting,
		AdminRateLimitEnabledSetting, AdminRateLimitLimitSetting, AdminRateLimitWindowSetting:
		return true
	default:
		return false
	}
}

func (s *RateLimitSettings) Load(ctx context.Context, db *store.Store) error {
	settings, err := db.GetAllSettings(ctx)
	if err != nil {
		return err
	}

	user, admin := s.Rules()
	defaults := map[string]string{
		RateLimitEnabledSetting:      strconv.FormatBool(user.Enabled),
		RateLimitLimitSetting:        strconv.Itoa(user.Limit),
		RateLimitWindowSetting:       strconv.Itoa(user.Window),
		AdminRateLimitEnabledSetting: strconv.FormatBool(admin.Enabled),
		AdminRateLimitLimitSetting:   strconv.Itoa(admin.Limit),
		AdminRateLimitWindowSetting:  strconv.Itoa(admin.Window),
	}
	for key, value := range defaults {
		if _, exists := settings[key]; exists {
			continue
		}
		if err := db.SetSetting(ctx, key, value); err != nil {
			return err
		}
		settings[key] = value
	}
	return s.Apply(settings)
}

func (s *RateLimitSettings) Apply(settings map[string]string) error {
	user, err := parseRateLimitRule(settings, "", true)
	if err != nil {
		return err
	}
	admin, err := parseRateLimitRule(settings, "admin_", false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.user = user
	s.admin = admin
	s.mu.Unlock()
	return nil
}

func ValidateRateLimitSettings(settings map[string]string) error {
	config := NewRateLimitSettings(500, 60)
	return config.Apply(settings)
}

func (s *RateLimitSettings) Rules() (RateLimitRule, RateLimitRule) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.user, s.admin
}

func parseRateLimitRule(settings map[string]string, prefix string, enabledDefault bool) (RateLimitRule, error) {
	enabled := enabledDefault
	if raw, exists := settings[prefix+RateLimitEnabledSetting]; exists {
		parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return RateLimitRule{}, fmt.Errorf("%s must be true or false", prefix+RateLimitEnabledSetting)
		}
		enabled = parsed
	}
	limit, err := parseRateLimitNumber(settings[prefix+RateLimitLimitSetting], 500, minRateLimit, maxRateLimit, prefix+RateLimitLimitSetting)
	if err != nil {
		return RateLimitRule{}, err
	}
	window, err := parseRateLimitNumber(settings[prefix+RateLimitWindowSetting], 60, minRateWindow, maxRateWindow, prefix+RateLimitWindowSetting)
	if err != nil {
		return RateLimitRule{}, err
	}
	return RateLimitRule{Enabled: enabled, Limit: limit, Window: window}, nil
}

func parseRateLimitNumber(raw string, fallback, min, max int, key string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, min, max)
	}
	return value, nil
}

// RateLimit applies independent fixed-window limits to ordinary and administrator accounts.
func RateLimit(rdb *redis.Client, settings *RateLimitSettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		account := GetAccount(c)
		if account == nil {
			c.Next()
			return
		}

		userRule, adminRule := settings.Rules()
		rule := userRule
		role := "user"
		if account.IsAdmin {
			rule = adminRule
			role = "admin"
		}
		if !rule.Enabled {
			c.Header("X-RateLimit-Limit", "unlimited")
			c.Next()
			return
		}

		redisKey := fmt.Sprintf("rl:%s:%s", role, account.ID.String())
		result, err := fixedWindowScript.Run(c.Request.Context(), rdb, []string{redisKey}, rule.Window).Slice()
		if err != nil || len(result) != 2 {
			c.Next()
			return
		}
		count, okCount := result[0].(int64)
		ttl, okTTL := result[1].(int64)
		if !okCount || !okTTL {
			c.Next()
			return
		}
		if ttl < 1 {
			ttl = int64(rule.Window)
		}

		remaining := int64(rule.Limit) - count
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(rule.Limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(ttl)*time.Second).Unix(), 10))

		if count > int64(rule.Limit) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"limit":       rule.Limit,
				"retry_after": ttl,
			})
			return
		}

		c.Next()
	}
}
