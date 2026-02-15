// Package web loads HTML templates from embedded files (Gitea-style).
// Templates live in internal/web/templates/ and are embedded at build time.
// Set TemplateDir to override with custom templates at runtime (e.g. for development).
package web

import (
	"embed"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sync"
)

//go:embed templates
var templatesFS embed.FS

var (
	templatesMu   sync.RWMutex
	loginTmpl     *template.Template
	registerTmpl  *template.Template
	dashboardTmpl *template.Template
)

func init() {
	loadEmbeddedTemplates()
}

func loadEmbeddedTemplates() {
	pwaData, _ := templatesFS.ReadFile("templates/partials/pwa.tmpl")
	base := template.Must(template.New("").Parse(string(pwaData)))

	loginData, _ := templatesFS.ReadFile("templates/auth/login.tmpl")
	loginTmpl = template.Must(template.Must(base.Clone()).Parse(string(loginData)))

	registerData, _ := templatesFS.ReadFile("templates/auth/register.tmpl")
	registerTmpl = template.Must(template.Must(base.Clone()).Parse(string(registerData)))

	dashboardData, _ := templatesFS.ReadFile("templates/dashboard.tmpl")
	dashboardTmpl = template.Must(template.Must(base.Clone()).Parse(string(dashboardData)))
}

// renderLogin executes the login template with the given error (empty string for no error).
func renderLogin(w io.Writer, errMsg string) error {
	templatesMu.RLock()
	t := loginTmpl
	templatesMu.RUnlock()
	if t == nil {
		return nil
	}
	return t.Execute(w, struct{ Error string }{Error: errMsg})
}

// renderRegister executes the register template with the given error.
func renderRegister(w io.Writer, errMsg string) error {
	templatesMu.RLock()
	t := registerTmpl
	templatesMu.RUnlock()
	if t == nil {
		return nil
	}
	return t.Execute(w, struct{ Error string }{Error: errMsg})
}

// renderDashboard writes the dashboard HTML (no template vars).
func renderDashboard(w io.Writer) error {
	templatesMu.RLock()
	t := dashboardTmpl
	templatesMu.RUnlock()
	if t == nil {
		return nil
	}
	return t.Execute(w, nil)
}

// ReloadTemplates loads templates from TemplateDir if set, otherwise keeps embedded.
// Call after changing TemplateDir (e.g. in tests or dev mode).
func ReloadTemplates() {
	templatesMu.Lock()
	defer templatesMu.Unlock()

	if TemplateDir == "" {
		pwaData, _ := templatesFS.ReadFile("templates/partials/pwa.tmpl")
		base := template.Must(template.New("").Parse(string(pwaData)))
		loginData, _ := templatesFS.ReadFile("templates/auth/login.tmpl")
		registerData, _ := templatesFS.ReadFile("templates/auth/register.tmpl")
		dashboardData, _ := templatesFS.ReadFile("templates/dashboard.tmpl")
		loginTmpl = template.Must(template.Must(base.Clone()).Parse(string(loginData)))
		registerTmpl = template.Must(template.Must(base.Clone()).Parse(string(registerData)))
		dashboardTmpl = template.Must(template.Must(base.Clone()).Parse(string(dashboardData)))
		return
	}

	pwaPath := filepath.Join(TemplateDir, "partials", "pwa.tmpl")
	loginPath := filepath.Join(TemplateDir, "auth", "login.tmpl")
	registerPath := filepath.Join(TemplateDir, "auth", "register.tmpl")
	dashboardPath := filepath.Join(TemplateDir, "dashboard.tmpl")

	if pwaData, err := os.ReadFile(pwaPath); err == nil {
		base := template.Must(template.New("").Parse(string(pwaData)))
		if d, err := os.ReadFile(loginPath); err == nil {
			loginTmpl = template.Must(template.Must(base.Clone()).Parse(string(d)))
		}
		if d, err := os.ReadFile(registerPath); err == nil {
			registerTmpl = template.Must(template.Must(base.Clone()).Parse(string(d)))
		}
		if d, err := os.ReadFile(dashboardPath); err == nil {
			dashboardTmpl = template.Must(template.Must(base.Clone()).Parse(string(d)))
		}
	}
}
