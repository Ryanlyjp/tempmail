package mailutil

import "testing"

func TestOriginalRecipientPrefersVisibleToHeader(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"To: Original User <original@example.com>\r\n" +
		"Delivered-To: forward@tempmail.example\r\n" +
		"\r\nBody"

	if got := OriginalRecipient(raw); got != "Original User <original@example.com>" {
		t.Fatalf("OriginalRecipient() = %q", got)
	}
}

func TestOriginalRecipientFallsBackToDeliveryHeaders(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"Original-Recipient: rfc822; fallback@example.com\r\n" +
		"Delivered-To: forward@tempmail.example\r\n" +
		"\r\nBody"

	if got := OriginalRecipient(raw); got != "fallback@example.com" {
		t.Fatalf("OriginalRecipient() = %q", got)
	}
}
