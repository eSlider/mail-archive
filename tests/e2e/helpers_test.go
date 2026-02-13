//go:build e2e

package e2e

import (
	"github.com/eslider/mails/internal/model"
	sync_state "github.com/eslider/mails/internal/sync"
	sync_imap "github.com/eslider/mails/internal/sync/imap"
)

// syncStateDB wraps sync.StateDB for use in test helpers.
type syncStateDB = sync_state.StateDB

func openSyncStateDB(stateDir, userID string) (*sync_state.StateDB, error) {
	return sync_state.OpenStateDB(stateDir, userID)
}

// imapAccount holds IMAP connection details for test helpers.
type imapAccount struct {
	id       string
	email    string
	host     string
	port     int
	password string
}

func syncIMAPAccount(acct imapAccount, emailDir string, state *sync_state.StateDB) (int, error) {
	a := model.EmailAccount{
		ID:       acct.id,
		Type:     model.AccountTypeIMAP,
		Email:    acct.email,
		Host:     acct.host,
		Port:     acct.port,
		Password: acct.password,
		SSL:      false,
		Folders:  "INBOX",
	}
	return sync_imap.Sync(a, emailDir, state)
}
