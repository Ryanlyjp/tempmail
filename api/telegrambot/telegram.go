package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tempmail/mailutil"
	"tempmail/model"
	"tempmail/otp"
)

const (
	ModeAllWithAttachments    = "all_with_attachments"
	ModeAllWithoutAttachments = "all_without_attachments"
	ModeAttachmentsOnly       = "attachments_only"
	ModeNotifyAttachments     = "notify_attachments"
)

type Config struct {
	BotToken        string
	ChatID          string
	MessageThreadID string
	Mode            string
}

type SettingsReader interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

var defaultHTTPClient = &http.Client{Timeout: 25 * time.Second}

func NormalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ModeAllWithoutAttachments:
		return ModeAllWithoutAttachments
	case ModeAttachmentsOnly:
		return ModeAttachmentsOnly
	case ModeNotifyAttachments:
		return ModeNotifyAttachments
	default:
		return ModeAllWithAttachments
	}
}

func ConfigReady(cfg Config) bool {
	return strings.TrimSpace(cfg.BotToken) != "" && strings.TrimSpace(cfg.ChatID) != ""
}

func LoadConfig(ctx context.Context, reader SettingsReader) (Config, error) {
	token, err := reader.GetSetting(ctx, "tg_bot_token")
	if err != nil {
		return Config{}, err
	}
	chatID, err := reader.GetSetting(ctx, "tg_chat_id")
	if err != nil {
		return Config{}, err
	}
	threadID, _ := reader.GetSetting(ctx, "tg_message_thread_id")
	mode, _ := reader.GetSetting(ctx, "tg_forward_mode")

	return Config{
		BotToken:        token,
		ChatID:          chatID,
		MessageThreadID: threadID,
		Mode:            mode,
	}, nil
}

func SendEmail(ctx context.Context, cfg Config, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) error {
	return SendEmailWithMode(ctx, cfg, mailbox, email, attachments, "")
}

func SendEmailWithMode(ctx context.Context, cfg Config, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment, overrideMode string) error {
	if !ConfigReady(cfg) {
		return nil
	}

	mode := NormalizeMode(cfg.Mode)
	if strings.TrimSpace(overrideMode) != "" {
		mode = NormalizeMode(overrideMode)
	}
	if !shouldSendForMode(mode, len(attachments) > 0) {
		return nil
	}

	message := buildMessageText(mode, mailbox, email, attachments)
	if err := sendMessage(ctx, cfg, message); err != nil {
		return err
	}

	if !shouldUploadAttachments(mode) || len(attachments) == 0 {
		return nil
	}

	for idx, attachment := range attachments {
		caption := fmt.Sprintf("邮箱: %s\n附件 %d/%d", mailbox.FullAddress, idx+1, len(attachments))
		if err := sendDocument(ctx, cfg, attachment, caption); err != nil {
			return err
		}
	}

	return nil
}

func SendTestMessage(ctx context.Context, cfg Config, text string) error {
	if !ConfigReady(cfg) {
		return fmt.Errorf("telegram config incomplete")
	}
	return sendMessage(ctx, cfg, text)
}

func shouldSendForMode(mode string, hasAttachments bool) bool {
	switch mode {
	case ModeAttachmentsOnly, ModeNotifyAttachments:
		return hasAttachments
	default:
		return true
	}
}

func shouldUploadAttachments(mode string) bool {
	return mode == ModeAllWithAttachments || mode == ModeAttachmentsOnly
}

func buildMessageText(mode string, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	var b strings.Builder
	if mode == ModeNotifyAttachments {
		b.WriteString("TempMail 附件通知\n")
	} else {
		b.WriteString("TempMail 邮件转发\n")
	}
	b.WriteString("邮箱: ")
	b.WriteString(mailbox.FullAddress)
	b.WriteString("\n发件人: ")
	b.WriteString(fallback(email.Sender, "—"))
	b.WriteString("\n主题: ")
	b.WriteString(fallback(email.Subject, "(无主题)"))
	b.WriteString("\n时间: ")
	b.WriteString(email.ReceivedAt.In(time.Local).Format("2006-01-02 15:04:05"))
	if len(attachments) > 0 {
		b.WriteString("\n附件: ")
		b.WriteString(strconv.Itoa(len(attachments)))
		b.WriteString(" 个")
	}

	if mode == ModeNotifyAttachments {
		b.WriteString("\n说明: 收到一封带附件邮件")
		return b.String()
	}

	preview := buildBodyPreview(email)
	if preview != "" {
		b.WriteString("\n\n正文预览:\n")
		b.WriteString(preview)
	}
	return b.String()
}

func buildBodyPreview(email model.Email) string {
	preview := strings.TrimSpace(email.BodyText)
	if preview == "" {
		preview = otp.StripHTML(email.BodyHTML)
	}
	preview = strings.Join(strings.Fields(preview), " ")
	return truncate(preview, 1200)
}

func fallback(value, backup string) string {
	if strings.TrimSpace(value) == "" {
		return backup
	}
	return value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func sendMessage(ctx context.Context, cfg Config, text string) error {
	payload := map[string]any{
		"chat_id": cfg.ChatID,
		"text":    text,
	}
	if threadID, ok := parseThreadID(cfg.MessageThreadID); ok {
		payload["message_thread_id"] = threadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL(cfg.BotToken, "sendMessage"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func sendDocument(ctx context.Context, cfg Config, attachment mailutil.ParsedAttachment, caption string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", cfg.ChatID); err != nil {
		return err
	}
	if threadID, ok := parseThreadID(cfg.MessageThreadID); ok {
		if err := writer.WriteField("message_thread_id", strconv.Itoa(threadID)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", attachment.Filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(attachment.Data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL(cfg.BotToken, "sendDocument"), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return doRequest(req)
}

func doRequest(req *http.Request) error {
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var parsed apiResponse
	if len(rawBody) > 0 {
		_ = json.Unmarshal(rawBody, &parsed)
	}

	if resp.StatusCode >= 300 {
		if parsed.Description != "" {
			return fmt.Errorf("telegram api error: %s", parsed.Description)
		}
		return fmt.Errorf("telegram api http %d", resp.StatusCode)
	}
	if !parsed.OK && len(rawBody) > 0 {
		if parsed.Description != "" {
			return fmt.Errorf("telegram api error: %s", parsed.Description)
		}
		return fmt.Errorf("telegram api returned not ok")
	}
	return nil
}

func apiURL(token, method string) string {
	return "https://api.telegram.org/bot" + strings.TrimSpace(token) + "/" + method
}

func parseThreadID(raw string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
