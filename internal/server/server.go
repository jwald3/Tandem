// Package server is the web UI: HTTP routes, page handlers and the HTMX
// fragments they swap in. Each tab's handlers and view models live in their
// own file.
//
// The chat page is HTMX-driven. The data tabs are plain HTML forms that post
// and redirect back (post/redirect/get), so they work without JavaScript.
package server

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/jwald3/tandem/internal/assistant"
	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
	"github.com/jwald3/tandem/web"
)

// App holds shared server state.
type App struct {
	store   *store.Store
	tmpl    *template.Template
	baseURL string // Anthropic API base URL override, passed to the assistant

	mu     sync.RWMutex
	apiKey string           // current active key ("" = chat disabled)
	envKey bool             // true when the key came from ANTHROPIC_API_KEY (UI is read-only)
	agent  *assistant.Agent // rebuilt whenever the key changes
}

// New builds the app around an open store. anthropicBaseURL overrides the API
// host ("" = the real API). Chat stays disabled until InitAPIKey (or the
// settings panel) supplies a key.
func New(st *store.Store, anthropicBaseURL string) (*App, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &App{store: st, tmpl: tmpl, baseURL: anthropicBaseURL}, nil
}

// Handler returns the app's routes.
func (app *App) Handler() http.Handler {
	mux := http.NewServeMux()

	// Chat (home)
	mux.HandleFunc("GET /", app.handleChatHome)
	mux.HandleFunc("GET /c/{id}", app.handleChatThread)
	mux.HandleFunc("POST /threads/{id}/delete", app.handleDeleteThread)
	mux.HandleFunc("POST /threads/{id}/rename", app.handleRenameThread)
	mux.HandleFunc("POST /chat", app.handleChat)
	mux.HandleFunc("GET /chat/msg/{id}", app.handleChatMessage)
	mux.HandleFunc("POST /chat/msg/{id}/retry", app.handleRetryChat)
	mux.HandleFunc("GET /chat/img/{id}", app.handleChatImage)
	mux.HandleFunc("GET /agenda", app.handleAgenda)
	mux.HandleFunc("GET /settings", app.handleSettingsFragment)
	mux.HandleFunc("POST /settings/key", app.handleSaveKey)
	mux.HandleFunc("POST /settings/key/delete", app.handleClearKey)

	// Reminders
	mux.HandleFunc("GET /reminders", app.handleRemindersPage)
	mux.HandleFunc("POST /reminders", app.handleAddReminder)
	mux.HandleFunc("POST /reminders/{id}", app.handleEditReminder)
	mux.HandleFunc("POST /reminders/{id}/toggle", app.handleToggleReminder)
	mux.HandleFunc("POST /reminders/{id}/delete", app.handleDeleteReminder)

	// Contacts
	mux.HandleFunc("GET /contacts", app.handleContactsPage)
	mux.HandleFunc("POST /contacts", app.handleAddContact)
	mux.HandleFunc("GET /contacts/{id}", app.handleContactPage)
	mux.HandleFunc("POST /contacts/{id}", app.handleEditContact)
	mux.HandleFunc("POST /contacts/{id}/delete", app.handleDeleteContact)

	// Interactions
	mux.HandleFunc("GET /interactions", app.handleInteractionsPage)
	mux.HandleFunc("POST /interactions", app.handleAddInteraction)
	mux.HandleFunc("POST /interactions/{id}", app.handleEditInteraction)
	mux.HandleFunc("POST /interactions/{id}/delete", app.handleDeleteInteraction)

	// Notes
	mux.HandleFunc("GET /notes", app.handleNotesPage)
	mux.HandleFunc("POST /notes", app.handleAddNote)
	mux.HandleFunc("POST /notes/{id}", app.handleEditNote)
	mux.HandleFunc("POST /notes/{id}/delete", app.handleDeleteNote)

	// Embedded static assets (web/static/*).
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(web.Static))))
	return mux
}

// page carries the fields every full page's layout needs.
type page struct {
	PageTitle string
	Active    string // which top-bar tab is highlighted
	Today     string
}

func newPage(title, active string) page {
	return page{PageTitle: title, Active: active, Today: dates.Today()}
}

// --- Helpers shared by the handlers ---

// render executes a named template, logging (not surfacing) failures: by the
// time a template fails, part of the response may already be written.
func (app *App) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

// writeHTML writes a small inline HTML fragment.
func writeHTML(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(s))
}

// pathID parses the {id} path segment.
func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// redirectBack finishes a form post by sending the browser to the form's
// "next" field (a local path), or to fallback.
func redirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	next := r.FormValue("next")
	// Only same-site paths: "/x" but not "//evil.com" or "/\evil.com".
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		next = fallback
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// formContactID reads the optional "contact_id" field; 0 means none.
func formContactID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.FormValue("contact_id"), 10, 64)
	return id
}

// field returns a trimmed form value.
func field(r *http.Request, name string) string {
	return strings.TrimSpace(r.FormValue(name))
}

// serverError reports a failed store call.
func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// badRequest reports invalid form input.
func badRequest(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

// storeError maps ErrNotFound to 404 and anything else to 500.
func storeError(w http.ResponseWriter, r *http.Request, err error) {
	if err == store.ErrNotFound {
		http.NotFound(w, r)
		return
	}
	serverError(w, err)
}

// allContacts lists every contact for the contact pickers in forms.
func (app *App) allContacts() []store.Contact {
	cs, err := app.store.ListContacts("")
	if err != nil {
		log.Printf("list contacts: %v", err)
	}
	return cs
}
