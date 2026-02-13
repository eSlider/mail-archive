// Package imap implements IMAP email sync.
// Messages are downloaded as .eml files and NEVER deleted from the server.
package imap

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eslider/mails/internal/model"
)

// SyncState abstracts the sync state storage (implemented by sync.StateDB).
type SyncState interface {
	IsUIDSynced(accountID, folder, uid string) bool
	MarkUIDSynced(accountID, folder, uid string) error
}

// Sync downloads new emails from an IMAP account.
// Returns (newMessages, error). NEVER deletes or marks messages on the server.
func Sync(acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	addr := net.JoinHostPort(acct.Host, fmt.Sprintf("%d", acct.Port))
	log.Printf("IMAP: connecting to %s as %s", addr, acct.Email)

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

	client, err := newIMAPClient(conn)
	if err != nil {
		return 0, fmt.Errorf("imap init: %w", err)
	}
	defer client.logout()

	if err := client.login(acct.Email, acct.Password); err != nil {
		return 0, fmt.Errorf("imap login: %w", err)
	}
	log.Printf("IMAP: logged in to %s", acct.Host)

	folders, err := client.listFolders(acct.Folders)
	if err != nil {
		return 0, fmt.Errorf("list folders: %w", err)
	}
	log.Printf("IMAP: %d folders to sync", len(folders))

	totalNew := 0
	for _, folder := range folders {
		n, err := syncFolder(client, acct, folder, emailDir, state)
		if err != nil {
			log.Printf("WARN: IMAP folder %q: %v", folder, err)
			continue
		}
		totalNew += n
	}

	log.Printf("IMAP: %s downloaded %d new messages", acct.Email, totalNew)
	return totalNew, nil
}

func syncFolder(client *imapClient, acct model.EmailAccount, folder, emailDir string, state SyncState) (int, error) {
	folderPath := imapFolderToPath(folder)
	dir := filepath.Join(emailDir, folderPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	uids, err := client.selectAndSearch(folder)
	if err != nil {
		return 0, err
	}

	newCount := 0
	for _, uid := range uids {
		uidStr := fmt.Sprintf("%d", uid)
		if state.IsUIDSynced(acct.ID, folder, uidStr) {
			continue
		}

		raw, err := client.fetch(uid)
		if err != nil {
			log.Printf("WARN: fetch UID %d: %v", uid, err)
			continue
		}

		checksum := contentChecksum(raw)
		filename := fmt.Sprintf("%s-%d.eml", checksum, uid)
		path := filepath.Join(dir, filename)

		if err := os.WriteFile(path, raw, 0o644); err != nil {
			log.Printf("WARN: write %s: %v", path, err)
			continue
		}

		setFileMtime(path, raw)
		state.MarkUIDSynced(acct.ID, folder, uidStr)
		newCount++
	}

	return newCount, nil
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

// --- IMAP folder name to filesystem path mapping ---

var imapFolderMap = map[string]string{
	"inbox":                     "inbox",
	"[gmail]/sent mail":         "gmail/sent",
	"[gmail]/sent":              "gmail/sent",
	"[gmail]/gesendet":          "gmail/sent",
	"[google mail]/sent mail":   "gmail/sent",
	"[gmail]/drafts":            "gmail/draft",
	"[gmail]/draft":             "gmail/draft",
	"[gmail]/entwürfe":          "gmail/draft",
	"[google mail]/drafts":      "gmail/draft",
	"[gmail]/trash":             "gmail/trash",
	"[gmail]/papierkorb":        "gmail/trash",
	"[google mail]/trash":       "gmail/trash",
	"[gmail]/spam":              "gmail/spam",
	"[google mail]/spam":        "gmail/spam",
	"[gmail]/all mail":          "gmail/allmail",
	"[gmail]/alle nachrichten":  "gmail/allmail",
	"[google mail]/all mail":    "gmail/allmail",
	"[gmail]/marked":            "gmail/marked",
	"[gmail]/markiert":          "gmail/marked",
	"[gmail]/important":         "gmail/important",
	"[gmail]/wichtig":           "gmail/important",
}

var reSlugUnsafe = regexp.MustCompile(`[^\w\s\-.]`)
var reSlugSep = regexp.MustCompile(`[.\s_\-]+`)

func slugifyPart(name string) string {
	name = strings.ReplaceAll(name, "[", "")
	name = strings.ReplaceAll(name, "]", "")
	name = strings.TrimSpace(strings.ToLower(name))
	name = reSlugUnsafe.ReplaceAllString(name, "")
	name = reSlugSep.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "other"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

func imapFolderToPath(folderName string) string {
	key := strings.TrimSpace(strings.ToLower(folderName))
	if mapped, ok := imapFolderMap[key]; ok {
		return mapped
	}

	parts := strings.Split(strings.ReplaceAll(folderName, "\\", "/"), "/")
	var slugs []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			slugs = append(slugs, slugifyPart(s))
		}
	}
	if len(slugs) > 0 && (slugs[0] == "gmail" || slugs[0] == "google_mail") {
		slugs[0] = "gmail"
	}
	if len(slugs) == 0 {
		return "other"
	}
	return strings.Join(slugs, "/")
}

// --- Minimal IMAP client (stdlib net/textproto based) ---
// A lightweight IMAP client for sync-only operations.

type imapClient struct {
	conn net.Conn
	tag  int
}

func newIMAPClient(conn net.Conn) (*imapClient, error) {
	c := &imapClient{conn: conn}
	// Read server greeting.
	if _, err := c.readLine(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *imapClient) command(format string, args ...any) ([]string, error) {
	c.tag++
	tag := fmt.Sprintf("A%04d", c.tag)
	cmd := fmt.Sprintf("%s %s\r\n", tag, fmt.Sprintf(format, args...))
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}

	var lines []string
	for {
		line, err := c.readLine()
		if err != nil {
			return lines, err
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, "OK") {
				return lines, nil
			}
			return lines, fmt.Errorf("IMAP error: %s", line)
		}
		lines = append(lines, line)
	}
}

func (c *imapClient) readLine() (string, error) {
	buf := make([]byte, 0, 4096)
	one := make([]byte, 1)
	for {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := c.conn.Read(one)
		if err != nil {
			return string(buf), err
		}
		if n > 0 {
			buf = append(buf, one[0])
			if one[0] == '\n' {
				return strings.TrimRight(string(buf), "\r\n"), nil
			}
		}
	}
}

func (c *imapClient) login(user, password string) error {
	_, err := c.command(`LOGIN "%s" "%s"`, user, password)
	return err
}

func (c *imapClient) logout() {
	c.command("LOGOUT")
	c.conn.Close()
}

func (c *imapClient) listFolders(foldersCfg string) ([]string, error) {
	if foldersCfg != "all" {
		return strings.Split(foldersCfg, ","), nil
	}
	lines, err := c.command(`LIST "" "*"`)
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, line := range lines {
		// Parse: * LIST (\flags) "." "FolderName"
		if strings.Contains(strings.ToLower(line), "\\noselect") {
			continue
		}
		// Extract folder name (last quoted string or last word).
		parts := strings.SplitN(line, ") ", 2)
		if len(parts) < 2 {
			continue
		}
		rest := parts[1]
		// Skip delimiter, get folder name.
		if idx := strings.LastIndex(rest, `"`); idx > 0 {
			start := strings.LastIndex(rest[:idx], `"`)
			if start >= 0 {
				folders = append(folders, rest[start+1:idx])
				continue
			}
		}
		// Fallback: last space-separated token.
		tokens := strings.Fields(rest)
		if len(tokens) > 0 {
			folders = append(folders, tokens[len(tokens)-1])
		}
	}
	return folders, nil
}

func (c *imapClient) selectAndSearch(folder string) ([]int, error) {
	_, err := c.command(`SELECT "%s"`, folder)
	if err != nil {
		return nil, err
	}
	lines, err := c.command("SEARCH ALL")
	if err != nil {
		return nil, err
	}
	var uids []int
	for _, line := range lines {
		if !strings.HasPrefix(line, "* SEARCH") {
			continue
		}
		for _, s := range strings.Fields(line)[2:] {
			var uid int
			if _, err := fmt.Sscanf(s, "%d", &uid); err == nil {
				uids = append(uids, uid)
			}
		}
	}
	return uids, nil
}

func (c *imapClient) fetch(uid int) ([]byte, error) {
	lines, err := c.command("FETCH %d RFC822", uid)
	if err != nil {
		return nil, err
	}
	// Collect raw message data from FETCH response.
	var data []byte
	collecting := false
	for _, line := range lines {
		if strings.Contains(line, "RFC822") && strings.Contains(line, "{") {
			collecting = true
			continue
		}
		if collecting {
			if strings.HasPrefix(line, ")") {
				break
			}
			data = append(data, []byte(line+"\r\n")...)
		}
	}
	return data, nil
}
