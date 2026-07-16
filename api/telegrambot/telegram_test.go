package telegrambot

import (
	"strings"
	"testing"

	"tempmail/model"
)

func TestForwardHeaderIncludesOriginalAndDeliveryRecipients(t *testing.T) {
	mailbox := model.Mailbox{FullAddress: "forward@tempmail.example"}
	email := model.Email{
		Sender:  "sender@example.com",
		Subject: "Test",
		RawMessage: "From: sender@example.com\r\n" +
			"To: Original User <original@example.com>\r\n" +
			"Delivered-To: forward@tempmail.example\r\n\r\nBody",
	}

	message := buildSubjectOnlyText(mailbox, email, nil)
	for _, want := range []string{
		"接收邮箱: forward@tempmail.example",
		"原始收件人: Original User <original@example.com>",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("forward message missing %q:\n%s", want, message)
		}
	}
}
