package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tempmail/mailutil"
	"tempmail/model"
	"tempmail/otp"
)

const (
	ModeAllWithAttachments       = "all_with_attachments"
	ModeAllWithoutAttachments    = "all_without_attachments"
	ModeAttachmentsOnly          = "attachments_only"
	ModeNotifyAttachments        = "notify_attachments"
	ModeSubjectOnly              = "subject_only"
	ModeImportantWithoutAttach   = "important_without_attachments"
	ModeImportantWithAttachments = "important_with_attachments"
	ModeNotifyAll                = "notify_all"
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

type linkCandidate struct {
	URL   string
	Label string
}

type importantContent struct {
	Code  string
	Lines []string
	Links []string
}

var defaultHTTPClient = &http.Client{Timeout: 25 * time.Second}

var (
	anchorRegexp     = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	urlRegexp        = regexp.MustCompile(`https?://[^\s<>"')]+`)
	whitespaceRegexp = regexp.MustCompile(`\s+`)
)

var importantKeywords = []string{
	"验证码", "校验码", "动态码", "安全码", "验证", "验证邮箱", "确认邮箱",
	"otp", "passcode", "one time", "one-time", "verify", "verification", "confirm",
	"confirmed", "activation", "activate", "reset", "password", "sign in", "signin",
	"log in", "login", "magic link", "approve", "approval", "authenticate",
	"authentication", "security", "2fa", "two-factor", "recover", "recovery",
	"邀请", "invite", "invitation", "加入", "accept invite",
}

var noiseKeywords = []string{
	"unsubscribe", "opt out", "optout", "opt-in", "opt in", "preference", "preferences",
	"privacy", "terms", "view in browser", "view online", "web version", "manage subscription",
	"facebook", "instagram", "twitter", "x.com", "tiktok", "linkedin", "youtube",
	"shop now", "browse", "sale", "discount", "offer", "newsletter", "utm_", "xnpe_",
	"mc_", "trk", "tracking", "optedout",
}

func NormalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ModeAllWithoutAttachments:
		return ModeAllWithoutAttachments
	case ModeAttachmentsOnly:
		return ModeAttachmentsOnly
	case ModeNotifyAttachments:
		return ModeNotifyAttachments
	case ModeSubjectOnly:
		return ModeSubjectOnly
	case ModeImportantWithoutAttach:
		return ModeImportantWithoutAttach
	case ModeImportantWithAttachments:
		return ModeImportantWithAttachments
	case ModeNotifyAll:
		return ModeNotifyAll
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
	return mode == ModeAllWithAttachments ||
		mode == ModeAttachmentsOnly ||
		mode == ModeImportantWithAttachments
}

func buildMessageText(mode string, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	switch mode {
	case ModeNotifyAttachments:
		return buildNotificationText("TempMail 附件通知", "收到一封带附件邮件", mailbox, email, attachments)
	case ModeNotifyAll:
		return buildNotificationText("TempMail 收件通知", "收到一封新邮件", mailbox, email, attachments)
	case ModeSubjectOnly:
		return buildSubjectOnlyText(mailbox, email, attachments)
	case ModeImportantWithoutAttach, ModeImportantWithAttachments:
		return buildImportantMessageText(mailbox, email, attachments)
	default:
		return buildFullMessageText(mailbox, email, attachments)
	}
}

func buildNotificationText(title, note string, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	var b strings.Builder
	appendMessageHeader(&b, title, mailbox, email, attachments)
	if note != "" {
		b.WriteString("\n说明: ")
		b.WriteString(note)
	}
	return b.String()
}

func buildSubjectOnlyText(mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	var b strings.Builder
	appendMessageHeader(&b, "TempMail 标题转发", mailbox, email, attachments)
	return b.String()
}

func buildImportantMessageText(mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	var b strings.Builder
	appendMessageHeader(&b, "TempMail 重点转发", mailbox, email, attachments)

	info := extractImportantContent(email)
	if info.Code != "" {
		b.WriteString("\n\n验证码:\n")
		b.WriteString(info.Code)
	}
	if len(info.Lines) > 0 {
		b.WriteString("\n\n重点正文:")
		for _, line := range info.Lines {
			b.WriteString("\n- ")
			b.WriteString(line)
		}
	}
	if len(info.Links) > 0 {
		b.WriteString("\n\n关键链接:")
		for _, link := range info.Links {
			b.WriteString("\n- ")
			b.WriteString(link)
		}
	}
	if info.Code == "" && len(info.Lines) == 0 && len(info.Links) == 0 {
		b.WriteString("\n\n说明: 未提取到关键正文，仅保留邮件头信息。")
	}
	return b.String()
}

func buildFullMessageText(mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) string {
	var b strings.Builder
	appendMessageHeader(&b, "TempMail 邮件转发", mailbox, email, attachments)

	preview := buildBodyPreview(email)
	if preview != "" {
		b.WriteString("\n\n正文预览:\n")
		b.WriteString(preview)
	}
	return b.String()
}

func appendMessageHeader(b *strings.Builder, title string, mailbox model.Mailbox, email model.Email, attachments []mailutil.ParsedAttachment) {
	b.WriteString(title)
	b.WriteString("\n邮箱: ")
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
}

func buildBodyPreview(email model.Email) string {
	preview := extractMessageText(email)
	return buildReadablePreview(preview, 1200)
}

func extractImportantContent(email model.Email) importantContent {
	text := extractMessageText(email)
	combined := strings.Join([]string{email.Subject, email.BodyText, otp.StripHTML(email.BodyHTML)}, "\n")

	info := importantContent{
		Code: otp.ExtractFromHTML(email.BodyHTML),
	}
	if info.Code == "" {
		info.Code = otp.Extract(combined)
	}

	info.Lines = selectImportantLines(text, info.Code)
	info.Links = selectImportantLinks(email)

	if len(info.Lines) == 0 {
		fallback := buildReadablePreview(text, 280)
		if fallback != "" {
			info.Lines = []string{fallback}
		}
	}
	return info
}

func extractMessageText(email model.Email) string {
	text := strings.TrimSpace(email.BodyText)
	if text == "" {
		text = otp.StripHTML(email.BodyHTML)
	}
	return html.UnescapeString(text)
}

func buildReadablePreview(text string, limit int) string {
	lines := collectTextCandidates(text)
	if len(lines) == 0 {
		return ""
	}

	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = compactPreviewLine(line)
		line = normalizeSpace(line)
		if line == "" || line == "[链接]" || isMostlyURLLine(line) || isBoilerplateLine(line) {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return ""
	}
	return truncate(strings.Join(parts, "\n"), limit)
}

func selectImportantLines(text, code string) []string {
	lines := collectTextCandidates(text)
	if len(lines) == 0 {
		return nil
	}

	selected := make([]string, 0, 3)
	for _, line := range lines {
		line = compactPreviewLine(line)
		line = normalizeSpace(line)
		if line == "" || isMostlyURLLine(line) || isBoilerplateLine(line) {
			continue
		}
		if scoreImportantLine(line, code) <= 0 {
			continue
		}
		selected = append(selected, truncate(line, 220))
		if len(selected) >= 3 {
			return selected
		}
	}

	if len(selected) > 0 {
		return selected
	}

	for _, line := range lines {
		line = compactPreviewLine(line)
		line = normalizeSpace(line)
		if line == "" || isMostlyURLLine(line) || isBoilerplateLine(line) {
			continue
		}
		selected = append(selected, truncate(line, 220))
		break
	}
	return selected
}

func collectTextCandidates(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	candidates := make([]string, 0, len(rawLines))
	seen := make(map[string]struct{})

	for _, rawLine := range rawLines {
		line := normalizeSpace(rawLine)
		if line == "" {
			continue
		}

		for _, piece := range splitLongLine(line) {
			piece = normalizeSpace(piece)
			if piece == "" {
				continue
			}
			if _, ok := seen[piece]; ok {
				continue
			}
			seen[piece] = struct{}{}
			candidates = append(candidates, piece)
		}
	}

	return candidates
}

func splitLongLine(line string) []string {
	if utf8Len(line) <= 180 {
		return []string{line}
	}

	parts := strings.FieldsFunc(line, func(r rune) bool {
		switch r {
		case '。', '！', '？', '!', '?':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return []string{line}
	}
	return parts
}

func compactPreviewLine(line string) string {
	return urlRegexp.ReplaceAllStringFunc(line, func(raw string) string {
		clean := trimURLPunctuation(raw)
		if clean == "" {
			return ""
		}
		if looksLikeTrackingURL(clean) || utf8Len(clean) > 96 {
			return "[链接]"
		}
		return clean
	})
}

func scoreImportantLine(line, code string) int {
	score := 0
	lower := strings.ToLower(line)
	if code != "" && strings.Contains(strings.ToUpper(line), strings.ToUpper(code)) {
		score += 5
	}
	if containsKeyword(lower, importantKeywords) {
		score += 3
	}
	if utf8Len(line) >= 10 && utf8Len(line) <= 180 {
		score++
	}
	if containsKeyword(lower, noiseKeywords) {
		score -= 3
	}
	if isMostlyURLLine(line) {
		score -= 4
	}
	return score
}

func selectImportantLinks(email model.Email) []string {
	candidates := collectLinkCandidates(email)
	if len(candidates) == 0 {
		return nil
	}

	context := strings.ToLower(strings.Join([]string{email.Subject, email.BodyText, otp.StripHTML(email.BodyHTML)}, "\n"))
	transactional := containsKeyword(context, importantKeywords)

	selected := make([]string, 0, 2)
	seen := make(map[string]struct{})

	for _, candidate := range candidates {
		if isStrongImportantLink(candidate) {
			if _, ok := seen[candidate.URL]; ok {
				continue
			}
			seen[candidate.URL] = struct{}{}
			selected = append(selected, formatLink(candidate))
			if len(selected) >= 2 {
				return selected
			}
		}
	}

	if len(selected) > 0 || !transactional {
		return selected
	}

	for _, candidate := range candidates {
		if _, ok := seen[candidate.URL]; ok {
			continue
		}
		if looksLikeTrackingURL(candidate.URL) || containsKeyword(strings.ToLower(candidate.Label), noiseKeywords) {
			continue
		}
		seen[candidate.URL] = struct{}{}
		selected = append(selected, formatLink(candidate))
		if len(selected) >= 2 {
			return selected
		}
	}

	return selected
}

func collectLinkCandidates(email model.Email) []linkCandidate {
	candidates := make([]linkCandidate, 0, 4)
	seen := make(map[string]struct{})

	for _, match := range anchorRegexp.FindAllStringSubmatch(email.BodyHTML, -1) {
		if len(match) < 3 {
			continue
		}
		rawURL := trimURLPunctuation(html.UnescapeString(strings.TrimSpace(match[1])))
		if rawURL == "" {
			continue
		}
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		label := normalizeSpace(html.UnescapeString(otp.StripHTML(match[2])))
		candidates = append(candidates, linkCandidate{URL: rawURL, Label: label})
	}

	text := strings.Join([]string{email.BodyText, otp.StripHTML(email.BodyHTML)}, "\n")
	for _, rawURL := range urlRegexp.FindAllString(text, -1) {
		rawURL = trimURLPunctuation(rawURL)
		if rawURL == "" {
			continue
		}
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		candidates = append(candidates, linkCandidate{URL: rawURL})
	}

	return candidates
}

func isStrongImportantLink(candidate linkCandidate) bool {
	lowerURL := strings.ToLower(candidate.URL)
	lowerLabel := strings.ToLower(candidate.Label)
	if lowerURL == "" || strings.HasPrefix(lowerURL, "mailto:") {
		return false
	}
	if containsKeyword(lowerURL, noiseKeywords) || containsKeyword(lowerLabel, noiseKeywords) {
		return false
	}
	if containsKeyword(lowerURL, importantKeywords) || containsKeyword(lowerLabel, importantKeywords) {
		return true
	}
	if looksLikeTrackingURL(candidate.URL) {
		return false
	}
	return false
}

func formatLink(candidate linkCandidate) string {
	label := normalizeSpace(candidate.Label)
	if label != "" && !strings.EqualFold(label, candidate.URL) {
		return truncate(label, 80) + ": " + candidate.URL
	}
	return candidate.URL
}

func looksLikeTrackingURL(raw string) bool {
	lower := strings.ToLower(raw)
	if containsKeyword(lower, noiseKeywords) {
		return true
	}

	parsed, err := neturl.Parse(raw)
	if err != nil {
		return false
	}
	if strings.Count(parsed.RawQuery, "&") >= 4 {
		return true
	}
	query := parsed.Query()
	if len(query) >= 4 {
		return true
	}
	return false
}

func isMostlyURLLine(line string) bool {
	urls := urlRegexp.FindAllString(line, -1)
	if len(urls) == 0 {
		return false
	}

	urlChars := 0
	for _, raw := range urls {
		urlChars += utf8Len(raw)
	}

	withoutURLs := normalizeSpace(urlRegexp.ReplaceAllString(line, " "))
	if withoutURLs == "" {
		return true
	}
	return urlChars >= utf8Len(line)/2
}

func isBoilerplateLine(line string) bool {
	lower := strings.ToLower(normalizeSpace(line))
	if lower == "" {
		return true
	}
	if containsKeyword(lower, noiseKeywords) {
		return true
	}
	if strings.HasPrefix(lower, "copyright") || strings.HasPrefix(lower, "all rights reserved") {
		return true
	}
	return false
}

func containsKeyword(lower string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func normalizeSpace(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return whitespaceRegexp.ReplaceAllString(value, " ")
}

func trimURLPunctuation(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,;:!?)\"]}>")
}

func utf8Len(value string) int {
	return len([]rune(value))
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
		"chat_id":                  cfg.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
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
