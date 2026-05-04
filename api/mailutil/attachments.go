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
	"strings"

	"tempmail/model"
)

type ParsedAttachment struct {
	model.Attachment
	Data []byte
}

func ParseAttachments(raw string) ([]ParsedAttachment, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}

	items := make([]ParsedAttachment, 0, 4)
	nextID := 0
	if err := collectAttachments(textproto.MIMEHeader(msg.Header), msg.Body, &nextID, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func collectAttachments(header textproto.MIMEHeader, body io.Reader, nextID *int, items *[]ParsedAttachment) error {
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
			if err := collectAttachments(part.Header, part, nextID, items); err != nil {
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

	isInline := strings.EqualFold(disposition, "inline")
	isAttachment := strings.EqualFold(disposition, "attachment") ||
		(filename != "" && strings.EqualFold(disposition, "inline")) ||
		(filename != "" && disposition == "")
	if !isAttachment {
		return nil
	}

	data, err := readTransferDecodedBody(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return err
	}

	*nextID = *nextID + 1
	if filename == "" {
		filename = fallbackAttachmentName(*nextID, mediaType)
	}
	filename = sanitizeAttachmentFilename(filename, *nextID, mediaType)

	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}

	*items = append(*items, ParsedAttachment{
		Attachment: model.Attachment{
			ID:          *nextID,
			Filename:    filename,
			ContentType: mediaType,
			SizeBytes:   len(data),
			Inline:      isInline,
		},
		Data: data,
	})

	return nil
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
