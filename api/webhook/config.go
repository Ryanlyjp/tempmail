package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const (
	EnabledSetting = "tgmag_webhook_enabled"
	URLSetting     = "tgmag_webhook_url"
	SecretSetting  = "tgmag_webhook_secret"
	DomainsSetting = "tgmag_webhook_domains"
)

type SettingReader interface {
	GetSetting(context.Context, string) (string, error)
}

type DomainRule struct {
	Domain            string `json:"domain"`
	IncludeSubdomains bool   `json:"include_subdomains"`
}

type Config struct {
	Enabled bool
	URL     string
	Secret  string
	Rules   []DomainRule
}

func ParseRules(raw string) ([]DomainRule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var rules []DomainRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("tgmag_webhook_domains must be valid JSON")
	}
	seen := make(map[string]bool, len(rules))
	for i := range rules {
		rules[i].Domain = strings.ToLower(strings.TrimSpace(rules[i].Domain))
		if rules[i].Domain == "" || strings.ContainsAny(rules[i].Domain, "@/ ") {
			return nil, fmt.Errorf("invalid webhook domain: %s", rules[i].Domain)
		}
		if seen[rules[i].Domain] {
			return nil, fmt.Errorf("duplicate webhook domain: %s", rules[i].Domain)
		}
		seen[rules[i].Domain] = true
	}
	return rules, nil
}

func ValidateURL(raw string) (string, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", nil
	}
	parsed, err := url.Parse(normalized)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("tgmag_webhook_url must be an http(s) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("tgmag_webhook_url must not contain credentials, query, or fragment")
	}
	return normalized, nil
}

func Validate(enabled bool, rawURL, secret string, rules []DomainRule) (Config, error) {
	validatedURL, err := ValidateURL(rawURL)
	if err != nil {
		return Config{}, err
	}
	secret = strings.TrimSpace(secret)
	if secret != "" && (len(secret) < 32 || strings.IndexFunc(secret, unicode.IsSpace) >= 0) {
		return Config{}, fmt.Errorf("tgmag_webhook_secret must contain at least 32 non-whitespace characters")
	}
	if enabled && (validatedURL == "" || secret == "" || len(rules) == 0) {
		return Config{}, fmt.Errorf("enabled tgmag webhook requires URL, secret, and at least one domain")
	}
	return Config{Enabled: enabled, URL: validatedURL, Secret: secret, Rules: rules}, nil
}

func Load(ctx context.Context, reader SettingReader) (Config, error) {
	values := make(map[string]string, 4)
	for _, key := range []string{EnabledSetting, URLSetting, SecretSetting, DomainsSetting} {
		value, err := reader.GetSetting(ctx, key)
		if err != nil {
			return Config{}, err
		}
		values[key] = value
	}
	rules, err := ParseRules(values[DomainsSetting])
	if err != nil {
		return Config{}, err
	}
	return Validate(strings.EqualFold(strings.TrimSpace(values[EnabledSetting]), "true"), values[URLSetting], values[SecretSetting], rules)
}

func (c Config) Matches(address string) bool {
	if !c.Enabled {
		return false
	}
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(address[at+1:]))
	for _, rule := range c.Rules {
		if domain == rule.Domain || (rule.IncludeSubdomains && strings.HasSuffix(domain, "."+rule.Domain)) {
			return true
		}
	}
	return false
}
