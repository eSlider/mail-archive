package eml_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eslider/mails/internal/search/eml"
)

// TestParseFileFull_RealCIDInline verifies a real Gmail-style email with inline image displays correctly.
// Run with: go test -run TestParseFileFull_RealCIDInline -v (skips if file missing)
func TestParseFileFull_RealCIDInline(t *testing.T) {
	path := filepath.Join("..", "..", "..", "users", "019c5708-50e9-7cf4-8acd-9a0bd39271af", "gmail.com", "eslider", "gmail", "allmail", "c54db7b808b5a65a-162582.eml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("real email file not found (users/ is gitignored)")
	}
	fe, err := eml.ParseFileFull(path)
	if err != nil {
		t.Fatalf("ParseFileFull: %v", err)
	}
	if strings.Contains(fe.HTMLBody, "cid:17711756476991") {
		t.Error("cid: reference was not rewritten - inline image will not display")
	}
	if !strings.Contains(fe.HTMLBody, "data:image/png;base64,") {
		t.Error("HTML should contain data URI for inline image")
	}
}
