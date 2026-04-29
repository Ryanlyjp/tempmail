package otp

import (
	"regexp"
	"strings"
)

var (
	keywordPattern      = regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|安全码|verif(?:y|ication)?\s*code|one[- ]?time\s*(?:code|password)|otp|pin|security\s*code)[^A-Za-z0-9]{0,8}((?:[A-Za-z0-9][\s\-]{0,2}){3,7}[A-Za-z0-9])`)
	numberPattern       = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(\d{4,8})(?:$|[^A-Za-z0-9])`)
	spacedNumberPattern = regexp.MustCompile(`(?:^|[^0-9])((?:\d[\s\-]){3,7}\d)(?:$|[^0-9])`)
	mixedPattern        = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Z0-9]{4,8})(?:$|[^A-Za-z0-9])`)
	tagPattern          = regexp.MustCompile(`<[^>]+>`)
	styleScriptPattern  = regexp.MustCompile(`(?is)<(style|script)\b[^>]*>.*?</\1\s*>`)
)

func Extract(text string) string {
	if text == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(text), " ")
	if m := keywordPattern.FindStringSubmatch(normalized); len(m) > 1 {
		return strings.ToUpper(compactCode(m[1]))
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
		if len(m) > 1 && hasUpperLetter(m[1]) && !isCommonWord(m[1]) {
			return m[1]
		}
	}
	return ""
}

func StripHTML(html string) string {
	if html == "" {
		return ""
	}
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

func compactCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var commonWordBlacklist = map[string]struct{}{
	"FONT": {}, "FACE": {}, "ARIAL": {}, "HELVETICA": {}, "VERDANA": {},
	"STYLE": {}, "CLASS": {}, "TABLE": {}, "TBODY": {}, "THEAD": {},
	"HTML": {}, "HEAD": {}, "BODY": {}, "META": {}, "LINK": {}, "SPAN": {},
	"COLOR": {}, "ALIGN": {}, "WIDTH": {}, "HEIGHT": {}, "BORDER": {},
	"PADDING": {}, "MARGIN": {}, "CENTER": {}, "BOLD": {}, "NORMAL": {},
	"UTF": {}, "HTTPS": {}, "HTTP": {},
}

func isCommonWord(s string) bool {
	_, ok := commonWordBlacklist[strings.ToUpper(s)]
	return ok
}
