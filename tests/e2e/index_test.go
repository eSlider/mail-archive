//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eslider/mails/internal/search/index"
)

// --- DuckDB Index Tests ---
//
// These tests verify the DuckDB-backed keyword search index using synthetic
// .eml files created in temporary directories. No external services needed.
//
// Cases covered:
//   - Build index from .eml files on disk
//   - Stats reflect correct email count and build timestamp
//   - Keyword search finds matching emails by subject and body
//   - Keyword search returns nothing for non-matching queries
//   - Empty query returns all emails ordered by date DESC
//   - Pagination: offset + limit slices results without overlap
//   - Parquet export creates a non-empty file
//   - Parquet import restores the index (round-trip)
//   - Rebuild index replaces stale data with fresh scan
//   - Deduplication: files sharing a checksum prefix are indexed once

// writeSyntheticEml creates a minimal .eml file in dir.
func writeSyntheticEml(t *testing.T, dir, filename, subject, from, to, date, body string) string {
	t.Helper()
	content := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@test.local>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		from, to, subject, date, filename, body,
	)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedSyntheticEmails creates 5 diverse .eml files and returns the directory.
func seedSyntheticEmails(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Filenames use the {checksum}-{id}.eml convention so the index dedup works.
	writeSyntheticEml(t, dir, "aaaa111122223333-1.eml",
		"Kubernetes cluster alert",
		"ops@infra.dev", "team@company.com",
		"Mon, 01 Jan 2024 08:00:00 +0000",
		"Node pool cpu utilization exceeded 90% threshold in production cluster.")

	writeSyntheticEml(t, dir, "bbbb444455556666-2.eml",
		"Q4 Financial Report",
		"cfo@acme.com", "board@acme.com",
		"Tue, 15 Feb 2024 10:30:00 +0000",
		"Attached is the quarterly financial report showing revenue growth of 23% year-over-year.")

	writeSyntheticEml(t, dir, "cccc777788889999-3.eml",
		"Invitation: Team offsite in Berlin",
		"hr@company.com", "all-hands@company.com",
		"Wed, 20 Mar 2024 14:00:00 +0000",
		"You are invited to a two-day team offsite event in Berlin on April 15-16.")

	writeSyntheticEml(t, dir, "ddddaaaabbbbcccc-4.eml",
		"Security patch CVE-2024-1234",
		"security@vendor.io", "admin@company.com",
		"Thu, 10 Apr 2024 16:45:00 +0000",
		"A critical vulnerability has been found in libxml2. Please apply the patch immediately.")

	writeSyntheticEml(t, dir, "eeeeddddccccbbbb-5.eml",
		"Golang 1.22 release notes",
		"release@golang.org", "gophers@golang.org",
		"Fri, 10 May 2024 07:00:00 +0000",
		"Go 1.22 brings range-over-func iterators, improved routing patterns, and trace improvements.")

	return dir
}

func TestIndex_BuildAndStats(t *testing.T) {
	// Case: Build an index from .eml files; Stats must reflect correct count.
	dir := seedSyntheticEmails(t)
	indexPath := filepath.Join(t.TempDir(), "index.parquet")

	idx, err := index.New(dir, indexPath)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer idx.Close()

	total, errCount := idx.Build()
	if errCount != 0 {
		t.Errorf("expected 0 parse errors, got %d", errCount)
	}
	if total != 5 {
		t.Fatalf("expected 5 indexed emails, got %d", total)
	}

	stats := idx.Stats()
	if stats.TotalEmails != 5 {
		t.Errorf("Stats.TotalEmails = %d, want 5", stats.TotalEmails)
	}
	if stats.IndexedAt.IsZero() {
		t.Error("Stats.IndexedAt should be non-zero after Build()")
	}
	t.Logf("Index built: %d emails, indexed at %s", stats.TotalEmails, stats.IndexedAt)
}

func TestIndex_KeywordSearch(t *testing.T) {
	// Case: Search by keywords present in subject or body.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	tests := []struct {
		query   string
		wantMin int
		wantMax int
		desc    string
	}{
		{"kubernetes", 1, 1, "matches subject of ops alert"},
		{"financial", 1, 1, "matches body of finance email"},
		{"berlin", 1, 1, "matches body of offsite invitation"},
		{"vulnerability", 1, 1, "matches body of security patch email"},
		{"golang", 1, 1, "matches subject of release notes"},
		{"revenue", 1, 1, "matches body keyword 'revenue growth'"},
		{"KUBERNETES", 1, 1, "case-insensitive search"},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			result := idx.Search(tc.query, 0, 50)
			if len(result.Hits) < tc.wantMin {
				t.Errorf("%s: got %d hits, want >= %d", tc.desc, len(result.Hits), tc.wantMin)
			}
			if tc.wantMax > 0 && len(result.Hits) > tc.wantMax {
				t.Errorf("%s: got %d hits, want <= %d", tc.desc, len(result.Hits), tc.wantMax)
			}
			t.Logf("Search %q: %d hits - %s", tc.query, len(result.Hits), tc.desc)
		})
	}
}

func TestIndex_NoMatch(t *testing.T) {
	// Case: Search for a term that exists in no email -> 0 hits.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	result := idx.Search("xylophonezebra42", 0, 50)
	if len(result.Hits) != 0 {
		t.Errorf("expected 0 hits for nonsense query, got %d", len(result.Hits))
	}
	if result.Total != 0 {
		t.Errorf("expected Total=0 for nonsense query, got %d", result.Total)
	}
	t.Log("No-match query: 0 hits as expected")
}

func TestIndex_EmptyQueryReturnsAll(t *testing.T) {
	// Case: Empty string query returns all indexed emails.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	result := idx.Search("", 0, 50)
	if result.Total != 5 {
		t.Errorf("empty query Total = %d, want 5", result.Total)
	}
	if len(result.Hits) != 5 {
		t.Errorf("empty query Hits = %d, want 5", len(result.Hits))
	}
	t.Logf("Empty query: total=%d, hits=%d", result.Total, len(result.Hits))
}

func TestIndex_Pagination(t *testing.T) {
	// Case: Paginate results without overlap or omission.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	page1 := idx.Search("", 0, 2)
	page2 := idx.Search("", 2, 2)
	page3 := idx.Search("", 4, 2)

	if len(page1.Hits) != 2 {
		t.Errorf("page1: got %d hits, want 2", len(page1.Hits))
	}
	if len(page2.Hits) != 2 {
		t.Errorf("page2: got %d hits, want 2", len(page2.Hits))
	}
	if len(page3.Hits) != 1 {
		t.Errorf("page3: got %d hits, want 1", len(page3.Hits))
	}

	// Verify no duplicates across pages.
	seen := make(map[string]bool)
	for _, h := range page1.Hits {
		seen[h.Path] = true
	}
	for _, h := range page2.Hits {
		if seen[h.Path] {
			t.Errorf("page2 contains duplicate from page1: %s", h.Path)
		}
		seen[h.Path] = true
	}
	for _, h := range page3.Hits {
		if seen[h.Path] {
			t.Errorf("page3 contains duplicate: %s", h.Path)
		}
	}

	// Verify all 5 unique emails were returned.
	if len(seen)+len(page3.Hits) != 5 {
		t.Errorf("pagination covered %d unique emails, want 5", len(seen)+len(page3.Hits))
	}
	t.Logf("Pagination: page1=%d, page2=%d, page3=%d, total unique=%d",
		len(page1.Hits), len(page2.Hits), len(page3.Hits), len(seen)+len(page3.Hits))
}

func TestIndex_DateOrdering(t *testing.T) {
	// Case: Empty query results are ordered by date DESC (newest first).
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	result := idx.Search("", 0, 50)
	for i := 1; i < len(result.Hits); i++ {
		prev := result.Hits[i-1].Date
		curr := result.Hits[i].Date
		if curr.After(prev) {
			t.Errorf("hit[%d] date %s is after hit[%d] date %s — not DESC", i, curr, i-1, prev)
		}
	}
	t.Log("Date ordering: descending order confirmed")
}

func TestIndex_ParquetRoundTrip(t *testing.T) {
	// Case: Export index to Parquet, then re-open from Parquet and search.
	dir := seedSyntheticEmails(t)
	indexPath := filepath.Join(t.TempDir(), "test_index.parquet")

	// Build and export.
	idx1, err := index.New(dir, indexPath)
	if err != nil {
		t.Fatal(err)
	}
	total, _ := idx1.Build()
	idx1.Close()

	if total != 5 {
		t.Fatalf("build: expected 5, got %d", total)
	}

	// Verify Parquet file exists and is non-empty.
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("parquet file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("parquet file is empty")
	}
	t.Logf("Parquet file: %d bytes", info.Size())

	// Re-open from Parquet (no rebuild needed).
	idx2, err := index.New(dir, indexPath)
	if err != nil {
		t.Fatalf("re-open from parquet: %v", err)
	}
	defer idx2.Close()

	stats := idx2.Stats()
	if stats.TotalEmails != 5 {
		t.Errorf("re-opened index has %d emails, want 5", stats.TotalEmails)
	}

	// Search on the imported index.
	result := idx2.Search("kubernetes", 0, 50)
	if len(result.Hits) < 1 {
		t.Error("search after parquet import returned 0 hits for 'kubernetes'")
	}
	t.Logf("Parquet round-trip: %d emails, search 'kubernetes' = %d hits", stats.TotalEmails, len(result.Hits))
}

func TestIndex_Rebuild(t *testing.T) {
	// Case: Rebuild replaces stale index with fresh data from disk.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	total1, _ := idx.Build()
	if total1 != 5 {
		t.Fatalf("first build: expected 5, got %d", total1)
	}

	// Add a 6th email.
	writeSyntheticEml(t, dir, "ffff000011112222-6.eml",
		"New hire onboarding checklist",
		"hr@company.com", "newbie@company.com",
		"Sat, 01 Jun 2024 09:00:00 +0000",
		"Welcome! Please complete your onboarding checklist by end of week.")

	// Rebuild should pick up the new file.
	total2, _ := idx.Build()
	if total2 != 6 {
		t.Errorf("rebuild: expected 6, got %d", total2)
	}

	result := idx.Search("onboarding", 0, 50)
	if len(result.Hits) < 1 {
		t.Error("rebuild: 'onboarding' not found after adding new email and rebuilding")
	}
	t.Logf("Rebuild: %d -> %d emails, 'onboarding' search = %d hits", total1, total2, len(result.Hits))
}

func TestIndex_Deduplication(t *testing.T) {
	// Case: Two .eml files with the same checksum prefix are deduplicated
	// (only one is indexed).
	dir := t.TempDir()

	// Same checksum prefix "aaaa111122223333", different IDs.
	writeSyntheticEml(t, dir, "aaaa111122223333-100.eml",
		"Duplicate test",
		"a@test.com", "b@test.com",
		"Mon, 01 Jan 2024 08:00:00 +0000",
		"This is the original message.")

	writeSyntheticEml(t, dir, "aaaa111122223333-200.eml",
		"Duplicate test copy",
		"a@test.com", "b@test.com",
		"Mon, 01 Jan 2024 08:00:00 +0000",
		"This is a duplicate by checksum.")

	// Different checksum, should be indexed.
	writeSyntheticEml(t, dir, "bbbb444455556666-300.eml",
		"Unique message",
		"c@test.com", "d@test.com",
		"Tue, 02 Jan 2024 09:00:00 +0000",
		"This is a unique message.")

	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	total, _ := idx.Build()
	// Should be 2: one from aaaa... (deduped), one from bbbb...
	if total != 2 {
		t.Errorf("expected 2 (with dedup), got %d", total)
	}
	t.Logf("Deduplication: 3 files on disk, %d indexed (1 deduped)", total)
}

func TestIndex_SearchWithSnippet(t *testing.T) {
	// Case: Search results include a context snippet around the matched keyword.
	dir := seedSyntheticEmails(t)
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	idx.Build()

	result := idx.Search("revenue", 0, 50)
	if len(result.Hits) < 1 {
		t.Fatal("expected at least 1 hit for 'revenue'")
	}

	hit := result.Hits[0]
	if hit.Snippet == "" {
		t.Error("expected non-empty snippet for keyword match")
	}
	t.Logf("Search 'revenue': snippet = %q", hit.Snippet)
}
