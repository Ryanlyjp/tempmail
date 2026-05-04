package main

import (
	"context"
	"log"
	"time"

	"tempmail/mailutil"
	"tempmail/model"
	"tempmail/store"
	"tempmail/telegrambot"
)

func forwardMailboxEmail(s *store.Store, mailbox model.Mailbox, email model.Email) {
	if !mailbox.TGForwardEnabled {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cfg, err := telegrambot.LoadConfig(ctx, s)
	if err != nil {
		log.Printf("[tg-forward] load config failed for %s: %v", mailbox.FullAddress, err)
		return
	}
	if !telegrambot.ConfigReady(cfg) {
		return
	}

	mode := telegrambot.NormalizeMode(cfg.Mode)
	attachments := make([]mailutil.ParsedAttachment, 0)
	if mode != telegrambot.ModeAllWithoutAttachments {
		attachments, err = mailutil.ParseAttachments(email.RawMessage)
		if err != nil {
			log.Printf("[tg-forward] parse attachments failed for %s: %v", mailbox.FullAddress, err)
			if mode == telegrambot.ModeAttachmentsOnly || mode == telegrambot.ModeNotifyAttachments {
				return
			}
			attachments = nil
		}
	}

	if err := telegrambot.SendEmail(ctx, cfg, mailbox, email, attachments); err != nil {
		log.Printf("[tg-forward] send failed for %s: %v", mailbox.FullAddress, err)
	}
}
