// Package eml provides RFC 822 / .eml file header parsing and body text extraction.
package eml

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// maxBodyBytes caps stored body text per email to avoid pathological memory use.
const maxBodyBytes = 64 * 1024

// Email holds the parsed metadata and body text from a single .eml file.
type Email struct {
	Path    string    `json:"path"`
	Subject string    `json:"subject"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Date    time.Time `json:"date"`
	Size    int64     `json:"size"`

	// BodyText is the extracted plain text body, used for search.
	// Hidden from JSON serialisation — callers add a snippet instead.
	BodyText string `json:"-"`
}

var decoder = &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		cs := strings.ToLower(strings.TrimSpace(charset))
		if cs == "utf-8" || cs == "us-ascii" || cs == "ascii" {
			return input, nil
		}
		enc, err := htmlindex.Get(cs)
		if err != nil {
			return nil, fmt.Errorf("unsupported charset %q: %w", charset, err)
		}
		return transform.NewReader(input, enc.NewDecoder()), nil
	},
}

// decodeHeader decodes a possibly MIME-encoded header value.
func decodeHeader(raw string) string {
	decoded, err := decoder.DecodeHeader(raw)
	if err != nil {
		return raw
	}
	return decoded
}

// ParseFile reads an .eml file, extracts header metadata and body text.
func ParseFile(path string) (Email, error) {
	f, err := os.Open(path)
	if err != nil {
		return Email{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Email{}, fmt.Errorf("stat %s: %w", path, err)
	}

	msg, err := mail.ReadMessage(bufio.NewReader(f))
	if err != nil {
		return Email{}, fmt.Errorf("parse %s: %w", path, err)
	}

	h := msg.Header
	date, _ := h.Date()
	if date.IsZero() {
		date = parseDateFuzzy(h.Get("Date"))
	}
	if date.IsZero() {
		date = parseReceivedDate(textproto.MIMEHeader(h))
	}
	if date.IsZero() {
		date = info.ModTime()
	}

	subject := ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("Subject"))))
	from := ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("From"))))
	to := ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("To"))))

	bodyText := extractBodyText(h.Get("Content-Type"), h.Get("Content-Transfer-Encoding"), msg.Body)

	return Email{
		Path:     path,
		Subject:  subject,
		From:     from,
		To:       to,
		Date:     date,
		Size:     info.Size(),
		BodyText: bodyText,
	}, nil
}

// parseDateFuzzy tries multiple date layouts to handle non-standard Date headers
// (e.g. missing timezone, unconventional formats).
func parseDateFuzzy(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	// Try common non-standard date formats.
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05",
		"2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05",
		time.RFC822Z,
		time.RFC822,
		"Mon, 02 Jan 2006 15:04:05 -0700 (MST)",
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"01-02-2006",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseReceivedDate extracts the date from the first (most recent) Received header.
// Format: "Received: ... ; <date>"
func parseReceivedDate(h textproto.MIMEHeader) time.Time {
	received := h.Values("Received")
	if len(received) == 0 {
		return time.Time{}
	}
	// The most recent Received header is the first one. The date is after the last ";".
	for _, r := range received {
		idx := strings.LastIndex(r, ";")
		if idx < 0 {
			continue
		}
		dateStr := strings.TrimSpace(r[idx+1:])
		for _, layout := range []string{
			time.RFC1123Z,
			time.RFC1123,
			"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
			"Mon, 2 Jan 2006 15:04:05 -0700",
			"2 Jan 2006 15:04:05 -0700",
			time.RFC822Z,
			time.RFC822,
		} {
			if t, err := time.Parse(layout, dateStr); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// extractBodyText walks the MIME structure and returns the first usable
// plain text body. Falls back to stripped HTML if no text/plain part exists.
func extractBodyText(contentType, transferEncoding string, body io.Reader) string {
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return readLimited(body, transferEncoding, "")
	}

	charset := params["charset"]

	if strings.HasPrefix(mediaType, "multipart/") {
		return extractFromMultipart(params["boundary"], body)
	}

	raw := readLimited(body, transferEncoding, charset)
	if mediaType == "text/html" {
		return stripHTML(raw)
	}
	return raw
}

// extractFromMultipart recursively walks multipart MIME parts.
func extractFromMultipart(boundary string, r io.Reader) string {
	if boundary == "" {
		return ""
	}
	mr := multipart.NewReader(r, boundary)

	var htmlFallback string

	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		cte := part.Header.Get("Content-Transfer-Encoding")
		if ct == "" {
			ct = "text/plain"
		}
		partMedia, partParams, parseErr := mime.ParseMediaType(ct)
		if parseErr != nil {
			part.Close()
			continue
		}

		charset := partParams["charset"]

		if strings.HasPrefix(partMedia, "multipart/") {
			if text := extractFromMultipart(partParams["boundary"], part); text != "" {
				part.Close()
				return text
			}
			part.Close()
			continue
		}

		if partMedia == "text/plain" {
			text := readLimited(part, cte, charset)
			part.Close()
			if text != "" {
				return text
			}
			continue
		}

		if partMedia == "text/html" && htmlFallback == "" {
			htmlFallback = stripHTML(readLimited(part, cte, charset))
		}

		part.Close()
	}

	return htmlFallback
}

// readLimited reads up to maxBodyBytes from r, applying transfer-encoding and charset decoding.
func readLimited(r io.Reader, transferEncoding, charset string) string {
	r = decodeTransferEncoding(r, transferEncoding)
	r = charsetReader(charset, r)
	limited := io.LimitReader(r, maxBodyBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	return ensureUTF8(s)
}

func decodeTransferEncoding(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func charsetReader(charset string, r io.Reader) io.Reader {
	cs := strings.ToLower(strings.TrimSpace(charset))
	if cs == "" || cs == "utf-8" || cs == "us-ascii" || cs == "ascii" {
		return r
	}
	enc, err := htmlindex.Get(cs)
	if err != nil || enc == nil {
		return r
	}
	return transform.NewReader(r, enc.NewDecoder())
}

func ensureUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	enc, err := htmlindex.Get("windows-1252")
	if err != nil || enc == nil {
		return s
	}
	decoded, _, err := transform.String(enc.NewDecoder(), s)
	if err != nil {
		return s
	}
	return decoded
}

var (
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reHTMLTag    = regexp.MustCompile(`<[^>]*>`)
	reWhitespace = regexp.MustCompile(`[\s]+`)
	reHTMLEntity = regexp.MustCompile(`&[a-zA-Z0-9#]+;`)
	// reCID matches cid: URLs in HTML (img src, style url(), etc). Captures the CID value with/without angle brackets.
	reCID = regexp.MustCompile(`(?i)cid:(<[^>]+>|[^"')\s\]>]+)`)
)

func stripHTML(html string) string {
	text := reStyle.ReplaceAllString(html, " ")
	text = reScript.ReplaceAllString(text, " ")
	text = reHTMLTag.ReplaceAllString(text, " ")
	text = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&apos;", "'", "&#39;", "'",
		"&nbsp;", " ",
	).Replace(text)
	text = reHTMLEntity.ReplaceAllString(text, " ")
	text = reWhitespace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// normalizeCID returns the Content-ID for map lookup (strips angle brackets, trims).
func normalizeCID(cid string) string {
	cid = strings.TrimSpace(cid)
	if strings.HasPrefix(cid, "<") && strings.HasSuffix(cid, ">") {
		cid = strings.TrimSpace(cid[1 : len(cid)-1])
	}
	return cid
}

// rewriteCIDsInHTML replaces cid: references in HTML with data: base64 URIs using the inline parts map.
func rewriteCIDsInHTML(html string, inline map[string]inlinePart) string {
	if len(inline) == 0 {
		return html
	}
	return reCID.ReplaceAllStringFunc(html, func(match string) string {
		subs := reCID.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		cid := normalizeCID(subs[1])
		part, ok := inline[cid]
		if !ok {
			return match
		}
		ct := part.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(part.Data)
	})
}

// Attachment holds metadata about a MIME attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// inlinePart holds data for a Content-ID referenced part (e.g. inline image).
type inlinePart struct {
	Data        []byte
	ContentType string
}

// FullEmail holds the complete parsed email for display, including HTML body.
type FullEmail struct {
	Path        string       `json:"path"`
	Subject     string       `json:"subject"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	CC          string       `json:"cc,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"`
	Date        time.Time    `json:"date"`
	Size        int64        `json:"size"`
	TextBody    string       `json:"text_body"`
	HTMLBody    string       `json:"html_body,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// ParseFileFull reads an .eml and returns complete content for preview.
func ParseFileFull(path string) (FullEmail, error) {
	f, err := os.Open(path)
	if err != nil {
		return FullEmail{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FullEmail{}, fmt.Errorf("stat %s: %w", path, err)
	}

	msg, err := mail.ReadMessage(bufio.NewReader(f))
	if err != nil {
		return FullEmail{}, fmt.Errorf("parse %s: %w", path, err)
	}

	h := msg.Header
	date, _ := h.Date()
	if date.IsZero() {
		date = parseDateFuzzy(h.Get("Date"))
	}
	if date.IsZero() {
		date = parseReceivedDate(textproto.MIMEHeader(h))
	}
	if date.IsZero() {
		date = info.ModTime()
	}

	fe := FullEmail{
		Path:    path,
		Subject: ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("Subject")))),
		From:    ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("From")))),
		To:      ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("To")))),
		CC:      ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("Cc")))),
		ReplyTo: ensureUTF8(strings.TrimSpace(decodeHeader(h.Get("Reply-To")))),
		Date:    date,
		Size:    info.Size(),
	}

	ct := h.Get("Content-Type")
	cte := h.Get("Content-Transfer-Encoding")
	extractFullBody(ct, cte, msg.Body, &fe)

	return fe, nil
}

func extractFullBody(contentType, transferEncoding string, body io.Reader, fe *FullEmail) {
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		fe.TextBody = readLimited(body, transferEncoding, "")
		return
	}

	charset := params["charset"]

	if strings.HasPrefix(mediaType, "multipart/") {
		inlineParts := make(map[string]inlinePart)
		extractFullMultipart(params["boundary"], body, fe, inlineParts)
		if fe.HTMLBody != "" && len(inlineParts) > 0 {
			fe.HTMLBody = rewriteCIDsInHTML(fe.HTMLBody, inlineParts)
		}
		return
	}

	raw := readLimited(body, transferEncoding, charset)
	if mediaType == "text/html" {
		fe.HTMLBody = raw
		fe.TextBody = stripHTML(raw)
	} else {
		fe.TextBody = raw
	}
}

func extractFullMultipart(boundary string, r io.Reader, fe *FullEmail, inlineParts map[string]inlinePart) {
	if boundary == "" {
		return
	}
	mr := multipart.NewReader(r, boundary)

	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		ct := part.Header.Get("Content-Type")
		cte := part.Header.Get("Content-Transfer-Encoding")
		if ct == "" {
			ct = "text/plain"
		}
		partMedia, partParams, parseErr := mime.ParseMediaType(ct)
		if parseErr != nil {
			part.Close()
			continue
		}

		charset := partParams["charset"]

		disposition := part.Header.Get("Content-Disposition")
		isAttachment := strings.HasPrefix(disposition, "attachment") ||
			(part.FileName() != "" && !strings.HasPrefix(partMedia, "text/"))

		if isAttachment {
			data, _ := io.ReadAll(io.LimitReader(part, 10*1024*1024))
			fe.Attachments = append(fe.Attachments, Attachment{
				Filename:    ensureUTF8(decodeHeader(part.FileName())),
				ContentType: partMedia,
				Size:        len(data),
			})
			part.Close()
			continue
		}

		if strings.HasPrefix(partMedia, "multipart/") {
			extractFullMultipart(partParams["boundary"], part, fe, inlineParts)
			part.Close()
			continue
		}

		if partMedia == "text/plain" && fe.TextBody == "" {
			fe.TextBody = readLimited(part, cte, charset)
			part.Close()
			continue
		}

		if partMedia == "text/html" && fe.HTMLBody == "" {
			fe.HTMLBody = readLimited(part, cte, charset)
			if fe.TextBody == "" {
				fe.TextBody = stripHTML(fe.HTMLBody)
			}
			part.Close()
			continue
		}

		// Inline part with Content-ID (e.g. embedded image referenced by cid: in HTML).
		contentID := strings.TrimSpace(part.Header.Get("Content-ID"))
		if contentID != "" {
			data, _ := io.ReadAll(io.LimitReader(decodeTransferEncoding(part, cte), 5*1024*1024))
			cid := normalizeCID(contentID)
			if cid != "" {
				inlineParts[cid] = inlinePart{Data: data, ContentType: partMedia}
			}
		}

		part.Close()
	}
}

// ExtractPartByCID reads a MIME part by Content-ID from an .eml file.
// Returns the raw bytes, content-type, and any error. Used for serving inline images via API.
func ExtractPartByCID(path string, cid string) ([]byte, string, error) {
	cid = normalizeCID(cid)
	if cid == "" {
		return nil, "", fmt.Errorf("empty content-id")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	msg, err := mail.ReadMessage(bufio.NewReader(f))
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}

	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = "text/plain"
		params = nil
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, "", fmt.Errorf("email has no multipart structure")
	}

	boundary := params["boundary"]
	if boundary == "" {
		return nil, "", fmt.Errorf("multipart missing boundary")
	}

	var data []byte
	var contentType string
	extractPartByCID(multipart.NewReader(msg.Body, boundary), cid, &data, &contentType)
	if data == nil {
		return nil, "", fmt.Errorf("content-id %q not found", cid)
	}
	return data, contentType, nil
}

func extractPartByCID(mr *multipart.Reader, targetCID string, outData *[]byte, outContentType *string) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		cte := part.Header.Get("Content-Transfer-Encoding")
		if ct == "" {
			ct = "text/plain"
		}
		partMedia, partParams, _ := mime.ParseMediaType(ct)

		contentID := part.Header.Get("Content-ID")
		if normalizeCID(contentID) == targetCID {
			decoded := decodeTransferEncoding(part, cte)
			data, _ := io.ReadAll(io.LimitReader(decoded, 10*1024*1024))
			part.Close()
			*outData = data
			*outContentType = partMedia
			return
		}

		if strings.HasPrefix(partMedia, "multipart/") && partParams["boundary"] != "" {
			extractPartByCID(multipart.NewReader(part, partParams["boundary"]), targetCID, outData, outContentType)
			part.Close()
			if *outData != nil {
				return
			}
			continue
		}
		part.Close()
	}
}

// ExtractAttachment reads the Nth attachment (0-based index) from an .eml file.
// Returns the raw bytes, content-type, filename, and any error.
func ExtractAttachment(path string, index int) ([]byte, string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	msg, err := mail.ReadMessage(bufio.NewReader(f))
	if err != nil {
		return nil, "", "", fmt.Errorf("parse %s: %w", path, err)
	}

	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = "text/plain"
		params = nil
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, "", "", fmt.Errorf("email has no attachments")
	}

	boundary := params["boundary"]
	if boundary == "" {
		return nil, "", "", fmt.Errorf("multipart missing boundary")
	}

	var data []byte
	var contentType, filename string
	var found bool
	var idx int
	extractPartByIndex(multipart.NewReader(msg.Body, boundary), index, &idx, &data, &contentType, &filename, &found)
	if !found {
		return nil, "", "", fmt.Errorf("attachment index %d out of range", index)
	}
	return data, contentType, filename, nil
}

func extractPartByIndex(mr *multipart.Reader, targetIndex int, currentIndex *int, outData *[]byte, outContentType, outFilename *string, found *bool) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		cte := part.Header.Get("Content-Transfer-Encoding")
		if ct == "" {
			ct = "text/plain"
		}
		partMedia, partParams, _ := mime.ParseMediaType(ct)
		disposition := part.Header.Get("Content-Disposition")
		isAttachment := strings.HasPrefix(disposition, "attachment") ||
			(part.FileName() != "" && !strings.HasPrefix(partMedia, "text/"))

		if isAttachment {
			if *currentIndex == targetIndex {
				decoded := decodeTransferEncoding(part, cte)
				data, readErr := io.ReadAll(io.LimitReader(decoded, 50*1024*1024))
				part.Close()
				if readErr != nil {
					return
				}
				*outData = data
				*outContentType = partMedia
				*outFilename = ensureUTF8(decodeHeader(part.FileName()))
				*found = true
				return
			}
			*currentIndex++
			part.Close()
			continue
		}

		if strings.HasPrefix(partMedia, "multipart/") && partParams["boundary"] != "" {
			extractPartByIndex(multipart.NewReader(part, partParams["boundary"]), targetIndex, currentIndex, outData, outContentType, outFilename, found)
			part.Close()
			if *found {
				return
			}
			continue
		}
		part.Close()
	}
}

// Snippet returns a short context window around the first occurrence of query.
func Snippet(e Email, query string, contextLen int) string {
	q := strings.ToLower(query)
	if q == "" {
		return ""
	}

	subjectRunes := []rune(strings.ToLower(e.Subject))
	queryRunes := []rune(q)
	if idx := runeIndex(subjectRunes, queryRunes); idx >= 0 {
		return buildSnippet([]rune(e.Subject), idx, len(queryRunes), contextLen)
	}

	bodyRunes := []rune(strings.ToLower(e.BodyText))
	if idx := runeIndex(bodyRunes, queryRunes); idx >= 0 {
		return buildSnippet([]rune(e.BodyText), idx, len(queryRunes), contextLen)
	}

	return ""
}

func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func buildSnippet(runes []rune, matchStart, matchLen, contextLen int) string {
	start := matchStart - contextLen
	if start < 0 {
		start = 0
	}
	end := matchStart + matchLen + contextLen
	if end > len(runes) {
		end = len(runes)
	}
	s := string(runes[start:end])
	s = reWhitespace.ReplaceAllString(s, " ")

	var buf bytes.Buffer
	if start > 0 {
		buf.WriteString("...")
	}
	buf.WriteString(s)
	if end < len(runes) {
		buf.WriteString("...")
	}
	return buf.String()
}
