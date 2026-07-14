package otp

import (
	"net/mail"
	"regexp"
	"strings"
)

var (
	keywordPattern       = regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^A-Za-z0-9]{0,8}([A-Za-z0-9]{4,8})`)
	keywordDigitsPattern = regexp.MustCompile(`(?is)(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[- ]?time\s*(?:code|password)|otp|pin|security\s*code).{0,40}?(\d{4,8})(?:\D|$)`)
	keywordSpacedPattern = regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^0-9]{0,16}((?:\d[\s\-]){3,7}\d)`)
	numberPattern        = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(\d{4,8})(?:$|[^A-Za-z0-9])`)
	spacedNumberPattern  = regexp.MustCompile(`(?:^|[^0-9])((?:\d[\s\-]){3,7}\d)(?:$|[^0-9])`)
	mixedPattern         = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Z0-9]{4,8})(?:$|[^A-Za-z0-9])`)
	tagPattern           = regexp.MustCompile(`<[^>]+>`)
	styleScriptPattern   = regexp.MustCompile(`(?is)<(?:style|script)\b[^>]*>.*?</(?:style|script)\s*>`)
	htmlCommentPattern   = regexp.MustCompile(`(?s)<!--.*?-->`)
	isolatedCodePattern  = regexp.MustCompile(`>\s*([A-Za-z0-9][A-Za-z0-9\s\-]{2,14}[A-Za-z0-9])\s*<`)
	segmented3Pattern    = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z0-9]{3}-[A-Za-z0-9]{3})(?:$|[^A-Za-z0-9])`)
	segmented4Pattern    = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z0-9]{4}-[A-Za-z0-9]{4})(?:$|[^A-Za-z0-9])`)
	isolatedSegmented3   = regexp.MustCompile(`>\s*([A-Za-z0-9]{3}-[A-Za-z0-9]{3})\s*<`)
	isolatedSegmented4   = regexp.MustCompile(`>\s*([A-Za-z0-9]{4}-[A-Za-z0-9]{4})\s*<`)
)

type SegmentedConfig struct {
	Enabled        bool
	AllowThree     bool
	AllowFour      bool
	AllowedSenders []string
}

func Extract(text string) string {
	return ExtractWithConfig(text, "", SegmentedConfig{})
}

func ExtractWithConfig(text, sender string, config SegmentedConfig) string {
	if text == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(text), " ")
	if segmentedEnabledForSender(sender, config) {
		if code := extractSegmented(normalized, config); code != "" {
			return code
		}
	}
	if m := keywordPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return strings.ToUpper(m[1])
	}
	if m := keywordSpacedPattern.FindStringSubmatch(normalized); len(m) > 1 {
		c := compactCode(m[1])
		if len(c) >= 4 && len(c) <= 8 {
			return c
		}
	}
	if m := keywordDigitsPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return m[1]
	}
	if m := numberPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return m[1]
	}
	if m := spacedNumberPattern.FindStringSubmatch(normalized); len(m) > 1 {
		c := compactCode(m[1])
		if len(c) >= 4 && len(c) <= 8 {
			return c
		}
	}
	matches := mixedPattern.FindAllStringSubmatch(normalized, -1)
	for _, m := range matches {
		if len(m) > 1 && hasUpperLetter(m[1]) && hasDigit(m[1]) {
			return m[1]
		}
	}
	return ""
}

// ExtractFromHTML finds a verification code that's the sole text content of a
// block element (e.g. styled "code box" emails). It strips comments / style /
// script first, then looks for any `>...<` text that compacts to a 4-8 char
// alphanumeric containing at least one digit. Returns "" if not found.
func ExtractFromHTML(html string) string {
	return ExtractFromHTMLWithConfig(html, "", SegmentedConfig{})
}

func ExtractFromHTMLWithConfig(html, sender string, config SegmentedConfig) string {
	if html == "" {
		return ""
	}
	s := htmlCommentPattern.ReplaceAllString(html, " ")
	s = styleScriptPattern.ReplaceAllString(s, " ")
	if segmentedEnabledForSender(sender, config) {
		if config.AllowThree {
			if m := isolatedSegmented3.FindStringSubmatch(s); len(m) > 1 {
				return strings.ToUpper(m[1])
			}
		}
		if config.AllowFour {
			if m := isolatedSegmented4.FindStringSubmatch(s); len(m) > 1 {
				return strings.ToUpper(m[1])
			}
		}
	}
	for _, m := range isolatedCodePattern.FindAllStringSubmatch(s, -1) {
		code := compactCode(m[1])
		if len(code) >= 4 && len(code) <= 8 && hasDigit(code) {
			return strings.ToUpper(code)
		}
	}
	return ""
}

func extractSegmented(text string, config SegmentedConfig) string {
	if config.AllowThree {
		if m := segmented3Pattern.FindStringSubmatch(text); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}
	if config.AllowFour {
		if m := segmented4Pattern.FindStringSubmatch(text); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}
	return ""
}

func segmentedEnabledForSender(sender string, config SegmentedConfig) bool {
	if !config.Enabled || (!config.AllowThree && !config.AllowFour) {
		return false
	}
	if len(config.AllowedSenders) == 0 {
		return true
	}
	actual := normalizeSender(sender)
	if actual == "" {
		return false
	}
	for _, allowed := range config.AllowedSenders {
		if normalizeSender(allowed) == actual {
			return true
		}
	}
	return false
}

func normalizeSender(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(value); err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	return strings.ToLower(value)
}

func StripHTML(html string) string {
	if html == "" {
		return ""
	}
	html = htmlCommentPattern.ReplaceAllString(html, " ")
	html = styleScriptPattern.ReplaceAllString(html, " ")
	return tagPattern.ReplaceAllString(html, " ")
}

func hasUpperLetter(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func compactCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
