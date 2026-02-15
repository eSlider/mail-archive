package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eslider/mails/internal/search/index"
)

// newTestIndex creates an in-memory index (no parquet persistence) for testing.
func newTestIndex(t *testing.T, dir string) *index.Index {
	t.Helper()
	idx, err := index.New(dir, "")
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func seedEmails(t *testing.T, dir string) {
	t.Helper()
	sub := filepath.Join(dir, "test-account", "inbox")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	emails := map[string]string{
		"a.eml": "From: alice@test.com\r\nTo: bob@test.com\r\nSubject: Meeting Tomorrow\r\nDate: Mon, 10 Feb 2025 09:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nLet's meet at the trampoline park.\r\n",
		"b.eml": "From: bob@test.com\r\nTo: alice@test.com\r\nSubject: Re: Meeting Tomorrow\r\nDate: Mon, 10 Feb 2025 10:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nSure, sounds good.\r\n",
		"c.eml": "From: carol@test.com\r\nTo: alice@test.com\r\nSubject: Invoice #1234\r\nDate: Tue, 11 Feb 2025 08:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nPlease pay the attached invoice for the xylophone delivery.\r\n",
	}

	for name, content := range emails {
		p := filepath.Join(sub, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildAndSearch(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	idx := newTestIndex(t, dir)
	total, errCount := idx.Build()

	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if errCount != 0 {
		t.Fatalf("errCount = %d, want 0", errCount)
	}

	// Search by subject substring — all results.
	res := idx.Search("meeting", 0, 0)
	if res.Total != 2 {
		t.Errorf("search 'meeting' total = %d, want 2", res.Total)
	}
	if len(res.Hits) != 2 {
		t.Errorf("search 'meeting' hits = %d, want 2", len(res.Hits))
	}

	// Search with limit.
	res = idx.Search("meeting", 0, 1)
	if res.Total != 2 {
		t.Errorf("search 'meeting' total = %d, want 2 (total should count all matches)", res.Total)
	}
	if len(res.Hits) != 1 {
		t.Errorf("search 'meeting' limit=1 hits = %d, want 1", len(res.Hits))
	}

	// Search for invoice.
	res = idx.Search("invoice", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'invoice' total = %d, want 1", res.Total)
	}

	// Empty query returns all.
	res = idx.Search("", 0, 0)
	if res.Total != 3 {
		t.Errorf("search '' total = %d, want 3", res.Total)
	}

	// No match.
	res = idx.Search("nonexistent", 0, 0)
	if res.Total != 0 {
		t.Errorf("search 'nonexistent' total = %d, want 0", res.Total)
	}
}

func TestSearchPaging(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	idx := newTestIndex(t, dir)
	idx.Build()

	// All 3 emails match empty query. Page through with limit=1.
	page1 := idx.Search("", 0, 1)
	if page1.Total != 3 {
		t.Fatalf("total = %d, want 3", page1.Total)
	}
	if len(page1.Hits) != 1 {
		t.Fatalf("page1 hits = %d, want 1", len(page1.Hits))
	}
	if page1.Offset != 0 {
		t.Errorf("page1 offset = %d, want 0", page1.Offset)
	}

	page2 := idx.Search("", 1, 1)
	if page2.Total != 3 {
		t.Errorf("page2 total = %d, want 3", page2.Total)
	}
	if len(page2.Hits) != 1 {
		t.Errorf("page2 hits = %d, want 1", len(page2.Hits))
	}
	if page2.Offset != 1 {
		t.Errorf("page2 offset = %d, want 1", page2.Offset)
	}

	page3 := idx.Search("", 2, 1)
	if len(page3.Hits) != 1 {
		t.Errorf("page3 hits = %d, want 1", len(page3.Hits))
	}

	// All three pages should have different emails.
	if page1.Hits[0].Path == page2.Hits[0].Path {
		t.Error("page1 and page2 returned the same email")
	}
	if page2.Hits[0].Path == page3.Hits[0].Path {
		t.Error("page2 and page3 returned the same email")
	}

	// Beyond last page.
	page4 := idx.Search("", 3, 1)
	if page4.Total != 3 {
		t.Errorf("page4 total = %d, want 3", page4.Total)
	}
	if len(page4.Hits) != 0 {
		t.Errorf("page4 hits = %d, want 0", len(page4.Hits))
	}

	// Larger page size than results.
	all := idx.Search("", 0, 100)
	if len(all.Hits) != 3 {
		t.Errorf("all hits = %d, want 3", len(all.Hits))
	}
}

func TestSearchBodyText(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	idx := newTestIndex(t, dir)
	idx.Build()

	// "trampoline" only appears in body of email a.eml, not in any subject.
	res := idx.Search("trampoline", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'trampoline' total = %d, want 1", res.Total)
	}
	if res.Total > 0 && res.Hits[0].Snippet == "" {
		t.Error("expected non-empty snippet for body match")
	}

	// "xylophone" only appears in body of email c.eml.
	res = idx.Search("xylophone", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'xylophone' total = %d, want 1", res.Total)
	}
}

func TestSearchSnippetForSubjectMatch(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	idx := newTestIndex(t, dir)
	idx.Build()

	// "Invoice" is in subject — snippet should reference the subject.
	res := idx.Search("Invoice", 0, 0)
	if res.Total < 1 {
		t.Fatal("expected at least 1 result")
	}
	if res.Hits[0].Snippet == "" {
		t.Error("expected non-empty snippet")
	}
}

func TestStats(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	idx := newTestIndex(t, dir)
	idx.Build()

	s := idx.Stats()
	if s.TotalEmails != 3 {
		t.Errorf("stats total = %d, want 3", s.TotalEmails)
	}
	if s.IndexedAt.IsZero() {
		t.Error("stats indexed_at should not be zero")
	}
}

func TestBuildDeduplicatesByChecksum(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "account", "inbox")
	allmail := filepath.Join(dir, "account", "allmail")
	if err := os.MkdirAll(inbox, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(allmail, 0755); err != nil {
		t.Fatal(err)
	}

	content := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Duplicate Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nBody.\r\n"
	os.WriteFile(filepath.Join(inbox, "a1b2c3d4e5f67890-100.eml"), []byte(content), 0644)
	os.WriteFile(filepath.Join(allmail, "a1b2c3d4e5f67890-5000.eml"), []byte(content), 0644)

	content2 := "From: x@y.com\r\nTo: z@w.com\r\nSubject: Unique Email\r\nDate: Tue, 11 Feb 2025 08:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nDifferent body.\r\n"
	os.WriteFile(filepath.Join(inbox, "bbbbccccddddeeee-200.eml"), []byte(content2), 0644)

	content3 := "From: foo@bar.com\r\nTo: baz@qux.com\r\nSubject: Legacy Email\r\nDate: Wed, 12 Feb 2025 09:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nLegacy.\r\n"
	os.WriteFile(filepath.Join(inbox, "2025-02-12_legacy_email.eml"), []byte(content3), 0644)

	idx := newTestIndex(t, dir)
	total, errCount := idx.Build()

	if errCount != 0 {
		t.Errorf("errCount = %d, want 0", errCount)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (duplicate should be excluded)", total)
	}

	res := idx.Search("Duplicate Test", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'Duplicate Test' total = %d, want 1", res.Total)
	}
	res = idx.Search("Unique Email", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'Unique Email' total = %d, want 1", res.Total)
	}
	res = idx.Search("Legacy Email", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'Legacy Email' total = %d, want 1", res.Total)
	}
}

func TestBuildEmptyDir(t *testing.T) {
	dir := t.TempDir()
	idx := newTestIndex(t, dir)
	total, errCount := idx.Build()

	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if errCount != 0 {
		t.Errorf("errCount = %d, want 0", errCount)
	}
}

func TestParquetPersistence(t *testing.T) {
	dir := t.TempDir()
	seedEmails(t, dir)

	parquetPath := filepath.Join(t.TempDir(), "test.parquet")

	// Build and save to parquet.
	idx1, err := index.New(dir, parquetPath)
	if err != nil {
		t.Fatalf("index.New: %v", err)
	}
	total, _ := idx1.Build()
	if total != 3 {
		t.Fatalf("build total = %d, want 3", total)
	}
	idx1.Close()

	// Verify parquet file exists.
	info, err := os.Stat(parquetPath)
	if err != nil {
		t.Fatalf("parquet file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("parquet file is empty")
	}
	t.Logf("Parquet file size: %d bytes", info.Size())

	// Load from parquet — should not need to re-parse .eml files.
	idx2, err := index.New(dir, parquetPath)
	if err != nil {
		t.Fatalf("index.New (reload): %v", err)
	}
	defer idx2.Close()

	s := idx2.Stats()
	if s.TotalEmails != 3 {
		t.Errorf("reloaded total = %d, want 3", s.TotalEmails)
	}

	// Search should work on the reloaded index.
	res := idx2.Search("meeting", 0, 0)
	if res.Total != 2 {
		t.Errorf("search 'meeting' after reload = %d, want 2", res.Total)
	}

	res = idx2.Search("trampoline", 0, 0)
	if res.Total != 1 {
		t.Errorf("search 'trampoline' after reload = %d, want 1", res.Total)
	}
}

func TestSearchMultiDeduplicatesByChecksum(t *testing.T) {
	// Simulate re-imported PST: two accounts with same emails (same content = same checksum).
	// SearchMulti should deduplicate so each email appears once.
	root := t.TempDir()

	// Account 1: inbox with checksum-named emails
	dir1 := filepath.Join(root, "pst-import-1")
	inbox1 := filepath.Join(dir1, "inbox")
	if err := os.MkdirAll(inbox1, 0755); err != nil {
		t.Fatal(err)
	}
	eml1 := "From: a@b.com\r\nTo: c@d.com\r\nSubject: Re-import Test\r\nDate: Mon, 10 Feb 2025 12:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nSame content in both imports.\r\n"
	os.WriteFile(filepath.Join(inbox1, "a1b2c3d4e5f60001-1.eml"), []byte(eml1), 0644)
	eml2 := "From: x@y.com\r\nTo: z@w.com\r\nSubject: Unique One\r\nDate: Tue, 11 Feb 2025 08:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nOnly in first.\r\n"
	os.WriteFile(filepath.Join(inbox1, "bbbb111122223333-2.eml"), []byte(eml2), 0644)

	// Account 2: same emails (re-imported PST) — different paths, same checksums
	dir2 := filepath.Join(root, "pst-import-2")
	inbox2 := filepath.Join(dir2, "inbox")
	if err := os.MkdirAll(inbox2, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(inbox2, "a1b2c3d4e5f60001-1.eml"), []byte(eml1), 0644) // same checksum
	os.WriteFile(filepath.Join(inbox2, "bbbb111122223333-99.eml"), []byte(eml2), 0644) // same checksum

	parquet1 := filepath.Join(root, "idx1.parquet")
	parquet2 := filepath.Join(root, "idx2.parquet")

	idx1, err := index.New(dir1, parquet1)
	if err != nil {
		t.Fatalf("index.New 1: %v", err)
	}
	idx1.Build()
	idx1.Close()

	idx2, err := index.New(dir2, parquet2)
	if err != nil {
		t.Fatalf("index.New 2: %v", err)
	}
	idx2.Build()
	idx2.Close()

	// SearchMulti across both accounts — should deduplicate by checksum
	accounts := []index.AccountIndex{
		{ID: "acct-1", IndexPath: parquet1},
		{ID: "acct-2", IndexPath: parquet2},
	}
	result := index.SearchMulti(accounts, "", 0, 100)

	// 2 unique emails, not 4 (2 per account)
	if result.Total != 2 {
		t.Errorf("SearchMulti total = %d, want 2 (deduplicated)", result.Total)
	}
	if len(result.Hits) != 2 {
		t.Errorf("SearchMulti hits = %d, want 2", len(result.Hits))
	}
	t.Logf("SearchMulti: 4 rows across 2 accounts -> %d unique (deduplicated)", result.Total)
}
