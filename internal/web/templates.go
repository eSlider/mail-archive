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
	dashboardHTML []byte
)

func init() {
	loadEmbeddedTemplates()
}

func loadEmbeddedTemplates() {
	loginData, _ := templatesFS.ReadFile("templates/auth/login.tmpl")
	registerData, _ := templatesFS.ReadFile("templates/auth/register.tmpl")
	dashboardData, _ := templatesFS.ReadFile("templates/dashboard.tmpl")

	loginTmpl, _ = template.New("login").Parse(string(loginData))
	registerTmpl, _ = template.New("register").Parse(string(registerData))
	dashboardHTML = dashboardData
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
	data := dashboardHTML
	templatesMu.RUnlock()
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

// ReloadTemplates loads templates from TemplateDir if set, otherwise keeps embedded.
// Call after changing TemplateDir (e.g. in tests or dev mode).
func ReloadTemplates() {
	templatesMu.Lock()
	defer templatesMu.Unlock()

	if TemplateDir == "" {
		loginData, _ := templatesFS.ReadFile("templates/auth/login.tmpl")
		registerData, _ := templatesFS.ReadFile("templates/auth/register.tmpl")
		dashboardData, _ := templatesFS.ReadFile("templates/dashboard.tmpl")
		loginTmpl, _ = template.New("login").Parse(string(loginData))
		registerTmpl, _ = template.New("register").Parse(string(registerData))
		dashboardHTML = dashboardData
		return
	}

	loginPath := filepath.Join(TemplateDir, "auth", "login.tmpl")
	registerPath := filepath.Join(TemplateDir, "auth", "register.tmpl")
	dashboardPath := filepath.Join(TemplateDir, "dashboard.tmpl")

	if d, err := os.ReadFile(loginPath); err == nil {
		loginTmpl, _ = template.New("login").Parse(string(d))
	}
	if d, err := os.ReadFile(registerPath); err == nil {
		registerTmpl, _ = template.New("register").Parse(string(d))
	}
	if d, err := os.ReadFile(dashboardPath); err == nil {
		dashboardHTML = d
	}
}
