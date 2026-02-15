// Package pst implements PST/OST file import.
// Extracts messages, contacts, appointments, and notes from Microsoft Outlook
// personal storage files. Emails as .eml, contacts as .vcf, calendars as .ics,
// notes as .txt — stored in the same folder hierarchy as .eml files.
package pst

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"

	"golang.org/x/text/encoding"

	charsets "github.com/emersion/go-message/charset"
)

// MAPI property IDs for common item properties (PidTagSubject, PidTagBody).
const (
	mapiTagSubject = 55
	mapiTagBody    = 4096
)

func init() {
	// Register extended charsets for go-pst.
	pst.ExtendCharsets(func(name string, enc encoding.Encoding) {
		charsets.RegisterEncoding(name, enc)
	})
}

// ProgressFunc receives progress updates during PST import.
type ProgressFunc func(phase string, current, total int)

// Import extracts all messages from a PST/OST file and saves them as .eml files.
// Uses go-pst first; if it panics or fails, falls back to readpst (from pst-utils)
// when available for broader OST compatibility.
// Returns (extracted count, error count).
func Import(pstPath, emailDir string, onProgress ProgressFunc) (int, int, error) {
	if onProgress == nil {
		onProgress = func(string, int, int) {}
	}

	var extracted, errCount int
	var importErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				importErr = fmt.Errorf("go-pst panic: %v", r)
			}
		}()
		extracted, errCount, importErr = importGoPst(pstPath, emailDir, onProgress)
	}()

	if importErr == nil {
		return extracted, errCount, nil
	}

	// Fallback to readpst when go-pst fails (e.g. newer OST formats, btree bugs).
	log.Printf("INFO: go-pst failed (%v), trying readpst fallback", importErr)
	return importReadpst(pstPath, emailDir, onProgress)
}

func importGoPst(pstPath, emailDir string, onProgress ProgressFunc) (int, int, error) {
	f, err := os.Open(pstPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open PST: %w", err)
	}
	defer f.Close()

	pstFile, err := pst.New(f)
	if err != nil {
		return 0, 0, fmt.Errorf("parse PST: %w", err)
	}
	defer pstFile.Cleanup()

	var extracted, errCount int

	onProgress("extracting", 0, 0)

	if err := pstFile.WalkFolders(func(folder *pst.Folder) error {
		folderPath := sanitizeFolderName(folder.Name)
		dir := filepath.Join(emailDir, folderPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		iter, err := folder.GetMessageIterator()
		if eris.Is(err, pst.ErrMessagesNotFound) {
			return nil
		} else if err != nil {
			log.Printf("WARN: PST folder %q: %v", folder.Name, err)
			return nil
		}

		for iter.Next() {
			msg := iter.Value()
			data, ext, date := itemToStoredFormat(msg, folderPath)
			if data == nil {
				errCount++
				continue
			}

			checksum := contentChecksum(data)
			filename := fmt.Sprintf("%s-%d.%s", checksum, extracted, ext)
			path := filepath.Join(dir, filename)

			if err := os.WriteFile(path, data, 0o644); err != nil {
				log.Printf("WARN: write %s: %v", path, err)
				errCount++
				continue
			}

			if !date.IsZero() {
				os.Chtimes(path, date, date)
			}

			extracted++
			if extracted%100 == 0 {
				onProgress("extracting", extracted, 0)
			}
		}

		if iter.Err() != nil {
			log.Printf("WARN: PST iterator %q: %v", folder.Name, iter.Err())
		}

		return nil
	}); err != nil {
		return extracted, errCount, fmt.Errorf("walk PST: %w", err)
	}

	onProgress("done", extracted, extracted)
	return extracted, errCount, nil
}

// itemToStoredFormat converts a PST item to the appropriate storage format.
// Returns (data, ext, date). ext is "eml", "vcf", "ics", or "txt".
func itemToStoredFormat(msg *pst.Message, folderPath string) ([]byte, string, time.Time) {
	switch p := msg.Properties.(type) {
	case *properties.Message:
		if strings.Contains(folderPath, "note") {
			return messageToNoteTxt(p), "txt", messageDate(p.GetClientSubmitTime(), p.GetMessageDeliveryTime())
		}
		return messageToEML(p), "eml", messageDate(p.GetClientSubmitTime(), p.GetMessageDeliveryTime())
	case *properties.Appointment:
		return appointmentToICS(msg, p), "ics", appointmentDate(p)
	case *properties.Contact:
		return contactToVCF(p), "vcf", contactDate(p)
	default:
		return nil, "", time.Time{}
	}
}

func messageDate(clientSubmit, messageDelivery int64) time.Time {
	if clientSubmit > 0 {
		return time.Unix(clientSubmit, 0)
	}
	if messageDelivery > 0 {
		return time.Unix(messageDelivery, 0)
	}
	return time.Now()
}

func messageToEML(p *properties.Message) []byte {
	subject := p.GetSubject()
	from := formatSender(p.GetSenderName(), p.GetSenderEmailAddress())
	to := p.GetDisplayTo()
	body := p.GetBody()
	date := messageDate(p.GetClientSubmitTime(), p.GetMessageDeliveryTime())
	dateStr := date.Format(time.RFC1123Z)

	var sb strings.Builder
	sb.WriteString("From: " + escapeHeader(from) + "\r\n")
	sb.WriteString("To: " + escapeHeader(to) + "\r\n")
	sb.WriteString("Subject: " + escapeHeader(subject) + "\r\n")
	sb.WriteString("Date: " + dateStr + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("X-Imported-From: PST\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)

	return []byte(sb.String())
}

// messageToNoteTxt converts a sticky-note Message to plain text.
func messageToNoteTxt(p *properties.Message) []byte {
	subject := p.GetSubject()
	body := p.GetBody()
	if subject != "" && body != "" {
		return []byte(subject + "\n\n" + body)
	}
	if subject != "" {
		return []byte(subject)
	}
	return []byte(body)
}

// readSubjectBody reads Subject and Body from a message's PropertyContext (for Appointment/Contact).
func readSubjectBody(msg *pst.Message) (subject, body string) {
	if msg.PropertyContext == nil {
		return "", ""
	}
	if r, err := msg.PropertyContext.GetPropertyReader(mapiTagSubject, msg.LocalDescriptors); err == nil {
		subject, _ = r.GetString()
	}
	if r, err := msg.PropertyContext.GetPropertyReader(mapiTagBody, msg.LocalDescriptors); err == nil {
		body, _ = r.GetString()
	}
	return subject, body
}

func appointmentDate(p *properties.Appointment) time.Time {
	if t := p.GetAppointmentStartWhole(); t > 0 {
		return time.Unix(t, 0)
	}
	if t := p.GetClipStart(); t > 0 {
		return time.Unix(t, 0)
	}
	return time.Now()
}

// appointmentToICS converts a PST appointment to iCalendar (.ics) format.
func appointmentToICS(msg *pst.Message, p *properties.Appointment) []byte {
	subject, body := readSubjectBody(msg)
	if subject == "" {
		subject = "Untitled"
	}
	loc := p.GetLocation()
	start := p.GetAppointmentStartWhole()
	end := p.GetAppointmentEndWhole()
	if start == 0 {
		start = p.GetClipStart()
	}
	if end == 0 {
		end = p.GetClipEnd()
	}
	if end <= start {
		end = start + 3600 // 1 hour default
	}

	startT := time.Unix(start, 0).UTC().Format("20060102T150405Z")
	endT := time.Unix(end, 0).UTC().Format("20060102T150405Z")
	now := time.Now().UTC().Format("20060102T150405Z")
	uid := fmt.Sprintf("pst-%d@imported", start)

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//PST Import//EN\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	sb.WriteString("UID:" + uid + "\r\n")
	sb.WriteString("DTSTAMP:" + now + "\r\n")
	sb.WriteString("DTSTART:" + startT + "\r\n")
	sb.WriteString("DTEND:" + endT + "\r\n")
	sb.WriteString("SUMMARY:" + foldLine(escapeICS(subject)) + "\r\n")
	if loc != "" {
		sb.WriteString("LOCATION:" + foldLine(escapeICS(loc)) + "\r\n")
	}
	if body != "" {
		sb.WriteString("DESCRIPTION:" + foldLine(escapeICS(body)) + "\r\n")
	}
	sb.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")

	return []byte(sb.String())
}

func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r\n", "\\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func foldLine(s string) string {
	const maxLen = 75
	if len(s) <= maxLen {
		return s
	}
	var sb strings.Builder
	for len(s) > maxLen {
		sb.WriteString(s[:maxLen])
		sb.WriteString("\r\n ")
		s = s[maxLen:]
	}
	sb.WriteString(s)
	return sb.String()
}

func contactDate(p *properties.Contact) time.Time {
	if t := p.GetBirthdayLocal(); t > 0 {
		return time.Unix(t, 0)
	}
	return time.Now()
}

// contactToVCF converts a PST contact to vCard (.vcf) format.
func contactToVCF(p *properties.Contact) []byte {
	fn := contactDisplayName(p)
	if fn == "" {
		fn = "Unknown"
	}
	given := p.GetGivenName()
	family := p.GetSurname()
	org := p.GetCompanyName()
	email := p.GetEmail1EmailAddress()
	if email == "" {
		email = p.GetEmail2EmailAddress()
	}
	if email == "" {
		email = p.GetEmail3EmailAddress()
	}
	phone := p.GetPrimaryTelephoneNumber()
	if phone == "" {
		phone = p.GetBusinessTelephoneNumber()
	}
	if phone == "" {
		phone = p.GetHomeTelephoneNumber()
	}
	addr := p.GetWorkAddressStreet()
	if addr == "" {
		addr = p.GetHomeAddressStreet()
	}
	city := p.GetWorkAddressCity()
	if city == "" {
		city = p.GetHomeAddressCity()
	}
	region := p.GetWorkAddressState()
	if region == "" {
		region = p.GetHomeAddressStateOrProvince()
	}
	postal := p.GetWorkAddressPostalCode()
	if postal == "" {
		postal = p.GetHomeAddressPostalCode()
	}
	country := p.GetWorkAddressCountry()
	if country == "" {
		country = p.GetHomeAddressCountry()
	}

	var sb strings.Builder
	sb.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	sb.WriteString("FN:" + vcfEscape(fn) + "\r\n")
	if given != "" || family != "" {
		sb.WriteString("N:" + vcfEscape(family) + ";" + vcfEscape(given) + ";;;\r\n")
	}
	if org != "" {
		sb.WriteString("ORG:" + vcfEscape(org) + "\r\n")
	}
	if email != "" {
		sb.WriteString("EMAIL:" + vcfEscape(email) + "\r\n")
	}
	if phone != "" {
		sb.WriteString("TEL:" + vcfEscape(phone) + "\r\n")
	}
	if addr != "" || city != "" || region != "" || postal != "" || country != "" {
		sb.WriteString("ADR:;;" + vcfEscape(addr) + ";" + vcfEscape(city) + ";" + vcfEscape(region) + ";" + vcfEscape(postal) + ";" + vcfEscape(country) + "\r\n")
	}
	if b := p.GetBirthdayLocal(); b > 0 {
		sb.WriteString("BDAY:" + time.Unix(b, 0).Format("2006-01-02") + "\r\n")
	}
	sb.WriteString("END:VCARD\r\n")

	return []byte(sb.String())
}

func contactDisplayName(p *properties.Contact) string {
	if s := p.GetFileUnder(); s != "" {
		return s
	}
	given := p.GetGivenName()
	family := p.GetSurname()
	if given != "" || family != "" {
		return strings.TrimSpace(given + " " + family)
	}
	if s := p.GetEmail1DisplayName(); s != "" {
		return s
	}
	if s := p.GetEmail1EmailAddress(); s != "" {
		return s
	}
	return p.GetDisplayNamePrefix()
}

func vcfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	return s
}

func formatSender(name, email string) string {
	if name != "" && email != "" {
		return fmt.Sprintf("%s <%s>", name, email)
	}
	if email != "" {
		return email
	}
	return name
}

func escapeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func contentChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// importReadpst uses the readpst command (pst-utils) when go-pst fails.
// Requires: apt install pst-utils (Debian/Ubuntu) or equivalent.
func importReadpst(pstPath, emailDir string, onProgress ProgressFunc) (int, int, error) {
	if _, err := exec.LookPath("readpst"); err != nil {
		return 0, 0, fmt.Errorf("readpst not installed (install pst-utils), go-pst failed earlier")
	}

	onProgress("extracting", 0, 0)

	cmd := exec.Command("readpst", "-e", "-o", emailDir, "-j", "0", pstPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("readpst: %w", err)
	}

	var count int
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".eml" || ext == ".vcf" || ext == ".ics" || ext == ".txt" {
				count++
			}
		}
		return nil
	})

	onProgress("done", count, count)
	return count, 0, nil
}

func sanitizeFolderName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		return "other"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

// StreamUpload reads a PST file from a reader with size, writing to a temp file
// with progress, then returns the temp path.
func StreamUpload(r io.Reader, size int64, onProgress ProgressFunc) (string, error) {
	tmp, err := os.CreateTemp("", "pst-upload-*.pst")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}

	var written int64
	buf := make([]byte, 256*1024) // 256KB chunks for smooth progress.
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, wErr := tmp.Write(buf[:n]); wErr != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return "", fmt.Errorf("write temp: %w", wErr)
			}
			written += int64(n)
			if size > 0 {
				onProgress("uploading", int(written/(1024*1024)), int(size/(1024*1024)))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("read upload: %w", readErr)
		}
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}

	return tmp.Name(), nil
}
