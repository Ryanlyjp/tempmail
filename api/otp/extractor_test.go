package otp

import "testing"

func TestExtractSegmentedCodes(t *testing.T) {
	config3 := SegmentedConfig{Enabled: true, AllowThree: true}
	config4 := SegmentedConfig{Enabled: true, AllowFour: true}

	tests := []struct {
		name   string
		text   string
		sender string
		config SegmentedConfig
		want   string
	}{
		{"letters", "Your code is ABC-DEF", "", config3, "ABC-DEF"},
		{"mixed", "Use A1C-34G to continue", "", config3, "A1C-34G"},
		{"digits", "Verification code: 123-456", "", config3, "123-456"},
		{"four", "Verification code: ABCD-12EF", "", config4, "ABCD-12EF"},
		{"three disabled", "Your code is ABC-DEF", "", SegmentedConfig{}, ""},
		{"legacy numeric hyphen behavior", "Verification code: 123-456", "", SegmentedConfig{}, ""},
		{"wrong length", "Your code is ABC-DEF", "", config4, ""},
		{"sender allowed", "Your code is ABC-DEF", "X <noreply@x.com>", SegmentedConfig{
			Enabled: true, AllowThree: true, AllowedSenders: []string{"noreply@x.com"},
		}, "ABC-DEF"},
		{"sender case insensitive", "Your code is ABC-DEF", "X <NoReply@X.COM>", SegmentedConfig{
			Enabled: true, AllowThree: true, AllowedSenders: []string{"noreply@x.com"},
		}, "ABC-DEF"},
		{"sender blocked", "Your code is ABC-DEF", "other@example.com", SegmentedConfig{
			Enabled: true, AllowThree: true, AllowedSenders: []string{"noreply@x.com"},
		}, ""},
		{"legacy fallback", "Your code is 827194", "other@example.com", SegmentedConfig{
			Enabled: true, AllowThree: true, AllowedSenders: []string{"noreply@x.com"},
		}, "827194"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractWithConfig(tt.text, tt.sender, tt.config); got != tt.want {
				t.Fatalf("ExtractWithConfig() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractSegmentedFromHTML(t *testing.T) {
	config := SegmentedConfig{Enabled: true, AllowThree: true}

	if got := ExtractFromHTMLWithConfig(`<div class="code">ABC-DEF</div>`, "noreply@x.com", config); got != "ABC-DEF" {
		t.Fatalf("segmented HTML code = %q, want ABC-DEF", got)
	}
	if got := ExtractFromHTMLWithConfig(`<div style="width:100%;max-width:600px">Welcome</div>`, "", config); got != "" {
		t.Fatalf("CSS-like attributes must not match, got %q", got)
	}
}
