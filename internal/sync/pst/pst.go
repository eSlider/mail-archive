// Package pst implements PST/OST file import.
// Extracts messages from Microsoft Outlook personal storage files
// and saves them as .eml files preserving original dates.
package pst

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"

	"golang.org/x/text/encoding"

	charsets "github.com/emersion/go-message/charset"
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
// Returns (extracted count, error count).
func Import(pstPath, emailDir string, onProgress ProgressFunc) (int, int, error) {
	if onProgress == nil {
		onProgress = func(string, int, int) {}
	}

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
			emlData, date := messageToEML(msg)
			if emlData == nil {
				errCount++
				continue
			}

			checksum := contentChecksum(emlData)
			filename := fmt.Sprintf("%s-%d.eml", checksum, extracted)
			path := filepath.Join(dir, filename)

			if err := os.WriteFile(path, emlData, 0o644); err != nil {
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

// messageToEML converts a PST message to RFC822 .eml format.
func messageToEML(msg *pst.Message) ([]byte, time.Time) {
	var subject, from, to, body string
	var date time.Time

	switch p := msg.Properties.(type) {
	case *properties.Message:
		subject = p.GetSubject()
		from = formatSender(p.GetSenderName(), p.GetSenderEmailAddress())
		to = p.GetDisplayTo()
		body = p.GetBody()
		if ct := p.GetClientSubmitTime(); ct > 0 {
			date = time.Unix(ct, 0)
		} else if dt := p.GetMessageDeliveryTime(); dt > 0 {
			date = time.Unix(dt, 0)
		}
	default:
		// Skip non-message items (appointments, contacts, etc.).
		return nil, time.Time{}
	}

	if date.IsZero() {
		date = time.Now()
	}
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

	return []byte(sb.String()), date
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
