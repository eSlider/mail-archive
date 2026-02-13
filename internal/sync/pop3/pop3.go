// Package pop3 implements POP3 email sync.
// Messages are downloaded as .eml files and NEVER deleted from the server.
package pop3

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eslider/mails/internal/model"
)

// SyncState abstracts the sync state storage (implemented by sync.StateDB).
type SyncState interface {
	IsUIDSynced(accountID, folder, uid string) bool
	MarkUIDSynced(accountID, folder, uid string) error
}

// Sync downloads new emails from a POP3 account.
// Uses SHA-256 hash deduplication since POP3 has no stable UIDs.
// Returns (newMessages, error). NEVER deletes messages from the server.
func Sync(acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	return SyncWithContext(context.Background(), acct, emailDir, state)
}

// SyncWithContext downloads new emails with cancellation support.
func SyncWithContext(ctx context.Context, acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	port := acct.Port
	if port == 0 {
		if acct.SSL {
			port = 995
		} else {
			port = 110
		}
	}

	addr := net.JoinHostPort(acct.Host, fmt.Sprintf("%d", port))
	log.Printf("POP3: connecting to %s as %s", addr, acct.Email)

	var conn net.Conn
	var err error
	if acct.SSL {
		conn, err = tls.Dial("tcp", addr, &tls.Config{ServerName: acct.Host})
	} else {
		conn, err = net.DialTimeout("tcp", addr, 30*time.Second)
	}
	if err != nil {
		return 0, fmt.Errorf("connect %s: %w", addr, err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read greeting.
	if _, err := readPOP3Line(reader); err != nil {
		return 0, err
	}

	// Login.
	if err := pop3Command(conn, reader, "USER %s", acct.Email); err != nil {
		return 0, fmt.Errorf("POP3 USER: %w", err)
	}
	if err := pop3Command(conn, reader, "PASS %s", acct.Password); err != nil {
		return 0, fmt.Errorf("POP3 PASS: %w", err)
	}
	log.Printf("POP3: logged in to %s", acct.Host)

	// List messages.
	count, err := pop3Stat(conn, reader)
	if err != nil {
		return 0, fmt.Errorf("POP3 STAT: %w", err)
	}
	log.Printf("POP3: %d messages in mailbox", count)

	inboxDir := filepath.Join(emailDir, "inbox")
	os.MkdirAll(inboxDir, 0o755)

	totalNew := 0
	for i := 1; i <= count; i++ {
		// Check for cancellation between messages.
		select {
		case <-ctx.Done():
			log.Printf("POP3: sync cancelled for %s after %d messages", acct.Email, totalNew)
			pop3Command(conn, reader, "QUIT")
			return totalNew, ctx.Err()
		default:
		}

		raw, err := pop3Retr(conn, reader, i)
		if err != nil {
			log.Printf("WARN: POP3 RETR %d: %v", i, err)
			continue
		}

		msgHash := hashContent(raw)
		if state.IsUIDSynced(acct.ID, "inbox", msgHash) {
			continue
		}

		checksum := contentChecksum(raw)
		filename := fmt.Sprintf("%s-%s.eml", checksum, msgHash)
		path := filepath.Join(inboxDir, filename)

		if err := os.WriteFile(path, raw, 0o644); err != nil {
			log.Printf("WARN: write %s: %v", path, err)
			continue
		}

		setFileMtime(path, raw)
		state.MarkUIDSynced(acct.ID, "inbox", msgHash)
		totalNew++
	}

	// QUIT (don't delete anything).
	pop3Command(conn, reader, "QUIT")

	log.Printf("POP3: %s downloaded %d new messages", acct.Email, totalNew)
	return totalNew, nil
}

func hashContent(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

func contentChecksum(data []byte) string {
	return hashContent(data)
}

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

// --- POP3 protocol helpers ---

func pop3Command(conn net.Conn, reader *bufio.Reader, format string, args ...any) error {
	cmd := fmt.Sprintf(format, args...) + "\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}
	line, err := readPOP3Line(reader)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "+OK") {
		return fmt.Errorf("POP3 error: %s", line)
	}
	return nil
}

func pop3Stat(conn net.Conn, reader *bufio.Reader) (int, error) {
	cmd := "STAT\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return 0, err
	}
	line, err := readPOP3Line(reader)
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(line, "+OK") {
		return 0, fmt.Errorf("POP3 STAT error: %s", line)
	}
	var count, size int
	fmt.Sscanf(line, "+OK %d %d", &count, &size)
	return count, nil
}

func pop3Retr(conn net.Conn, reader *bufio.Reader, msgNum int) ([]byte, error) {
	cmd := fmt.Sprintf("RETR %d\r\n", msgNum)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}
	line, err := readPOP3Line(reader)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "+OK") {
		return nil, fmt.Errorf("POP3 RETR error: %s", line)
	}

	// Read multi-line response until "."
	var data []byte
	for {
		line, err := readPOP3Line(reader)
		if err != nil {
			return data, err
		}
		if line == "." {
			break
		}
		// Byte-stuffed lines starting with ".." -> "."
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		data = append(data, []byte(line+"\r\n")...)
	}
	return data, nil
}

func readPOP3Line(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
