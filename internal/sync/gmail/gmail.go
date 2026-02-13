// Package gmail implements Gmail API email sync.
// Messages are downloaded as .eml files and NEVER deleted from the server.
package gmail

import (
	"context"
	"fmt"
	"log"

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

	return 0, fmt.Errorf("gmail API sync not yet implemented — use IMAP with app password instead")
}
