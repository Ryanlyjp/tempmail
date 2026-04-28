package otp

import (
	"regexp"
	"strings"
)

var (
	keywordPattern = regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^A-Za-z0-9]{0,8}([A-Za-z0-9]{4,8})`)
	numberPattern  = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(\d{4,8})(?:$|[^A-Za-z0-9])`)
	mixedPattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Z0-9]{4,8})(?:$|[^A-Za-z0-9])`)
	tagPattern     = regexp.MustCompile(`<[^>]+>`)
)

func Extract(text string) string {
	if text == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(text), " ")
	if m := keywordPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return strings.ToUpper(m[1])
	}
	if m := numberPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return m[1]
	}
	matches := mixedPattern.FindAllStringSubmatch(normalized, -1)
	for _, m := range matches {
		if len(m) > 1 && hasUpperLetter(m[1]) {
			return m[1]
		}
	}
	return ""
}

func StripHTML(html string) string {
	if html == "" {
		return ""
	}
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
