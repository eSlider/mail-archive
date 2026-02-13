// Package gmail implements Gmail API email sync.
// Messages are downloaded as .eml files and NEVER deleted from the server.
package gmail

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/eslider/mails/internal/model"
)

// SyncState abstracts the sync state storage (implemented by sync.StateDB).
type SyncState interface {
	IsUIDSynced(accountID, folder, uid string) bool
	MarkUIDSynced(accountID, folder, uid string) error
}

// Sync downloads new emails from a Gmail account via the Gmail API.
// Returns (newMessages, error). NEVER deletes messages from the server.
func Sync(acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	return SyncWithContext(context.Background(), acct, emailDir, state)
}

// SyncWithContext downloads new emails with cancellation support.
//
// TODO: Implement Gmail API client using golang.org/x/oauth2/google
// and google.golang.org/api/gmail/v1. For now, this is a stub that
// returns an error indicating it needs implementation with proper
// OAuth2 credentials.
func SyncWithContext(ctx context.Context, acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	log.Printf("Gmail API: sync for %s (stub — not yet implemented)", acct.Email)

	// Gmail API sync requires:
	// 1. OAuth2 credentials (credentials.json from Google Cloud Console)
	// 2. Token file (obtained via browser-based OAuth flow)
	// 3. Gmail API client to list and fetch messages
	// 4. Convert Gmail message format to raw RFC822 .eml

	// The implementation should:
	// - List all messages using users.messages.list
	// - For each new message ID (not in state), fetch raw RFC822
	// - Map Gmail labels to filesystem paths
	// - Save as {checksum}-{messageID}.eml
	// - Track synced message IDs in state

	return 0, fmt.Errorf("Gmail API sync not yet implemented — use IMAP with app password instead")
}

// labelToPath maps a Gmail label to a filesystem path.
func labelToPath(label string) string {
	label = strings.ToLower(label)
	switch label {
	case "inbox":
		return "inbox"
	case "sent":
		return "gmail/sent"
	case "draft":
		return "gmail/draft"
	case "trash":
		return "gmail/trash"
	case "spam":
		return "gmail/spam"
	default:
		// Custom labels: slugify.
		safe := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '/' || r == '_' {
				return r
			}
			if r == ' ' || r == '-' || r == '.' {
				return '_'
			}
			return -1
		}, label)
		if safe == "" {
			safe = "other"
		}
		return safe
	}
}

// contentChecksum returns the first 16 hex chars of SHA-256.
func contentChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// setFileMtime sets the file's modification time from the email Date header.
func setFileMtime(path string, raw []byte) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return
	}
	date, err := msg.Header.Date()
	if err != nil || date.IsZero() {
		return
	}
	os.Chtimes(path, date, date)
}

// saveEmail writes a raw email to the appropriate directory.
func saveEmail(raw []byte, msgID, emailDir, label string) (string, error) {
	dir := filepath.Join(emailDir, labelToPath(label))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	checksum := contentChecksum(raw)
	filename := fmt.Sprintf("%s-%s.eml", checksum, msgID)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}

	setFileMtime(path, raw)
	return path, nil
}
