package mailutil

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"

	"tempmail/model"
)

type ParsedAttachment struct {
	model.Attachment
	Data []byte
}

type inlineResource struct {
	ContentID       string
	ContentLocation string
	Filename        string
	ContentType     string
	Data            []byte
}

type parsedAssets struct {
	attachments []ParsedAttachment
	inline      []inlineResource
}

func ParseAttachments(raw string) ([]ParsedAttachment, error) {
	assets, err := parseAssets(raw)
	if err != nil {
		return nil, err
	}
	return assets.attachments, nil
}

func ParseAttachmentsAndInlineHTML(raw, bodyHTML string) ([]ParsedAttachment, string, error) {
	assets, err := parseAssets(raw)
	if err != nil {
		return nil, bodyHTML, err
	}
	return assets.attachments, embedInlineResources(bodyHTML, assets.inline), nil
}

func parseAssets(raw string) (parsedAssets, error) {
	if strings.TrimSpace(raw) == "" {
		return parsedAssets{}, nil
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return parsedAssets{}, err
	}

	assets := parsedAssets{
		attachments: make([]ParsedAttachment, 0, 4),
		inline:      make([]inlineResource, 0, 4),
	}
	nextID := 0
	if err := collectAssets(textproto.MIMEHeader(msg.Header), msg.Body, &nextID, &assets); err != nil {
		return parsedAssets{}, err
	}
	return assets, nil
}

func collectAssets(header textproto.MIMEHeader, body io.Reader, nextID *int, assets *parsedAssets) error {
	contentType := header.Get("Content-Type")
	mediaType, typeParams, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = ""
		typeParams = map[string]string{}
	}

	disposition, dispParams, err := mime.ParseMediaType(header.Get("Content-Disposition"))
	if err != nil {
		disposition = ""
		dispParams = map[string]string{}
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := typeParams["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart message missing boundary")
		}

		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := collectAssets(part.Header, part, nextID, assets); err != nil {
				return err
			}
		}
	}

	filename := firstNonEmpty(
		dispParams["filename*"],
		dispParams["filename"],
		typeParams["name*"],
		typeParams["name"],
	)
	filename = decodeAttachmentFilename(filename)

	isExplicitAttachment := strings.EqualFold(disposition, "attachment")
	contentID := normalizeContentID(header.Get("Content-ID"))
	isInline := !isExplicitAttachment &&
		(strings.EqualFold(disposition, "inline") || contentID != "")
	isAttachment := isExplicitAttachment || (filename != "" && !isInline)
	if !isInline && !isAttachment {
		return nil
	}

	data, err := readTransferDecodedBody(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return err
	}

	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}

	if isInline {
		assets.inline = append(assets.inline, inlineResource{
			ContentID:       contentID,
			ContentLocation: strings.TrimSpace(header.Get("Content-Location")),
			Filename:        filename,
			ContentType:     mediaType,
			Data:            data,
		})
		return nil
	}

	*nextID = *nextID + 1
	if filename == "" {
		filename = fallbackAttachmentName(*nextID, mediaType)
	}
	filename = sanitizeAttachmentFilename(filename, *nextID, mediaType)

	assets.attachments = append(assets.attachments, ParsedAttachment{
		Attachment: model.Attachment{
			ID:          *nextID,
			Filename:    filename,
			ContentType: mediaType,
			SizeBytes:   len(data),
			Inline:      false,
		},
		Data: data,
	})

	return nil
}

func embedInlineResources(bodyHTML string, resources []inlineResource) string {
	if strings.TrimSpace(bodyHTML) == "" || len(resources) == 0 {
		return bodyHTML
	}

	rendered := bodyHTML
	for _, resource := range resources {
		if len(resource.Data) == 0 || !strings.HasPrefix(strings.ToLower(resource.ContentType), "image/") {
			continue
		}

		dataURL := "data:" + resource.ContentType + ";base64," +
			base64.StdEncoding.EncodeToString(resource.Data)
		if resource.ContentID != "" {
			rendered = replaceCaseInsensitive(rendered, "cid:"+resource.ContentID, dataURL)
			rendered = replaceCaseInsensitive(rendered, "cid:<"+resource.ContentID+">", dataURL)
		}
		if resource.ContentLocation != "" {
			rendered = replaceCaseInsensitive(rendered, resource.ContentLocation, dataURL)
		}
	}
	return rendered
}

func replaceCaseInsensitive(value, old, replacement string) string {
	if old == "" {
		return value
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(old))
	return re.ReplaceAllStringFunc(value, func(string) string {
		return replacement
	})
}

func normalizeContentID(value string) string {
	return strings.Trim(strings.TrimSpace(value), "<>")
}

func readTransferDecodedBody(body io.Reader, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(body))
	default:
		return io.ReadAll(body)
	}
}

func Meta(items []ParsedAttachment) []model.Attachment {
	if len(items) == 0 {
		return nil
	}

	meta := make([]model.Attachment, 0, len(items))
	for _, item := range items {
		meta = append(meta, item.Attachment)
	}
	return meta
}

func Find(items []ParsedAttachment, id int) *ParsedAttachment {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func decodeAttachmentFilename(filename string) string {
	if filename == "" || !strings.Contains(filename, "=?") {
		return filename
	}

	decoded, err := new(mime.WordDecoder).DecodeHeader(filename)
	if err != nil {
		return filename
	}
	return decoded
}

func sanitizeAttachmentFilename(filename string, id int, mediaType string) string {
	cleaned := strings.TrimSpace(filename)
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"\x00", "",
		"\r", "",
		"\n", "",
	)
	cleaned = replacer.Replace(cleaned)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return fallbackAttachmentName(id, mediaType)
	}
	return cleaned
}

func fallbackAttachmentName(id int, mediaType string) string {
	ext := ".bin"
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		ext = exts[0]
	}
	return fmt.Sprintf("attachment-%d%s", id, ext)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
