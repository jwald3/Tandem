package server

import (
	"net/http"
	"strconv"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// --- Interactions and Notes tabs ---

// listPage is the shared model for the interactions and notes pages: a
// search box, an optional contact filter, the items, and the add form.
type listPage[T any] struct {
	page
	Query     string
	ContactID int64 // filter; 0 = everyone
	Items     []T
	Contacts  []store.Contact
	Kinds     []string
	Next      string
}

func (app *App) newListPage(r *http.Request, title, active string) (page, string, int64, []store.Contact) {
	cid, _ := strconv.ParseInt(r.URL.Query().Get("contact"), 10, 64)
	return newPage(title, active), r.URL.Query().Get("q"), cid, app.allContacts()
}

func (app *App) handleInteractionsPage(w http.ResponseWriter, r *http.Request) {
	p, q, cid, contacts := app.newListPage(r, "Interactions", "interactions")
	items, err := app.store.ListInteractions(store.InteractionFilter{ContactID: cid, Query: q, Limit: 500})
	if err != nil {
		serverError(w, err)
		return
	}
	app.render(w, "interactions.html", listPage[store.Interaction]{
		page: p, Query: q, ContactID: cid, Items: items, Contacts: contacts,
		Kinds: store.InteractionKinds, Next: r.URL.RequestURI(),
	})
}

func formInteraction(r *http.Request) (store.Interaction, string) {
	in := store.Interaction{
		ContactID: formContactID(r),
		Kind:      field(r, "kind"),
		Date:      field(r, "date"),
		Summary:   field(r, "summary"),
	}
	if in.Summary == "" {
		return in, "An interaction needs a summary."
	}
	if in.Date == "" {
		in.Date = dates.Today()
	} else if !dates.Valid(in.Date) {
		return in, "That date isn't valid."
	}
	return in, ""
}

func (app *App) handleAddInteraction(w http.ResponseWriter, r *http.Request) {
	in, msg := formInteraction(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	if _, err := app.store.CreateInteraction(in); err != nil {
		serverError(w, err)
		return
	}
	redirectBack(w, r, "/interactions")
}

func (app *App) handleEditInteraction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	in, msg := formInteraction(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	in.ID = id
	if _, err := app.store.UpdateInteraction(in); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/interactions")
}

func (app *App) handleDeleteInteraction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if err := app.store.DeleteInteraction(id); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/interactions")
}

func (app *App) handleNotesPage(w http.ResponseWriter, r *http.Request) {
	p, q, cid, contacts := app.newListPage(r, "Notes", "notes")
	items, err := app.store.ListNotes(store.NoteFilter{ContactID: cid, Query: q, Limit: 500})
	if err != nil {
		serverError(w, err)
		return
	}
	app.render(w, "notes.html", listPage[store.Note]{
		page: p, Query: q, ContactID: cid, Items: items, Contacts: contacts, Next: r.URL.RequestURI(),
	})
}

func formNote(r *http.Request) (store.Note, string) {
	n := store.Note{ContactID: formContactID(r), Title: field(r, "title"), Body: field(r, "body")}
	if n.Body == "" {
		return n, "A note can't be empty."
	}
	return n, ""
}

func (app *App) handleAddNote(w http.ResponseWriter, r *http.Request) {
	n, msg := formNote(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	if _, err := app.store.CreateNote(n); err != nil {
		serverError(w, err)
		return
	}
	redirectBack(w, r, "/notes")
}

func (app *App) handleEditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	n, msg := formNote(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	n.ID = id
	if _, err := app.store.UpdateNote(n); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/notes")
}

func (app *App) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if err := app.store.DeleteNote(id); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/notes")
}
