package pst

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// goPstDataDir returns the path to the go-pst module's data/ folder if available.
func goPstDataDir() (string, bool) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/mooijtech/go-pst/v6").Output()
	if err != nil {
		return "", false
	}
	dir := strings.TrimSpace(string(out))
	dataDir := filepath.Join(dir, "data")
	if fi, err := os.Stat(dataDir); err != nil || !fi.IsDir() {
		return "", false
	}
	return dataDir, true
}

// dataDir returns the path to data/ at module root.
// Uses the test file's location to find module root (internal/sync/pst -> ../../../).
func dataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(filename)
	return filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "data"))
}

func TestImportFromDataFiles(t *testing.T) {
	dataRoot := dataDir()
	patterns := []string{
		filepath.Join(dataRoot, "*.pst"),
		filepath.Join(dataRoot, "*.ost"),
	}
	var files []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			t.Fatalf("glob %q: %v", p, err)
		}
		files = append(files, matches...)
	}

	if len(files) == 0 {
		t.Skipf("no *.pst or *.ost files in %s — add PST/OST files to run this test", dataRoot)
	}

	for _, pstPath := range files {
		t.Run(filepath.Base(pstPath), func(t *testing.T) {
			if _, err := os.Stat(pstPath); os.IsNotExist(err) {
				t.Skipf("%s does not exist", pstPath)
			}

			emailDir := t.TempDir()
			var progressCalls int
			onProgress := func(phase string, current, total int) {
				progressCalls++
			}

			extracted, errCount, importErr := Import(pstPath, emailDir, onProgress)
			if importErr != nil {
				if strings.Contains(importErr.Error(), "readpst not installed") {
					t.Skipf("go-pst failed and readpst fallback unavailable: %v (install pst-utils to test OST)", importErr)
				}
				t.Fatalf("Import: %v", importErr)
			}

			if extracted == 0 && errCount == 0 {
				t.Logf("file has no extractable messages (may contain only appointments/contacts)")
			}

			// Verify .eml files were written.
			var emlCount int
			filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && filepath.Ext(path) == ".eml" {
					emlCount++
				}
				return nil
			})

			if extracted > 0 && emlCount != extracted {
				t.Errorf("extracted=%d but found %d .eml files", extracted, emlCount)
			}

			t.Logf("extracted=%d errors=%d progressCalls=%d", extracted, errCount, progressCalls)
		})
	}
}

// TestImportExtractionWorks verifies email extraction using go-pst's bundled test fixtures.
// Ensures Import produces .eml files when given a compatible PST.
func TestImportExtractionWorks(t *testing.T) {
	gopstData, ok := goPstDataDir()
	if !ok {
		t.Skip("go-pst module data dir not found")
	}

	patterns := []string{
		filepath.Join(gopstData, "*.pst"),
	}
	var files []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			t.Fatalf("glob %q: %v", p, err)
		}
		files = append(files, matches...)
	}

	if len(files) == 0 {
		t.Skip("no PST files in go-pst data dir")
	}

	// Prefer enron.pst (real mail) or 32-bit.pst; go-pst fixtures vary in compatibility.
	var pstPath string
	for _, want := range []string{"enron.pst", "32-bit.pst", "support.pst"} {
		for _, f := range files {
			if filepath.Base(f) == want {
				pstPath = f
				break
			}
		}
		if pstPath != "" {
			break
		}
	}
	if pstPath == "" {
		pstPath = files[0]
	}

	emailDir := t.TempDir()
	extracted, errCount, err := Import(pstPath, emailDir, func(phase string, current, total int) {})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	var emlCount int
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".eml" {
			emlCount++
		}
		return nil
	})

	if extracted != emlCount {
		t.Errorf("extracted=%d but found %d .eml files", extracted, emlCount)
	}
	if extracted == 0 {
		t.Fatalf("expected at least one email from %s (errCount=%d)", filepath.Base(pstPath), errCount)
	}

	// Verify .eml files have RFC822-style headers.
	var samplePath string
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || samplePath != "" {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".eml" {
			samplePath = path
		}
		return nil
	})
	if samplePath != "" {
		body, err := os.ReadFile(samplePath)
		if err != nil {
			t.Errorf("read sample .eml: %v", err)
		} else if !containsAll(body, "From:", "Subject:") {
			t.Errorf("sample .eml missing RFC822 headers; got %d bytes", len(body))
		}
	}

	t.Logf("extracted %d emails, %d errors", extracted, errCount)
}

func containsAll(b []byte, subs ...string) bool {
	s := string(b)
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
