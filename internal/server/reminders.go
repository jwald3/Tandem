package server

import (
	"net/http"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// --- Agenda panel (beside the chat) ---

// agendaData is the at-a-glance list of what's due.
type agendaData struct {
	Overdue  []store.Reminder
	Today    []store.Reminder
	Upcoming []store.Reminder // the next 7 days
	Later    int              // open reminders due after that or undated
}

func (d agendaData) Empty() bool {
	return len(d.Overdue)+len(d.Today)+len(d.Upcoming) == 0
}

func (app *App) agendaData() agendaData {
	var d agendaData
	open, _ := app.store.ListReminders(store.ReminderFilter{Limit: 500})
	today, weekOut := dates.Today(), dates.DaysFromToday(7)
	for _, r := range open {
		switch {
		case r.DueDate == "" || r.DueDate > weekOut:
			d.Later++
		case r.DueDate < today:
			d.Overdue = append(d.Overdue, r)
		case r.DueDate == today:
			d.Today = append(d.Today, r)
		default:
			d.Upcoming = append(d.Upcoming, r)
		}
	}
	return d
}

func (app *App) handleAgenda(w http.ResponseWriter, r *http.Request) {
	app.render(w, "agenda.html", app.agendaData())
}

// --- Reminders tab ---

// dayGroup is a run of reminders due on the same date.
type dayGroup struct {
	Date  string
	Items []store.Reminder
}

type remindersData struct {
	page
	View     string // "open" or "done"
	Overdue  []store.Reminder
	Days     []dayGroup // today onward, grouped by date
	NoDate   []store.Reminder
	Done     []store.Reminder
	Contacts []store.Contact
	Next     string // where forms return to
}

func (app *App) handleRemindersPage(w http.ResponseWriter, r *http.Request) {
	d := remindersData{page: newPage("Reminders", "reminders"), View: "open", Contacts: app.allContacts(), Next: r.URL.RequestURI()}
	if r.URL.Query().Get("view") == "done" {
		d.View = "done"
		d.Done, _ = app.store.ListReminders(store.ReminderFilter{Status: store.ReminderDone, Limit: 300})
		app.render(w, "reminders.html", d)
		return
	}
	open, err := app.store.ListReminders(store.ReminderFilter{Limit: 1000})
	if err != nil {
		serverError(w, err)
		return
	}
	today := dates.Today()
	for _, rem := range open {
		switch {
		case rem.DueDate == "":
			d.NoDate = append(d.NoDate, rem)
		case rem.DueDate < today:
			d.Overdue = append(d.Overdue, rem)
		default:
			if n := len(d.Days); n > 0 && d.Days[n-1].Date == rem.DueDate {
				d.Days[n-1].Items = append(d.Days[n-1].Items, rem)
			} else {
				d.Days = append(d.Days, dayGroup{Date: rem.DueDate, Items: []store.Reminder{rem}})
			}
		}
	}
	app.render(w, "reminders.html", d)
}

// formReminder reads and validates the reminder form fields.
func formReminder(r *http.Request) (store.Reminder, string) {
	rem := store.Reminder{
		Title:     field(r, "title"),
		Notes:     field(r, "notes"),
		DueDate:   field(r, "due_date"),
		ContactID: formContactID(r),
	}
	if rem.Title == "" {
		return rem, "A reminder needs a title."
	}
	if rem.DueDate != "" && !dates.Valid(rem.DueDate) {
		return rem, "That date isn't valid."
	}
	tm, err := dates.NormalizeTime(field(r, "due_time"))
	if err != nil {
		return rem, "That time isn't valid."
	}
	if rem.DueDate != "" {
		rem.DueTime = tm // a time without a date is dropped
	}
	return rem, ""
}

func (app *App) handleAddReminder(w http.ResponseWriter, r *http.Request) {
	rem, msg := formReminder(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	if _, err := app.store.CreateReminder(rem); err != nil {
		serverError(w, err)
		return
	}
	redirectBack(w, r, "/reminders")
}

func (app *App) handleEditReminder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	rem, msg := formReminder(r)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	rem.ID = id
	if _, err := app.store.UpdateReminder(rem); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/reminders")
}

// handleToggleReminder completes or reopens a reminder. From the chat page's
// agenda panel (an HTMX request) it answers with the refreshed panel instead
// of a redirect.
func (app *App) handleToggleReminder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	rem, err := app.store.GetReminder(id)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if _, err := app.store.SetReminderDone(id, !rem.Done()); err != nil {
		serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		app.render(w, "agenda.html", app.agendaData())
		return
	}
	redirectBack(w, r, "/reminders")
}

func (app *App) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if err := app.store.DeleteReminder(id); err != nil {
		storeError(w, r, err)
		return
	}
	redirectBack(w, r, "/reminders")
}
