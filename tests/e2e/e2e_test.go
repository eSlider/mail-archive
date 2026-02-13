//go:build e2e

// Package e2e contains end-to-end tests that require a running GreenMail
// instance (docker compose --profile test up greenmail).
//
// Run with:
//
//	go test -tags e2e -v ./tests/e2e/
package e2e

import (
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eslider/mails/internal/model"
	"github.com/eslider/mails/internal/search/index"
	sync_state "github.com/eslider/mails/internal/sync"
	sync_imap "github.com/eslider/mails/internal/sync/imap"
	sync_pop3 "github.com/eslider/mails/internal/sync/pop3"
)

// Environment-overridable connection settings.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	smtpAddr = envOr("GREENMAIL_SMTP", "localhost:3025")
	imapHost = envOr("GREENMAIL_IMAP_HOST", "localhost")
	imapPort = 3143
	pop3Host = envOr("GREENMAIL_POP3_HOST", "localhost")
	pop3Port = 3110
	testUser = "testuser@localhost.com"
	testPass = "testuser@localhost.com" // GreenMail: password = email when auth disabled
)

// testMessages are the 5 emails seeded into GreenMail before each test run.
var testMessages = []struct {
	subject string
	from    string
	to      string
	body    string
	date    string
}{
	{
		subject: "Invoice #2024-001",
		from:    "billing@acme.com",
		to:      testUser,
		body:    "Please find attached your invoice for January 2024. Total amount: $1,234.56",
		date:    "Mon, 15 Jan 2024 09:00:00 +0000",
	},
	{
		subject: "Meeting Tomorrow at 10am",
		from:    "manager@company.org",
		to:      testUser,
		body:    "Hi team, reminder about our standup meeting tomorrow at 10am in Conference Room B.",
		date:    "Tue, 20 Feb 2024 14:30:00 +0000",
	},
	{
		subject: "Your order has shipped",
		from:    "noreply@shop.example",
		to:      testUser,
		body:    "Great news! Your order #98765 has been shipped via DHL. Tracking number: 1Z999AA10123456784.",
		date:    "Wed, 06 Mar 2024 08:15:00 +0000",
	},
	{
		subject: "Password Reset Request",
		from:    "security@service.io",
		to:      testUser,
		body:    "We received a request to reset your password. If you did not make this request, please ignore this email.",
		date:    "Thu, 11 Apr 2024 16:45:00 +0000",
	},
	{
		subject: "Weekly Newsletter - Golang Tips",
		from:    "newsletter@golangweekly.com",
		to:      testUser,
		body:    "This week: generics best practices, new testing patterns, and a deep dive into the sync package.",
		date:    "Fri, 10 May 2024 07:00:00 +0000",
	},
}

// TestMain ensures GreenMail is reachable before running tests.
func TestMain(m *testing.M) {
	// Quick connectivity check.
	conn, err := net.DialTimeout("tcp", smtpAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: GreenMail not reachable at %s: %v\n", smtpAddr, err)
		fmt.Fprintf(os.Stderr, "Start it with: docker compose --profile test up -d greenmail\n")
		os.Exit(1)
	}
	conn.Close()

	os.Exit(m.Run())
}

// seedMessages sends the test emails via SMTP to GreenMail.
// GreenMail auto-creates mailboxes for any recipient.
func seedMessages(t *testing.T) {
	t.Helper()

	for i, msg := range testMessages {
		body := fmt.Sprintf(
			"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <test-%d@e2e.local>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
			msg.from, msg.to, msg.subject, msg.date, i+1, msg.body,
		)
		err := smtp.SendMail(smtpAddr, nil, msg.from, []string{msg.to}, []byte(body))
		if err != nil {
			t.Fatalf("seed message %d (%s): %v", i+1, msg.subject, err)
		}
	}

	// Give GreenMail a moment to process.
	time.Sleep(500 * time.Millisecond)
	t.Logf("Seeded %d test messages via SMTP to %s", len(testMessages), smtpAddr)
}

// newTempDir creates a temporary directory for test artifacts.
func newTempDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	return sub
}

// --- IMAP Tests ---

func TestIMAPSync(t *testing.T) {
	seedMessages(t)

	emailDir := newTempDir(t, "emails-imap")
	stateDir := newTempDir(t, "state-imap")

	// Open a real SQLite sync state DB.
	stateDB, err := sync_state.OpenStateDB(stateDir, "test-user")
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer stateDB.Close()

	acct := model.EmailAccount{
		ID:       "imap-test-001",
		Type:     model.AccountTypeIMAP,
		Email:    testUser,
		Host:     imapHost,
		Port:     imapPort,
		Password: testPass,
		SSL:      false,
		Folders:  "INBOX",
	}

	newMsgs, err := sync_imap.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("IMAP sync failed: %v", err)
	}

	if newMsgs < 5 {
		t.Errorf("expected at least 5 new messages, got %d", newMsgs)
	}
	t.Logf("IMAP sync: %d new messages downloaded", newMsgs)

	// Verify .eml files were created.
	emlFiles := countEmlFiles(t, emailDir)
	if emlFiles < 5 {
		t.Errorf("expected at least 5 .eml files, found %d", emlFiles)
	}
	t.Logf("Found %d .eml files in %s", emlFiles, emailDir)

	// Verify sync state (UIDs should be recorded).
	uids, err := stateDB.SyncedUIDs(acct.ID, "INBOX")
	if err != nil {
		t.Fatalf("get synced UIDs: %v", err)
	}
	if len(uids) < 5 {
		t.Errorf("expected at least 5 synced UIDs, got %d", len(uids))
	}

	// Test idempotency: second sync should download 0 new messages.
	newMsgs2, err := sync_imap.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("IMAP second sync failed: %v", err)
	}
	if newMsgs2 != 0 {
		t.Errorf("expected 0 new messages on second sync, got %d", newMsgs2)
	}
	t.Log("IMAP idempotency check passed: 0 new messages on re-sync")
}

// --- POP3 Tests ---

func TestPOP3Sync(t *testing.T) {
	seedMessages(t)

	emailDir := newTempDir(t, "emails-pop3")
	stateDir := newTempDir(t, "state-pop3")

	stateDB, err := sync_state.OpenStateDB(stateDir, "test-user")
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer stateDB.Close()

	acct := model.EmailAccount{
		ID:       "pop3-test-001",
		Type:     model.AccountTypePOP3,
		Email:    testUser,
		Host:     pop3Host,
		Port:     pop3Port,
		Password: testPass,
		SSL:      false,
	}

	newMsgs, err := sync_pop3.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("POP3 sync failed: %v", err)
	}

	if newMsgs < 5 {
		t.Errorf("expected at least 5 new messages, got %d", newMsgs)
	}
	t.Logf("POP3 sync: %d new messages downloaded", newMsgs)

	// Verify .eml files were created in inbox/.
	inboxDir := filepath.Join(emailDir, "inbox")
	emlFiles := countEmlFiles(t, inboxDir)
	if emlFiles < 5 {
		t.Errorf("expected at least 5 .eml files in inbox, found %d", emlFiles)
	}
	t.Logf("Found %d .eml files in %s", emlFiles, inboxDir)

	// Verify hash-based dedup state.
	uids, err := stateDB.SyncedUIDs(acct.ID, "inbox")
	if err != nil {
		t.Fatalf("get synced UIDs: %v", err)
	}
	if len(uids) < 5 {
		t.Errorf("expected at least 5 synced hashes, got %d", len(uids))
	}

	// Test idempotency.
	newMsgs2, err := sync_pop3.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("POP3 second sync failed: %v", err)
	}
	if newMsgs2 != 0 {
		t.Errorf("expected 0 new messages on second sync, got %d", newMsgs2)
	}
	t.Log("POP3 idempotency check passed: 0 new messages on re-sync")
}

// --- Index + Search Tests ---

func TestIMAPSyncThenIndexAndSearch(t *testing.T) {
	seedMessages(t)

	emailDir := newTempDir(t, "emails-search-imap")
	stateDir := newTempDir(t, "state-search-imap")

	stateDB, err := sync_state.OpenStateDB(stateDir, "test-user")
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer stateDB.Close()

	// Step 1: Sync via IMAP.
	acct := model.EmailAccount{
		ID:       "search-imap-001",
		Type:     model.AccountTypeIMAP,
		Email:    testUser,
		Host:     imapHost,
		Port:     imapPort,
		Password: testPass,
		SSL:      false,
		Folders:  "INBOX",
	}

	newMsgs, err := sync_imap.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("IMAP sync failed: %v", err)
	}
	t.Logf("Synced %d messages via IMAP", newMsgs)

	// Step 2: Build the search index.
	indexPath := filepath.Join(emailDir, "index.parquet")
	idx, err := index.New(emailDir, indexPath)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer idx.Close()

	total, errCount := idx.Build()
	t.Logf("Indexed %d emails (%d errors)", total, errCount)

	if total < 5 {
		t.Errorf("expected at least 5 indexed emails, got %d", total)
	}

	// Step 3: Search by keyword.
	testSearchCases := []struct {
		query       string
		expectMin   int
		description string
	}{
		{"invoice", 1, "should find the invoice email"},
		{"meeting", 1, "should find the meeting email"},
		{"shipped", 1, "should find the shipping notification"},
		{"password", 1, "should find the password reset email"},
		{"golang", 1, "should find the newsletter"},
		{"nonexistent-xyzzy-42", 0, "should return 0 for nonsense query"},
	}

	for _, tc := range testSearchCases {
		t.Run("Search_"+tc.query, func(t *testing.T) {
			result := idx.Search(tc.query, 0, 50)
			if len(result.Hits) < tc.expectMin {
				t.Errorf("%s: expected at least %d hits for %q, got %d",
					tc.description, tc.expectMin, tc.query, len(result.Hits))
			} else {
				t.Logf("Search %q: %d hits (expected >= %d) - OK", tc.query, len(result.Hits), tc.expectMin)
			}
		})
	}

	// Step 4: Empty query returns all messages.
	t.Run("Search_empty_query", func(t *testing.T) {
		result := idx.Search("", 0, 50)
		if result.Total < 5 {
			t.Errorf("empty query: expected at least 5 total, got %d", result.Total)
		}
		t.Logf("Empty query: total=%d, hits=%d", result.Total, len(result.Hits))
	})

	// Step 5: Verify parquet file was created.
	if info, err := os.Stat(indexPath); err != nil || info.Size() == 0 {
		t.Error("expected non-empty index.parquet file")
	} else {
		t.Logf("Parquet index: %s (%d bytes)", indexPath, info.Size())
	}
}

func TestPOP3SyncThenIndexAndSearch(t *testing.T) {
	seedMessages(t)

	emailDir := newTempDir(t, "emails-search-pop3")
	stateDir := newTempDir(t, "state-search-pop3")

	stateDB, err := sync_state.OpenStateDB(stateDir, "test-user")
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	defer stateDB.Close()

	// Step 1: Sync via POP3.
	acct := model.EmailAccount{
		ID:       "search-pop3-001",
		Type:     model.AccountTypePOP3,
		Email:    testUser,
		Host:     pop3Host,
		Port:     pop3Port,
		Password: testPass,
		SSL:      false,
	}

	newMsgs, err := sync_pop3.Sync(acct, emailDir, stateDB)
	if err != nil {
		t.Fatalf("POP3 sync failed: %v", err)
	}
	t.Logf("Synced %d messages via POP3", newMsgs)

	// Step 2: Build index over inbox/.
	inboxDir := filepath.Join(emailDir, "inbox")
	indexPath := filepath.Join(emailDir, "index.parquet")
	idx, err := index.New(inboxDir, indexPath)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer idx.Close()

	total, errCount := idx.Build()
	t.Logf("Indexed %d emails (%d errors)", total, errCount)

	if total < 5 {
		t.Errorf("expected at least 5 indexed emails, got %d", total)
	}

	// Step 3: Search — verify same messages are findable.
	for _, kw := range []string{"invoice", "meeting", "shipped", "password", "golang"} {
		result := idx.Search(kw, 0, 50)
		if len(result.Hits) < 1 {
			t.Errorf("POP3 search for %q: expected at least 1 hit, got %d", kw, len(result.Hits))
		} else {
			t.Logf("POP3 search %q: %d hit(s) - OK", kw, len(result.Hits))
		}
	}

	// Step 4: Pagination.
	t.Run("Pagination", func(t *testing.T) {
		page1 := idx.Search("", 0, 3)
		page2 := idx.Search("", 3, 3)

		if len(page1.Hits) != 3 {
			t.Errorf("page 1: expected 3 hits, got %d", len(page1.Hits))
		}
		if len(page2.Hits) < 1 {
			t.Errorf("page 2: expected at least 1 hit, got %d", len(page2.Hits))
		}
		// Ensure no overlap.
		seen := make(map[string]bool)
		for _, h := range page1.Hits {
			seen[h.Path] = true
		}
		for _, h := range page2.Hits {
			if seen[h.Path] {
				t.Errorf("pagination overlap: %s appears in both pages", h.Path)
			}
		}
		t.Logf("Pagination: page1=%d hits, page2=%d hits, no overlap", len(page1.Hits), len(page2.Hits))
	})
}

// --- Helpers ---

func countEmlFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".eml") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Logf("WARN: walk %s: %v", dir, err)
	}
	return count
}
