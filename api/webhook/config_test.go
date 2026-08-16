package webhook

import "testing"

func TestConfigMatchesExactAndSelectedSubdomains(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Rules: []DomainRule{
			{Domain: "exact.example.com"},
			{Domain: "wild.example.net", IncludeSubdomains: true},
		},
	}

	tests := map[string]bool{
		"a@exact.example.com":         true,
		"a@child.exact.example.com":   false,
		"b@wild.example.net":          true,
		"b@one.two.wild.example.net":  true,
		"b@notwild.example.net":       false,
		"b@wild.example.net.attacker": false,
	}
	for address, expected := range tests {
		if actual := cfg.Matches(address); actual != expected {
			t.Fatalf("Matches(%q) = %v, want %v", address, actual, expected)
		}
	}
}

func TestValidateRequiresCompleteEnabledConfiguration(t *testing.T) {
	_, err := Validate(true, "https://tgmag.example/webhooks/selfhosted-tempmail", "s", []DomainRule{{Domain: "mail.example.com"}})
	if err == nil {
		t.Fatal("Validate accepted a short secret")
	}
	_, err = Validate(true, "https://tgmag.example/hook?token=secret", "ssssssssssssssssssssssssssssssss", []DomainRule{{Domain: "mail.example.com"}})
	if err == nil {
		t.Fatal("Validate accepted a URL query")
	}
}

func TestSignatureUsesTimestampDotBody(t *testing.T) {
	actual := Signature("secret", "1700000000", []byte(`{"id":"mail-1"}`))
	const expected = "381969e9947d0ede97d1ef375ec610da3a56f8e9f89f39a623c00d16f5b533fb"
	if actual != expected {
		t.Fatalf("Signature() = %q, want %q", actual, expected)
	}
}
