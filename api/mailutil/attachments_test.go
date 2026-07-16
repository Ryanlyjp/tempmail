package mailutil

import (
	"strings"
	"testing"
)

func TestParseAttachmentsSeparatesInlineCIDImages(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"To: original@example.com",
		"Subject: MIME test",
		"MIME-Version: 1.0",
		`Content-Type: multipart/related; boundary="related-boundary"`,
		"",
		"--related-boundary",
		"Content-Type: text/html; charset=UTF-8",
		"",
		`<html><body><img src="cid:brand-logo"></body></html>`,
		"--related-boundary",
		`Content-Type: image/png; name="logo.png"`,
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: inline; filename="logo.png"`,
		"Content-ID: <brand-logo>",
		"",
		"aGVsbG8=",
		"--related-boundary",
		`Content-Type: application/pdf; name="invoice.pdf"`,
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: attachment; filename="invoice.pdf"`,
		"",
		"cGRm",
		"--related-boundary--",
		"",
	}, "\r\n")

	attachments, renderedHTML, err := ParseAttachmentsAndInlineHTML(
		raw,
		`<html><body><img src="cid:brand-logo"></body></html>`,
	)
	if err != nil {
		t.Fatalf("ParseAttachmentsAndInlineHTML() error = %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(attachments))
	}
	if attachments[0].Filename != "invoice.pdf" {
		t.Fatalf("attachment filename = %q, want invoice.pdf", attachments[0].Filename)
	}
	if attachments[0].ID != 1 {
		t.Fatalf("attachment id = %d, want 1", attachments[0].ID)
	}
	if strings.Contains(renderedHTML, "cid:brand-logo") {
		t.Fatalf("rendered HTML still contains CID reference: %s", renderedHTML)
	}
	if !strings.Contains(renderedHTML, "data:image/png;base64,aGVsbG8=") {
		t.Fatalf("rendered HTML does not contain embedded image: %s", renderedHTML)
	}
}

func TestParseAttachmentsExcludesInlineImagesWithoutContentID(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"To: recipient@example.com",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="mixed-boundary"`,
		"",
		"--mixed-boundary",
		`Content-Type: image/png; name="signature.png"`,
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: inline; filename="signature.png"`,
		"",
		"aGVsbG8=",
		"--mixed-boundary--",
		"",
	}, "\r\n")

	attachments, err := ParseAttachments(raw)
	if err != nil {
		t.Fatalf("ParseAttachments() error = %v", err)
	}
	if len(attachments) != 0 {
		t.Fatalf("attachments count = %d, want 0", len(attachments))
	}
}

func TestExplicitAttachmentWithContentIDRemainsDownloadable(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"To: recipient@example.com",
		"MIME-Version: 1.0",
		`Content-Type: image/png; name="photo.png"`,
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: attachment; filename="photo.png"`,
		"Content-ID: <photo>",
		"",
		"aGVsbG8=",
	}, "\r\n")

	attachments, err := ParseAttachments(raw)
	if err != nil {
		t.Fatalf("ParseAttachments() error = %v", err)
	}
	if len(attachments) != 1 || attachments[0].Filename != "photo.png" {
		t.Fatalf("attachments = %#v, want photo.png", attachments)
	}
}
