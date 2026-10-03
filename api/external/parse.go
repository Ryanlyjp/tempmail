package external

import (
	"bytes"
	"errors"
	"io"
	"net/mail"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset"
	messageMail "github.com/emersion/go-message/mail"
	"tempmail/model"
)

// Only a single, unambiguous recipient is eligible for a scoped share. Message
// bodies and forwarded attachments are never used to grant mailbox access.
func originalRecipient(raw []byte, mother string) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	candidates := map[string]bool{}
	for _, key := range []string{"Original-Recipient", "X-Original-To", "Envelope-To", "Delivered-To", "To", "Cc"} {
		for _, value := range msg.Header[key] {
			if key == "Original-Recipient" {
				if _, v, ok := strings.Cut(value, ";"); ok {
					value = v
				}
			}
			list, e := mail.ParseAddressList(value)
			if e != nil {
				return ""
			}
			for _, a := range list {
				v := strings.ToLower(a.Address)
				if v != mother {
					candidates[v] = true
				}
			}
		}
	}
	if len(candidates) == 1 {
		for v := range candidates {
			return v
		}
	}
	if len(candidates) > 1 {
		return ""
	}
	for _, key := range []string{"To", "Delivered-To", "X-Original-To"} {
		list, _ := mail.ParseAddressList(msg.Header.Get(key))
		for _, a := range list {
			if strings.EqualFold(a.Address, mother) {
				return mother
			}
		}
	}
	return ""
}

func parseEmail(raw []byte, received time.Time) (model.Email, error) {
	var e model.Email
	r, err := messageMail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		return e, err
	}
	defer r.Close()
	e.Subject, _ = r.Header.Subject()
	from, _ := r.Header.AddressList("From")
	if len(from) > 0 {
		e.Sender = from[0].String()
	}
	e.ReceivedAt = received
	e.SizeBytes = len(raw)
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if p == nil {
			return e, err
		}
		if h, ok := p.Header.(*messageMail.InlineHeader); ok {
			ct, _, _ := h.ContentType()
			if ct != "text/plain" && ct != "text/html" {
				continue
			}
			b, e2 := io.ReadAll(p.Body)
			if e2 != nil {
				return e, e2
			}
			if ct == "text/html" {
				e.BodyHTML += string(b)
			} else {
				e.BodyText += string(b)
			}
		}
	}
	return e, nil
}

func excludedFolder(name string, attrs []string) bool {
	for _, v := range attrs {
		switch strings.ToLower(v) {
		case `\sent`, `\drafts`, `\trash`, `\noselect`:
			return true
		}
	}
	lower := strings.ToLower(name)
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool { return r == '/' || r == '.' }) {
		switch part {
		case "sent", "sent items", "sent messages", "sent mail", "outbox", "drafts", "draft", "draft messages", "trash", "deleted", "deleted items", "deleted messages", "已发送", "已发送邮件", "已发送的邮件", "发件箱", "草稿箱", "草稿", "已删除", "已删除邮件", "回收站":
			return true
		}
	}
	return false
}
