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

			extracted, errCount, importErr := Import(pstPath, emailDir, onProgress, nil)
			if importErr != nil {
				if strings.Contains(importErr.Error(), "readpst not installed") {
					t.Skipf("go-pst failed and readpst fallback unavailable: %v (install pst-utils to test OST)", importErr)
				}
				t.Fatalf("Import: %v", importErr)
			}

			if extracted == 0 && errCount == 0 {
				t.Logf("file has no extractable messages (may contain only appointments/contacts)")
			}

			// Verify extracted files were written (.eml, .vcf, .ics, .txt).
			var fileCount int
			filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					ext := strings.ToLower(filepath.Ext(path))
					if ext == ".eml" || ext == ".vcf" || ext == ".ics" || ext == ".txt" {
						fileCount++
					}
				}
				return nil
			})

			if extracted > 0 && fileCount != extracted {
				t.Errorf("extracted=%d but found %d files (.eml/.vcf/.ics/.txt)", extracted, fileCount)
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
	extracted, errCount, err := Import(pstPath, emailDir, func(phase string, current, total int) {}, nil)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	var fileCount int
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".eml" || ext == ".vcf" || ext == ".ics" || ext == ".txt" {
				fileCount++
			}
		}
		return nil
	})

	if extracted != fileCount {
		t.Errorf("extracted=%d but found %d files", extracted, fileCount)
	}
	if extracted == 0 {
		t.Fatalf("expected at least one item from %s (errCount=%d)", filepath.Base(pstPath), errCount)
	}

	// Verify at least one .eml file has RFC822-style headers.
	var sampleEmlPath string
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || sampleEmlPath != "" {
			return err
		}
		if !info.IsDir() && strings.ToLower(filepath.Ext(path)) == ".eml" {
			sampleEmlPath = path
		}
		return nil
	})
	if sampleEmlPath != "" {
		body, err := os.ReadFile(sampleEmlPath)
		if err != nil {
			t.Errorf("read sample .eml: %v", err)
		} else if !containsAll(body, "From:", "Subject:") {
			t.Errorf("sample .eml missing RFC822 headers; got %d bytes", len(body))
		}
	}

	// Verify .vcf, .ics, .txt formats when present.
	verifyNonEmailFormats(t, emailDir)

	t.Logf("extracted %d items (eml/vcf/ics/txt), %d errors", extracted, errCount)
}

// verifyNonEmailFormats checks that extracted .vcf, .ics, .txt files have valid content.
func verifyNonEmailFormats(t *testing.T, emailDir string) {
	var vcfCount, icsCount, txtCount int
	filepath.Walk(emailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".vcf":
			vcfCount++
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read .vcf %s: %v", path, err)
				return nil
			}
			if !containsAll(body, "BEGIN:VCARD", "END:VCARD") {
				t.Errorf(".vcf %s missing vCard markers", path)
			}
		case ".ics":
			icsCount++
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read .ics %s: %v", path, err)
				return nil
			}
			if !containsAll(body, "BEGIN:VCALENDAR", "END:VCALENDAR", "BEGIN:VEVENT", "END:VEVENT") {
				t.Errorf(".ics %s missing iCalendar markers", path)
			}
		case ".txt":
			txtCount++
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read .txt %s: %v", path, err)
				return nil
			}
			if len(body) == 0 {
				t.Errorf(".txt %s is empty", path)
			}
		}
		return nil
	})
	if vcfCount+icsCount+txtCount > 0 {
		t.Logf("verified formats: %d .vcf, %d .ics, %d .txt", vcfCount, icsCount, txtCount)
	}
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
