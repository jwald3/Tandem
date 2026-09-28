package store

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	// Close before TempDir cleanup so Windows can delete the file.
	t.Cleanup(func() { st.Close() })
	return st
}

func mustContact(t *testing.T, st *Store, name, rel string) Contact {
	t.Helper()
	c, err := st.CreateContact(Contact{Name: name, Relationship: rel})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContactCRUDAndSummary(t *testing.T) {
	st := newTestStore(t)
	alex := mustContact(t, st, "  Alex Rivera ", "coworker")
	if alex.ID == 0 || alex.Name != "Alex Rivera" {
		t.Fatalf("create should trim and assign an id: %+v", alex)
	}
	alex.Company, alex.Title = "Acme", "PM"
	alex, err := st.UpdateContact(alex)
	if err != nil || alex.Subtitle() != "PM, Acme · coworker" {
		t.Fatalf("update: %v, subtitle %q", err, alex.Subtitle())
	}
	if _, err := st.UpdateContact(Contact{ID: 999, Name: "x"}); err != ErrNotFound {
		t.Fatalf("update missing: %v", err)
	}

	st.CreateReminder(Reminder{Title: "Meet about Atlas", DueDate: "2026-09-28", ContactID: alex.ID})
	st.CreateReminder(Reminder{Title: "Send deck", DueDate: "2026-10-05", ContactID: alex.ID})
	st.CreateInteraction(Interaction{ContactID: alex.ID, Kind: "call", Date: "2026-09-20", Summary: "Kickoff"})

	list, err := st.ListContacts("acme")
	if err != nil || len(list) != 1 {
		t.Fatalf("search by company: %v %+v", err, list)
	}
	got := list[0]
	if got.OpenReminders != 2 || got.NextDue != "2026-09-28" || got.LastInteraction != "2026-09-20" {
		t.Fatalf("summary fields wrong: %+v", got)
	}
	if list, _ := st.ListContacts("50%"); len(list) != 0 {
		t.Fatal("LIKE wildcards in the query must be matched literally")
	}
}

func TestFindContacts(t *testing.T) {
	st := newTestStore(t)
	alex := mustContact(t, st, "Alex Rivera", "coworker")
	mustContact(t, st, "Alexandra Diaz", "friend")
	mustContact(t, st, "Alex", "neighbor") // exact-name match must beat partials

	for query, want := range map[string]int{
		"alex rivera": 1,
		"al rivera":   1,
		"Rivera":      1,
		"alex":        1, // exact match on the neighbor
		"al":          3,
		"nobody":      0,
	} {
		got, err := st.FindContacts(query)
		if err != nil || len(got) != want {
			t.Errorf("FindContacts(%q) = %d matches (%v), want %d", query, len(got), err, want)
		}
	}
	if got, _ := st.FindContacts("al  RIVERA"); len(got) != 1 || got[0].ID != alex.ID {
		t.Errorf("extra whitespace and case should not matter: %+v", got)
	}
}

func TestDeletingContactUnlinksItsItems(t *testing.T) {
	st := newTestStore(t)
	alex := mustContact(t, st, "Alex Rivera", "coworker")
	r, _ := st.CreateReminder(Reminder{Title: "Meet", ContactID: alex.ID})
	i, _ := st.CreateInteraction(Interaction{ContactID: alex.ID, Summary: "Lunch", Kind: "lunch"})
	n, _ := st.CreateNote(Note{ContactID: alex.ID, Body: "Prefers mornings"})
	if r.ContactName != "Alex Rivera" || i.Kind != "meeting" || n.ContactName != "Alex Rivera" {
		t.Fatalf("items should join the contact name (and normalize kind): %+v %+v %+v", r, i, n)
	}

	if err := st.DeleteContact(alex.ID); err != nil {
		t.Fatal(err)
	}
	r, err := st.GetReminder(r.ID)
	if err != nil || r.ContactID != 0 || r.ContactName != "" {
		t.Fatalf("reminder should survive, unlinked: %+v %v", r, err)
	}
	i, _ = st.GetInteraction(i.ID)
	n, _ = st.GetNote(n.ID)
	if i.ContactID != 0 || n.ContactID != 0 {
		t.Fatalf("interaction/note should be unlinked: %+v %+v", i, n)
	}
}

func TestReminderOrderingAndFilters(t *testing.T) {
	st := newTestStore(t)
	alex := mustContact(t, st, "Alex Rivera", "coworker")
	mk := func(title, date, tm string, cid int64) Reminder {
		r, err := st.CreateReminder(Reminder{Title: title, DueDate: date, DueTime: tm, ContactID: cid})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	someday := mk("Someday", "", "", 0)
	late := mk("Afternoon", "2026-09-28", "15:00", alex.ID)
	allDay := mk("All day", "2026-09-28", "", 0)
	early := mk("Earlier", "2026-09-20", "", 0)

	open, _ := st.ListReminders(ReminderFilter{})
	order := []int64{early.ID, allDay.ID, late.ID, someday.ID}
	if len(open) != 4 {
		t.Fatalf("want 4 open, got %d", len(open))
	}
	for i, id := range order {
		if open[i].ID != id {
			t.Fatalf("position %d: got %q, want id %d (soonest first, all-day before timed, undated last)", i, open[i].Title, id)
		}
	}

	if _, err := st.SetReminderDone(early.ID, true); err != nil {
		t.Fatal(err)
	}
	if open, _ := st.ListReminders(ReminderFilter{}); len(open) != 3 {
		t.Fatalf("completed reminder should leave the open list, got %d", len(open))
	}
	if done, _ := st.ListReminders(ReminderFilter{Status: ReminderDone}); len(done) != 1 || !done[0].Done() {
		t.Fatalf("done list: %+v", done)
	}
	if got, _ := st.ListReminders(ReminderFilter{ContactID: alex.ID}); len(got) != 1 || got[0].ID != late.ID {
		t.Fatalf("contact filter: %+v", got)
	}
	if got, _ := st.ListReminders(ReminderFilter{From: "2026-09-28", To: "2026-09-28"}); len(got) != 2 {
		t.Fatalf("date range should exclude undated: %+v", got)
	}
	if got, _ := st.ListReminders(ReminderFilter{Query: "rivera"}); len(got) != 1 {
		t.Fatalf("query should search the contact name too: %+v", got)
	}
	if _, err := st.SetReminderDone(early.ID, false); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetReminder(early.ID); r.Done() {
		t.Fatal("reopen failed")
	}
	if err := st.DeleteReminder(12345); err != ErrNotFound {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestNotesAndInteractionsSearch(t *testing.T) {
	st := newTestStore(t)
	st.CreateNote(Note{Title: "Atlas", Body: "Launch slips to Q1"})
	st.CreateNote(Note{Body: "Buy milk"})
	st.CreateInteraction(Interaction{Date: "2026-09-01", Summary: "Atlas sync", Kind: "meeting"})
	st.CreateInteraction(Interaction{Date: "2026-09-02", Summary: "Dentist", Kind: "weird-kind"})

	if got, _ := st.ListNotes(NoteFilter{Query: "atlas"}); len(got) != 1 {
		t.Fatalf("notes search: %+v", got)
	}
	all, _ := st.ListInteractions(InteractionFilter{})
	if len(all) != 2 || all[0].Summary != "Dentist" || all[0].Kind != "other" {
		t.Fatalf("interactions newest first, unknown kind -> other: %+v", all)
	}
	if has, _ := st.HasData(); !has {
		t.Fatal("HasData should be true")
	}
}
