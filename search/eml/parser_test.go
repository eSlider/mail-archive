package eml_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eslider/mails/search/eml"
)

func writeTestEml(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseFile_BasicHeaders(t *testing.T) {
	dir := t.TempDir()
	path := writeTestEml(t, dir, "test.eml", "From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Hello World\r\nDate: Mon, 10 Feb 2025 14:30:00 +0000\r\n\r\nBody text here.\r\n")

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Subject != "Hello World" {
		t.Errorf("subject = %q, want %q", e.Subject, "Hello World")
	}
	if e.From != "sender@example.com" {
		t.Errorf("from = %q, want %q", e.From, "sender@example.com")
	}
	if e.To != "recipient@example.com" {
		t.Errorf("to = %q, want %q", e.To, "recipient@example.com")
	}
	if e.Date.Year() != 2025 || e.Date.Month() != time.February || e.Date.Day() != 10 {
		t.Errorf("date = %v, want 2025-02-10", e.Date)
	}
}

func TestParseFile_MIMEEncodedSubject(t *testing.T) {
	dir := t.TempDir()
	path := writeTestEml(t, dir, "mime.eml", "From: test@example.com\r\nTo: dest@example.com\r\nSubject: =?UTF-8?B?SGVsbG8gV29ybGQgw7w=?=\r\nDate: Tue, 11 Feb 2025 10:00:00 +0000\r\n\r\nContent.\r\n")

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Subject != "Hello World ü" {
		t.Errorf("subject = %q, want %q", e.Subject, "Hello World ü")
	}
}

func TestParseFile_NoDateFallsBackToMtime(t *testing.T) {
	dir := t.TempDir()
	path := writeTestEml(t, dir, "nodate.eml", "From: test@example.com\r\nTo: dest@example.com\r\nSubject: No Date Header\r\n\r\nContent.\r\n")

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Date.IsZero() {
		t.Error("date should not be zero (should fall back to mtime)")
	}
}

func TestParseFile_NonExistent(t *testing.T) {
	_, err := eml.ParseFile("/nonexistent/file.eml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseFile_PlainTextBody(t *testing.T) {
	dir := t.TempDir()
	path := writeTestEml(t, dir, "plain.eml", "From: a@b.com\r\nTo: c@d.com\r\nSubject: Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nThis is the body text with a unique keyword xylophone.\r\n")

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "xylophone") {
		t.Errorf("body should contain 'xylophone', got %q", e.BodyText)
	}
}

func TestParseFile_MultipartBody(t *testing.T) {
	dir := t.TempDir()
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Multipart\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: multipart/alternative; boundary=\"BOUND\"\r\n\r\n--BOUND\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain text body with marshmallow.\r\n--BOUND\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><body><p>HTML body</p></body></html>\r\n--BOUND--\r\n"
	path := writeTestEml(t, dir, "multi.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should prefer text/plain.
	if !strings.Contains(e.BodyText, "marshmallow") {
		t.Errorf("body should contain 'marshmallow', got %q", e.BodyText)
	}
}

func TestParseFile_HTMLOnlyBody(t *testing.T) {
	dir := t.TempDir()
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: HTML only\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><body><p>Visible text with kaleidoscope</p><script>hidden();</script></body></html>\r\n"
	path := writeTestEml(t, dir, "html.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "kaleidoscope") {
		t.Errorf("body should contain 'kaleidoscope', got %q", e.BodyText)
	}
	if strings.Contains(e.BodyText, "hidden()") {
		t.Error("body should not contain script content")
	}
}

func TestParseFile_Base64Body(t *testing.T) {
	dir := t.TempDir()
	// "Hello base64 trampoline" in base64
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: B64\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gYmFzZTY0IHRyYW1wb2xpbmU=\r\n"
	path := writeTestEml(t, dir, "b64.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "trampoline") {
		t.Errorf("body should contain 'trampoline', got %q", e.BodyText)
	}
}

func TestParseFile_QuotedPrintableBody(t *testing.T) {
	dir := t.TempDir()
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: QP\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello quoted-printable =C3=BC (=3D umlaut)\r\n"
	path := writeTestEml(t, dir, "qp.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "ü") {
		t.Errorf("body should contain decoded ü, got %q", e.BodyText)
	}
}

func TestSnippet_SubjectMatch(t *testing.T) {
	e := eml.Email{Subject: "Meeting about project alpha", BodyText: "Some body text."}
	s := eml.Snippet(e, "alpha", 20)
	if !strings.Contains(s, "alpha") {
		t.Errorf("snippet should contain 'alpha', got %q", s)
	}
}

func TestSnippet_BodyMatch(t *testing.T) {
	e := eml.Email{Subject: "Unrelated subject", BodyText: "The quick brown fox jumps over the lazy dog and finds a trampoline in the garden."}
	s := eml.Snippet(e, "trampoline", 15)
	if !strings.Contains(s, "trampoline") {
		t.Errorf("snippet should contain 'trampoline', got %q", s)
	}
	// Should have ellipsis since match is in the middle.
	if !strings.HasPrefix(s, "...") {
		t.Errorf("snippet should start with '...', got %q", s)
	}
}

func TestSnippet_NoMatch(t *testing.T) {
	e := eml.Email{Subject: "Hello", BodyText: "World"}
	s := eml.Snippet(e, "zzz", 20)
	if s != "" {
		t.Errorf("snippet should be empty for no match, got %q", s)
	}
}

// --- ParseFileFull tests ---

func TestParseFileFull_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := writeTestEml(t, dir, "full-plain.eml",
		"From: alice@test.com\r\nTo: bob@test.com\r\nCc: carol@test.com\r\nReply-To: alice-reply@test.com\r\nSubject: Full Parse\r\nDate: Mon, 10 Feb 2025 09:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain body content.\r\n")

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fe.Subject != "Full Parse" {
		t.Errorf("subject = %q", fe.Subject)
	}
	if fe.CC != "carol@test.com" {
		t.Errorf("cc = %q, want carol@test.com", fe.CC)
	}
	if fe.ReplyTo != "alice-reply@test.com" {
		t.Errorf("reply_to = %q", fe.ReplyTo)
	}
	if !strings.Contains(fe.TextBody, "Plain body content") {
		t.Errorf("text_body = %q", fe.TextBody)
	}
	if fe.HTMLBody != "" {
		t.Errorf("html_body should be empty for plain-text email, got %q", fe.HTMLBody)
	}
}

func TestParseFileFull_MultipartWithHTML(t *testing.T) {
	dir := t.TempDir()
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Full Multi\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: multipart/alternative; boundary=\"ALT\"\r\n\r\n--ALT\r\nContent-Type: text/plain\r\n\r\nPlain version.\r\n--ALT\r\nContent-Type: text/html\r\n\r\n<html><body><b>HTML version</b></body></html>\r\n--ALT--\r\n"
	path := writeTestEml(t, dir, "full-multi.eml", raw)

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fe.TextBody, "Plain version") {
		t.Errorf("text_body = %q", fe.TextBody)
	}
	if !strings.Contains(fe.HTMLBody, "<b>HTML version</b>") {
		t.Errorf("html_body = %q", fe.HTMLBody)
	}
}

func TestParseFileFull_Attachments(t *testing.T) {
	dir := t.TempDir()
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: With Attachment\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: multipart/mixed; boundary=\"MIX\"\r\n\r\n--MIX\r\nContent-Type: text/plain\r\n\r\nSee attached.\r\n--MIX\r\nContent-Type: application/pdf; name=\"report.pdf\"\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQKMSAwIG9iago=\r\n--MIX--\r\n"
	path := writeTestEml(t, dir, "full-attach.eml", raw)

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fe.TextBody, "See attached") {
		t.Errorf("text_body = %q", fe.TextBody)
	}
	if len(fe.Attachments) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(fe.Attachments))
	}
	if fe.Attachments[0].Filename != "report.pdf" {
		t.Errorf("attachment filename = %q", fe.Attachments[0].Filename)
	}
	if fe.Attachments[0].ContentType != "application/pdf" {
		t.Errorf("attachment content_type = %q", fe.Attachments[0].ContentType)
	}
}

func TestParseFileFull_NonExistent(t *testing.T) {
	_, err := eml.ParseFileFull("/nonexistent/file.eml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// --- Charset decoding tests ---

func TestParseFile_ISO88591Body(t *testing.T) {
	dir := t.TempDir()
	// "Sch\xf6ne Gr\xfc\xdfe" is "Schöne Grüße" in ISO-8859-1
	body := "Sch\xf6ne Gr\xfc\xdfe aus Berlin.\r\n"
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\n" + body
	path := writeTestEml(t, dir, "latin1.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "Schöne Grüße") {
		t.Errorf("body should contain 'Schöne Grüße', got %q", e.BodyText)
	}
}

func TestParseFile_Windows1252Body(t *testing.T) {
	dir := t.TempDir()
	// \x93 and \x94 are left/right double quotes in Windows-1252
	body := "\x93Hello World\x94\r\n"
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=windows-1252\r\n\r\n" + body
	path := writeTestEml(t, dir, "cp1252.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.BodyText, "\u201cHello World\u201d") {
		t.Errorf("body should contain smart quotes, got %q", e.BodyText)
	}
}

func TestParseFile_KOI8REncodedSubject(t *testing.T) {
	dir := t.TempDir()
	// Real-world KOI8-R MIME-encoded subject: "eSlider ПРЕДЛАГАЕТ Вам ЗАВЕСТИ АДРЕС В Почтовой Службе Google Mail."
	raw := "From: eslider@gmail.com\r\nTo: test@example.com\r\n" +
		"Subject: =?KOI8-R?B?ZVNsaWRlciDQ0sXEzMHHwcXUIPfBzSDawdfF09TJIMHE?=\r\n" +
		" =?KOI8-R?B?0sXTINcg0M/e1M/Xz8og08zV1sLFIEdvb2dsZSBNYWlsLg==?=\r\n" +
		"Date: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=koi8-r\r\n\r\nBody.\r\n"
	path := writeTestEml(t, dir, "koi8r.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.Subject, "eSlider") {
		t.Errorf("subject should contain 'eSlider', got %q", e.Subject)
	}
	if !strings.Contains(e.Subject, "предлагает") {
		t.Errorf("subject should contain 'предлагает', got %q", e.Subject)
	}
	if !strings.Contains(e.Subject, "Google Mail") {
		t.Errorf("subject should contain 'Google Mail', got %q", e.Subject)
	}
}

func TestParseFileFull_KOI8REncodedSubject(t *testing.T) {
	dir := t.TempDir()
	raw := "From: eslider@gmail.com\r\nTo: test@example.com\r\n" +
		"Subject: =?KOI8-R?B?ZVNsaWRlciDQ0sXEzMHHwcXUIPfBzSDawdfF09TJIMHE?=\r\n" +
		" =?KOI8-R?B?0sXTINcg0M/e1M/Xz8og08zV1sLFIEdvb2dsZSBNYWlsLg==?=\r\n" +
		"Date: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain; charset=koi8-r\r\n\r\nBody.\r\n"
	path := writeTestEml(t, dir, "koi8r-full.eml", raw)

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fe.Subject, "предлагает") {
		t.Errorf("subject should contain 'предлагает', got %q", fe.Subject)
	}
}

func TestParseFile_RawNonUTF8Header(t *testing.T) {
	dir := t.TempDir()
	// Subject with raw ISO-8859-1 bytes (no MIME encoding) — "Grüße"
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Gr\xfc\xdfe\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\n\r\nBody.\r\n"
	path := writeTestEml(t, dir, "rawheader.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ensureUTF8 should decode Windows-1252 fallback to get "Grüße"
	if !strings.Contains(e.Subject, "Grüße") {
		t.Errorf("subject should contain 'Grüße', got %q", e.Subject)
	}
}

func TestParseFile_NoContentTypeLatinBody(t *testing.T) {
	dir := t.TempDir()
	// Email with no Content-Type header at all, body has raw Latin-1 bytes.
	raw := "From: support@hetzner.de\r\nTo: test@example.com\r\nSubject: Auftragsbest\xe4tigung\r\nDate: Mon, 31 Oct 2005 17:37:02 +0100\r\n\r\nbaldm\xf6glichst ausf\xfchren.\r\nMit freundlichen Gr\xfc\xdfen\r\n"
	path := writeTestEml(t, dir, "no-ct.eml", raw)

	e, err := eml.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(e.Subject, "Auftragsbestätigung") {
		t.Errorf("subject should contain 'Auftragsbestätigung', got %q", e.Subject)
	}
	if !strings.Contains(e.BodyText, "baldmöglichst ausführen") {
		t.Errorf("body should contain 'baldmöglichst ausführen', got %q", e.BodyText)
	}
	if !strings.Contains(e.BodyText, "Grüßen") {
		t.Errorf("body should contain 'Grüßen', got %q", e.BodyText)
	}
}

func TestParseFileFull_NoContentTypeLatinBody(t *testing.T) {
	dir := t.TempDir()
	raw := "From: support@hetzner.de\r\nTo: test@example.com\r\nSubject: Test\r\nDate: Mon, 31 Oct 2005 17:37:02 +0100\r\n\r\nMit freundlichen Gr\xfc\xdfen\r\n"
	path := writeTestEml(t, dir, "full-no-ct.eml", raw)

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fe.TextBody, "Grüßen") {
		t.Errorf("text_body should contain 'Grüßen', got %q", fe.TextBody)
	}
}

func TestParseFileFull_ISO88591MultipartHTML(t *testing.T) {
	dir := t.TempDir()
	htmlBody := "<html><body><p>Sch\xf6ne Gr\xfc\xdfe</p></body></html>"
	raw := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\n" +
		"Content-Type: multipart/alternative; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\nSch\xf6ne Gr\xfc\xdfe\r\n" +
		"--B\r\nContent-Type: text/html; charset=iso-8859-1\r\n\r\n" + htmlBody + "\r\n" +
		"--B--\r\n"
	path := writeTestEml(t, dir, "full-latin1.eml", raw)

	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fe.TextBody, "Schöne Grüße") {
		t.Errorf("text_body should contain 'Schöne Grüße', got %q", fe.TextBody)
	}
	if !strings.Contains(fe.HTMLBody, "Schöne Grüße") {
		t.Errorf("html_body should contain 'Schöne Grüße', got %q", fe.HTMLBody)
	}
}
