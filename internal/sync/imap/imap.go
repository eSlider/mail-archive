// Package imap implements IMAP email sync.
// Messages are downloaded as .eml files and NEVER deleted from the server.
package imap

import (
	"context"
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

// ProgressFunc is called with human-readable progress updates during sync.
type ProgressFunc func(msg string)

// Sync downloads new emails from an IMAP account.
// Returns (newMessages, error). NEVER deletes or marks messages on the server.
func Sync(acct model.EmailAccount, emailDir string, state SyncState) (int, error) {
	return SyncWithContext(context.Background(), acct, emailDir, state, nil)
}

// SyncWithContext downloads new emails with cancellation and progress reporting.
func SyncWithContext(ctx context.Context, acct model.EmailAccount, emailDir string, state SyncState, onProgress ProgressFunc) (int, error) {
	if onProgress == nil {
		onProgress = func(string) {}
	}

	addr := net.JoinHostPort(acct.Host, fmt.Sprintf("%d", acct.Port))
	log.Printf("IMAP: connecting to %s as %s", addr, acct.Email)
	onProgress("connecting to " + acct.Host)

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
	onProgress("logged in, listing folders")

	folders, err := client.listFolders(acct.Folders)
	if err != nil {
		return 0, fmt.Errorf("list folders: %w", err)
	}
	log.Printf("IMAP: %d folders to sync", len(folders))

	totalNew := 0
	for fi, folder := range folders {
		// Check for cancellation between folders.
		select {
		case <-ctx.Done():
			log.Printf("IMAP: sync cancelled for %s after %d folders, %d messages", acct.Email, fi, totalNew)
			return totalNew, ctx.Err()
		default:
		}

		onProgress(fmt.Sprintf("folder %d/%d: %s", fi+1, len(folders), folder))
		n, err := syncFolderWithContext(ctx, client, acct, folder, emailDir, state)
		if err != nil {
			if ctx.Err() != nil {
				return totalNew, ctx.Err()
			}
			log.Printf("WARN: IMAP folder %q: %v", folder, err)
			continue
		}
		totalNew += n
	}

	log.Printf("IMAP: %s downloaded %d new messages", acct.Email, totalNew)
	return totalNew, nil
}

const fetchBatchSize = 50

func syncFolder(client *imapClient, acct model.EmailAccount, folder, emailDir string, state SyncState) (int, error) {
	return syncFolderWithContext(context.Background(), client, acct, folder, emailDir, state)
}

func syncFolderWithContext(ctx context.Context, client *imapClient, acct model.EmailAccount, folder, emailDir string, state SyncState) (int, error) {
	folderPath := imapFolderToPath(folder)
	dir := filepath.Join(emailDir, folderPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	uids, err := client.selectAndSearch(folder)
	if err != nil {
		return 0, err
	}

	// Filter to only new UIDs (not yet synced).
	var newUIDs []int
	for _, uid := range uids {
		if !state.IsUIDSynced(acct.ID, folder, fmt.Sprintf("%d", uid)) {
			newUIDs = append(newUIDs, uid)
		}
	}

	if len(newUIDs) == 0 {
		return 0, nil
	}

	log.Printf("IMAP: folder %q: %d new of %d total", folder, len(newUIDs), len(uids))

	newCount := 0
	// Fetch in batches (like Python's IMAPClient batch of 100).
	for i := 0; i < len(newUIDs); i += fetchBatchSize {
		// Check for cancellation between batches.
		select {
		case <-ctx.Done():
			return newCount, ctx.Err()
		default:
		}
		end := i + fetchBatchSize
		if end > len(newUIDs) {
			end = len(newUIDs)
		}
		batch := newUIDs[i:end]

		messages, err := client.fetchBatch(batch)
		if err != nil {
			log.Printf("WARN: batch fetch in %q: %v", folder, err)
			// Fall back to one-by-one for this batch.
			for _, uid := range batch {
				raw, err := client.fetch(uid)
				if err != nil {
					log.Printf("WARN: fetch UID %d: %v", uid, err)
					continue
				}
				if saveEmail(dir, uid, raw, acct.ID, folder, state) {
					newCount++
				}
			}
			continue
		}

		for uid, raw := range messages {
			if saveEmail(dir, uid, raw, acct.ID, folder, state) {
				newCount++
			}
		}
	}

	return newCount, nil
}

func saveEmail(dir string, uid int, raw []byte, accountID, folder string, state SyncState) bool {
	if len(raw) == 0 {
		return false
	}
	checksum := contentChecksum(raw)
	filename := fmt.Sprintf("%s-%d.eml", checksum, uid)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		log.Printf("WARN: write %s: %v", path, err)
		return false
	}

	setFileMtime(path, raw)
	state.MarkUIDSynced(accountID, folder, fmt.Sprintf("%d", uid))
	return true
}

// contentChecksum returns the first 16 hex chars of SHA-256.
func contentChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// setFileMtime sets the file's modification time from the email Date header.
// Falls back to the first Received header if Date is missing or unparseable.
func setFileMtime(path string, raw []byte) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return
	}
	date, _ := msg.Header.Date()
	if date.IsZero() {
		date = parseDateFuzzy(msg.Header.Get("Date"))
	}
	if date.IsZero() {
		date = parseReceivedDate(msg.Header)
	}
	if date.IsZero() {
		return
	}
	os.Chtimes(path, date, date)
}

// parseDateFuzzy tries multiple date layouts for non-standard Date headers.
func parseDateFuzzy(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
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
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseReceivedDate extracts the date from the first Received header.
func parseReceivedDate(h mail.Header) time.Time {
	received := h.Get("Received")
	if received == "" {
		return time.Time{}
	}
	idx := strings.LastIndex(received, ";")
	if idx < 0 {
		return time.Time{}
	}
	dateStr := strings.TrimSpace(received[idx+1:])
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
	return time.Time{}
}

// --- IMAP folder name to filesystem path mapping ---

var imapFolderMap = map[string]string{
	"inbox":                    "inbox",
	"[gmail]/sent mail":        "gmail/sent",
	"[gmail]/sent":             "gmail/sent",
	"[gmail]/gesendet":         "gmail/sent",
	"[google mail]/sent mail":  "gmail/sent",
	"[gmail]/drafts":           "gmail/draft",
	"[gmail]/draft":            "gmail/draft",
	"[gmail]/entwürfe":         "gmail/draft",
	"[google mail]/drafts":     "gmail/draft",
	"[gmail]/trash":            "gmail/trash",
	"[gmail]/papierkorb":       "gmail/trash",
	"[google mail]/trash":      "gmail/trash",
	"[gmail]/spam":             "gmail/spam",
	"[google mail]/spam":       "gmail/spam",
	"[gmail]/all mail":         "gmail/allmail",
	"[gmail]/alle nachrichten": "gmail/allmail",
	"[google mail]/all mail":   "gmail/allmail",
	"[gmail]/marked":           "gmail/marked",
	"[gmail]/markiert":         "gmail/marked",
	"[gmail]/important":        "gmail/important",
	"[gmail]/wichtig":          "gmail/important",
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

// --- Minimal IMAP client (buffered, UID-based) ---
// A lightweight IMAP client for sync-only operations.
// Uses UID commands (stable across sessions) and proper literal parsing.

type imapClient struct {
	conn net.Conn
	buf  []byte // read buffer
	tag  int
}

func newIMAPClient(conn net.Conn) (*imapClient, error) {
	c := &imapClient{conn: conn, buf: make([]byte, 0, 8192)}
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

// readLine reads one CRLF-terminated line using a buffered approach.
func (c *imapClient) readLine() (string, error) {
	for {
		// Check if buffer already contains a full line.
		if idx := indexOf(c.buf, '\n'); idx >= 0 {
			line := string(c.buf[:idx])
			c.buf = c.buf[idx+1:]
			return strings.TrimRight(line, "\r"), nil
		}
		// Read more data into buffer.
		c.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		tmp := make([]byte, 8192)
		n, err := c.conn.Read(tmp)
		if n > 0 {
			c.buf = append(c.buf, tmp[:n]...)
		}
		if err != nil {
			// Return what we have if buffer is non-empty.
			if len(c.buf) > 0 {
				line := string(c.buf)
				c.buf = c.buf[:0]
				return strings.TrimRight(line, "\r\n"), err
			}
			return "", err
		}
	}
}

// readExact reads exactly n bytes from the connection (for IMAP literals).
func (c *imapClient) readExact(n int) ([]byte, error) {
	for len(c.buf) < n {
		c.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		tmp := make([]byte, 8192)
		nr, err := c.conn.Read(tmp)
		if nr > 0 {
			c.buf = append(c.buf, tmp[:nr]...)
		}
		if err != nil {
			return nil, err
		}
	}
	data := make([]byte, n)
	copy(data, c.buf[:n])
	c.buf = c.buf[n:]
	return data, nil
}

func indexOf(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
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

// selectAndSearch uses UID SEARCH to get stable UIDs (like Python's IMAPClient).
// Sequence numbers change between sessions; UIDs are persistent.
func (c *imapClient) selectAndSearch(folder string) ([]int, error) {
	_, err := c.command(`SELECT "%s"`, folder)
	if err != nil {
		return nil, err
	}
	lines, err := c.command("UID SEARCH ALL")
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

// fetch retrieves a single email by UID.
func (c *imapClient) fetch(uid int) ([]byte, error) {
	result, err := c.fetchBatch([]int{uid})
	if err != nil {
		return nil, err
	}
	if raw, ok := result[uid]; ok {
		return raw, nil
	}
	return nil, fmt.Errorf("UID %d not in FETCH response", uid)
}

// fetchBatch retrieves multiple emails in one UID FETCH command.
// Returns map[uid]rawBytes. Matches Python's `client.fetch(batch, ["RFC822"])`.
func (c *imapClient) fetchBatch(uids []int) (map[int][]byte, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	// Build UID set string: "1,2,3,4,5"
	parts := make([]string, len(uids))
	for i, uid := range uids {
		parts[i] = fmt.Sprintf("%d", uid)
	}
	uidSet := strings.Join(parts, ",")

	c.tag++
	tag := fmt.Sprintf("A%04d", c.tag)
	cmd := fmt.Sprintf("%s UID FETCH %s RFC822\r\n", tag, uidSet)
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}

	result := make(map[int][]byte)
	for {
		line, err := c.readLine()
		if err != nil {
			return result, fmt.Errorf("fetchBatch: %w", err)
		}

		// Tag line = end of response.
		if strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, "OK") {
				return result, nil
			}
			return result, fmt.Errorf("IMAP fetch error: %s", line)
		}

		// Look for: * N FETCH (UID NNN RFC822 {size})
		if !strings.Contains(line, "{") {
			continue
		}

		// Extract UID from the response line.
		// Format: * <seq> FETCH (UID <uid> RFC822 {<size>})
		// or:     * <seq> FETCH (RFC822 {<size>} UID <uid>)
		msgUID := 0
		if idx := strings.Index(strings.ToUpper(line), "UID "); idx >= 0 {
			fmt.Sscanf(line[idx+4:], "%d", &msgUID)
		}

		// Extract literal size.
		braceStart := strings.LastIndex(line, "{")
		braceEnd := strings.LastIndex(line, "}")
		if braceStart < 0 || braceEnd <= braceStart {
			continue
		}
		var size int
		if _, err := fmt.Sscanf(line[braceStart:braceEnd+1], "{%d}", &size); err != nil || size <= 0 {
			continue
		}

		// Read exactly `size` bytes of literal data.
		rawData, err := c.readExact(size)
		if err != nil {
			return result, fmt.Errorf("fetchBatch literal UID %d: %w", msgUID, err)
		}

		// Read the trailing line after the literal (e.g. " UID 123)" or ")").
		// If UID wasn't in the pre-literal line, look for it here.
		trailing, trailErr := c.readLine()
		if trailErr != nil {
			return result, fmt.Errorf("fetchBatch trailing: %w", trailErr)
		}
		if msgUID == 0 {
			if idx := strings.Index(strings.ToUpper(trailing), "UID "); idx >= 0 {
				fmt.Sscanf(trailing[idx+4:], "%d", &msgUID)
			}
		}

		if msgUID > 0 {
			result[msgUID] = rawData
		}
	}
}
