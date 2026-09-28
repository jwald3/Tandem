package server

import (
	"net/http"
	"strconv"

	"github.com/jwald3/tandem/internal/store"
)

// --- Contacts tab ---

type contactsData struct {
	page
	Query    string
	Contacts []store.Contact
	Total    int
}

func (app *App) handleContactsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	cs, err := app.store.ListContacts(q)
	if err != nil {
		serverError(w, err)
		return
	}
	d := contactsData{page: newPage("Contacts", "contacts"), Query: q, Contacts: cs, Total: len(cs)}
	if q != "" {
		all, _ := app.store.ListContacts("")
		d.Total = len(all)
	}
	app.render(w, "contacts.html", d)
}

// formContact reads the contact form fields into c (keeping its id).
func formContact(r *http.Request, c store.Contact) store.Contact {
	c.Name = field(r, "name")
	c.Relationship = field(r, "relationship")
	c.Company = field(r, "company")
	c.Title = field(r, "title")
	c.Email = field(r, "email")
	c.Phone = field(r, "phone")
	c.Birthday = field(r, "birthday")
	c.About = field(r, "about")
	return c
}

func (app *App) handleAddContact(w http.ResponseWriter, r *http.Request) {
	c := formContact(r, store.Contact{})
	if c.Name == "" {
		badRequest(w, "A contact needs a name.")
		return
	}
	c, err := app.store.CreateContact(c)
	if err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/contacts/"+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
}

// contactData is one contact's page: details plus everything linked to them.
type contactData struct {
	page
	Contact      store.Contact
	Open         []store.Reminder
	Done         []store.Reminder
	Interactions []store.Interaction
	Notes        []store.Note
	Contacts     []store.Contact // for the "move to another contact" pickers
	Next         string
}

func (app *App) handleContactPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := app.store.GetContact(id)
	if err != nil {
		storeError(w, r, err)
		return
	}
	d := contactData{page: newPage(c.Name, "contacts"), Contact: c, Contacts: app.allContacts(), Next: r.URL.Path}
	d.Open, _ = app.store.ListReminders(store.ReminderFilter{ContactID: id})
	d.Done, _ = app.store.ListReminders(store.ReminderFilter{ContactID: id, Status: store.ReminderDone, Limit: 10})
	d.Interactions, _ = app.store.ListInteractions(store.InteractionFilter{ContactID: id})
	d.Notes, _ = app.store.ListNotes(store.NoteFilter{ContactID: id})
	app.render(w, "contact.html", d)
}

func (app *App) handleEditContact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	c := formContact(r, store.Contact{ID: id})
	if c.Name == "" {
		badRequest(w, "A contact needs a name.")
		return
	}
	if _, err := app.store.UpdateContact(c); err != nil {
		storeError(w, r, err)
		return
	}
	http.Redirect(w, r, "/contacts/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (app *App) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if err := app.store.DeleteContact(id); err != nil {
		storeError(w, r, err)
		return
	}
	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}
