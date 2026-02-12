// Package index provides a DuckDB-backed email index persisted as Parquet (zstd).
package index

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/eslider/mails/search/eml"
)

// reChecksum matches the 16-hex-char checksum prefix in filenames like "a1b2c3d4e5f67890-123.eml".
var reChecksum = regexp.MustCompile(`^([0-9a-f]{16})-`)

// Index stores parsed email metadata in a DuckDB in-memory database,
// persisted to a Parquet file with zstd compression.
type Index struct {
	mu        sync.RWMutex
	db        *sql.DB
	buildAt   time.Time
	emailDir  string
	indexPath string
	total     int
}

const createTableSQL = `CREATE TABLE IF NOT EXISTS emails (
	path      VARCHAR NOT NULL DEFAULT '',
	subject   VARCHAR NOT NULL DEFAULT '',
	from_addr VARCHAR NOT NULL DEFAULT '',
	to_addr   VARCHAR NOT NULL DEFAULT '',
	date      TIMESTAMP,
	size      BIGINT  NOT NULL DEFAULT 0,
	body_text VARCHAR NOT NULL DEFAULT ''
)`

// New creates a new index. If indexPath points to an existing Parquet file,
// the index is loaded from it (fast startup). Pass "" for indexPath to use
// a purely in-memory index without persistence.
func New(emailDir, indexPath string) (*Index, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1)

	idx := &Index{
		db:        db,
		emailDir:  emailDir,
		indexPath: indexPath,
	}

	// Try loading from existing Parquet file.
	if indexPath != "" {
		if info, statErr := os.Stat(indexPath); statErr == nil && info.Size() > 0 {
			count, loadErr := idx.loadParquet()
			if loadErr == nil {
				idx.total = count
				idx.buildAt = info.ModTime()
				log.Printf("Loaded %d emails from %s", count, indexPath)
				return idx, nil
			}
			log.Printf("WARN: could not load %s: %v, will rebuild", indexPath, loadErr)
		}
	}

	if _, err := idx.db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}
	return idx, nil
}

// Close releases the DuckDB database connection.
func (idx *Index) Close() error {
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}

func (idx *Index) loadParquet() (int, error) {
	escaped := strings.ReplaceAll(idx.indexPath, "'", "''")
	if _, err := idx.db.Exec(
		fmt.Sprintf("CREATE TABLE emails AS SELECT * FROM read_parquet('%s')", escaped),
	); err != nil {
		return 0, fmt.Errorf("load parquet: %w", err)
	}
	var n int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM emails").Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (idx *Index) saveParquet() error {
	if idx.indexPath == "" {
		return nil
	}
	if dir := filepath.Dir(idx.indexPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// Remove old file first — COPY TO won't overwrite.
	os.Remove(idx.indexPath)
	escaped := strings.ReplaceAll(idx.indexPath, "'", "''")
	_, err := idx.db.Exec(
		fmt.Sprintf("COPY emails TO '%s' (FORMAT PARQUET, CODEC 'ZSTD')", escaped))
	return err
}

// WalkEmails walks the email directory, parses .eml files, and returns
// deduplicated emails by checksum. Used for both DuckDB indexing and Qdrant.
func WalkEmails(emailDir string) ([]eml.Email, int) {
	var parsed []eml.Email
	var errCount int
	seen := make(map[string]bool)

	_ = filepath.WalkDir(emailDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".eml") {
			return nil
		}
		if cs := extractChecksum(d.Name()); cs != "" {
			if seen[cs] {
				return nil
			}
			seen[cs] = true
		}
		e, parseErr := eml.ParseFile(path)
		if parseErr != nil {
			log.Printf("WARN: skip %s: %v", path, parseErr)
			errCount++
			return nil
		}
		if rel, relErr := filepath.Rel(emailDir, path); relErr == nil {
			e.Path = rel
		}
		parsed = append(parsed, e)
		return nil
	})
	return parsed, errCount
}

// Build walks the email directory, parses every .eml file, stores them in
// DuckDB and exports to Parquet with zstd. Emails with duplicate content
// checksums are deduplicated. It replaces the previous index atomically.
func (idx *Index) Build() (int, int) {
	parsed, errCount := WalkEmails(idx.emailDir)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.db.Exec("DROP TABLE IF EXISTS emails")
	if _, err := idx.db.Exec(createTableSQL); err != nil {
		log.Printf("ERROR: create table: %v", err)
		return 0, errCount
	}

	tx, err := idx.db.Begin()
	if err != nil {
		log.Printf("ERROR: begin tx: %v", err)
		return 0, errCount
	}
	stmt, err := tx.Prepare(
		"INSERT INTO emails (path, subject, from_addr, to_addr, date, size, body_text) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		log.Printf("ERROR: prepare: %v", err)
		return 0, errCount
	}
	for _, e := range parsed {
		if _, err := stmt.Exec(e.Path, e.Subject, e.From, e.To, e.Date, e.Size, e.BodyText); err != nil {
			log.Printf("WARN: insert %s: %v", e.Path, err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: commit: %v", err)
		return 0, errCount
	}

	if err := idx.saveParquet(); err != nil {
		log.Printf("WARN: save parquet: %v", err)
	} else if idx.indexPath != "" {
		log.Printf("Saved index to %s", idx.indexPath)
	}

	idx.total = len(parsed)
	idx.buildAt = time.Now()
	return len(parsed), errCount
}

// Hit is a single search result with a context snippet.
type Hit struct {
	eml.Email
	Snippet string `json:"snippet,omitempty"`
}

// SearchResult wraps matched emails with metadata.
type SearchResult struct {
	Query   string    `json:"query"`
	Total   int       `json:"total"`
	Offset  int       `json:"offset"`
	Limit   int       `json:"limit"`
	Hits    []Hit     `json:"hits"`
	IndexAt time.Time `json:"indexed_at"`
}

// Search returns emails whose subject or body contains the query (case-insensitive).
func (idx *Index) Search(query string, offset, limit int) SearchResult {
	q := strings.ToLower(strings.TrimSpace(query))

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var total int
	var hits []Hit

	if q == "" {
		total = idx.total
		hits = idx.queryPage(offset, limit)
	} else {
		total = idx.countMatches(q)
		hits = idx.queryMatches(q, offset, limit)
	}

	return SearchResult{
		Query:   query,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		Hits:    hits,
		IndexAt: idx.buildAt,
	}
}

// queryPage returns a page of all emails ordered newest-first.
func (idx *Index) queryPage(offset, limit int) []Hit {
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = idx.db.Query(
			"SELECT path, subject, from_addr, to_addr, date, size FROM emails ORDER BY date DESC LIMIT ? OFFSET ?",
			limit, offset)
	} else {
		rows, err = idx.db.Query(
			"SELECT path, subject, from_addr, to_addr, date, size FROM emails ORDER BY date DESC")
	}
	if err != nil {
		log.Printf("WARN: queryPage: %v", err)
		return make([]Hit, 0)
	}
	defer rows.Close()
	return scanHits(rows, "", false)
}

// countMatches returns the total number of emails matching the query.
func (idx *Index) countMatches(q string) int {
	var n int
	_ = idx.db.QueryRow(
		"SELECT COUNT(*) FROM emails WHERE contains(LOWER(subject), ?) OR contains(LOWER(body_text), ?)",
		q, q).Scan(&n)
	return n
}

// queryMatches returns a page of matching emails with snippet data.
func (idx *Index) queryMatches(q string, offset, limit int) []Hit {
	const base = `SELECT path, subject, from_addr, to_addr, date, size, body_text
		FROM emails
		WHERE contains(LOWER(subject), ?) OR contains(LOWER(body_text), ?)
		ORDER BY date DESC`

	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = idx.db.Query(base+" LIMIT ? OFFSET ?", q, q, limit, offset)
	} else {
		rows, err = idx.db.Query(base, q, q)
	}
	if err != nil {
		log.Printf("WARN: queryMatches: %v", err)
		return make([]Hit, 0)
	}
	defer rows.Close()
	return scanHits(rows, q, true)
}

// scanHits reads rows into Hit slices. When withBody is true, body_text is
// scanned for snippet generation.
func scanHits(rows *sql.Rows, query string, withBody bool) []Hit {
	hits := make([]Hit, 0)
	for rows.Next() {
		var e eml.Email
		var scanErr error
		if withBody {
			scanErr = rows.Scan(&e.Path, &e.Subject, &e.From, &e.To, &e.Date, &e.Size, &e.BodyText)
		} else {
			scanErr = rows.Scan(&e.Path, &e.Subject, &e.From, &e.To, &e.Date, &e.Size)
		}
		if scanErr != nil {
			log.Printf("WARN: scan row: %v", scanErr)
			continue
		}
		var snippet string
		if query != "" {
			snippet = eml.Snippet(e, query, 80)
		}
		hits = append(hits, Hit{Email: e, Snippet: snippet})
	}
	return hits
}

// EmailDir returns the root email directory path.
func (idx *Index) EmailDir() string {
	return idx.emailDir
}

// Stats holds index statistics.
type Stats struct {
	TotalEmails int       `json:"total_emails"`
	IndexedAt   time.Time `json:"indexed_at"`
	EmailDir    string    `json:"email_dir"`
	IndexPath   string    `json:"index_path,omitempty"`
}

// Stats returns current index statistics.
func (idx *Index) Stats() Stats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return Stats{
		TotalEmails: idx.total,
		IndexedAt:   idx.buildAt,
		EmailDir:    idx.emailDir,
		IndexPath:   idx.indexPath,
	}
}

// extractChecksum returns the 16-hex-char checksum prefix from a filename
// like "a1b2c3d4e5f67890-123.eml", or "" if the filename doesn't match.
func extractChecksum(name string) string {
	m := reChecksum.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1]
}
