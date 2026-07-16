package mailutil

import (
	"mime"
	"net/mail"
	"strings"
)

var recipientHeaderPriority = []string{
	"To",
	"Original-Recipient",
	"X-Original-To",
	"Envelope-To",
	"Delivered-To",
}

func OriginalRecipient(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return ""
	}

	for _, header := range recipientHeaderPriority {
		value := strings.TrimSpace(msg.Header.Get(header))
		if value == "" {
			continue
		}
		if decoded, err := new(mime.WordDecoder).DecodeHeader(value); err == nil {
			value = decoded
		}
		if strings.EqualFold(header, "Original-Recipient") {
			if _, address, ok := strings.Cut(value, ";"); ok {
				value = address
			}
		}
		value = strings.Join(strings.Fields(value), " ")
		if value != "" {
			return value
		}
	}
	return ""
}
