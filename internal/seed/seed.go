// Package seed fills an empty database with a small, realistic set of fake
// contacts, reminders, interactions and notes, dated relative to today, for
// trying the app out and for screenshots.
package seed

import (
	"errors"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// ErrNotEmpty is returned when the database already holds data.
var ErrNotEmpty = errors.New("database is not empty; seed a fresh file with -db demo.db")

// Demo writes the demo data. It refuses to touch a database that has any.
func Demo(st *store.Store) error {
	has, err := st.HasData()
	if err != nil {
		return err
	}
	if has {
		return ErrNotEmpty
	}

	contacts := map[string]store.Contact{}
	for _, c := range []store.Contact{
		{Name: "Alex Rivera", Relationship: "coworker", Company: "Northwind", Title: "Product Manager", Email: "alex.rivera@example.com",
			About: "Leads the Atlas launch. Prefers morning meetings."},
		{Name: "Priya Shah", Relationship: "manager", Company: "Northwind", Title: "Director of Engineering", Email: "priya@example.com"},
		{Name: "Dana Whitfield", Relationship: "friend", Phone: "555-0142", Birthday: "--11-03", About: "College roommate. Lives in Denver now."},
		{Name: "Marcus Lee", Relationship: "client", Company: "Fabrikam", Title: "CTO"},
		{Name: "Mom", Relationship: "family", Phone: "555-0100"},
	} {
		saved, err := st.CreateContact(c)
		if err != nil {
			return err
		}
		contacts[c.Name] = saved
	}
	id := func(name string) int64 { return contacts[name].ID }
	day := dates.DaysFromToday

	for _, r := range []store.Reminder{
		{Title: "Meet with Alex about Atlas", DueDate: day(1), DueTime: "10:00", ContactID: id("Alex Rivera"), Notes: "Bring the updated rollout timeline."},
		{Title: "Send Marcus the revised proposal", DueDate: day(-1), ContactID: id("Marcus Lee")},
		{Title: "1:1 with Priya", DueDate: day(3), DueTime: "14:30", ContactID: id("Priya Shah")},
		{Title: "Call Mom", DueDate: day(0), ContactID: id("Mom")},
		{Title: "Book flights for Dana's visit", DueDate: day(12), ContactID: id("Dana Whitfield")},
		{Title: "Renew passport"},
		{Title: "Pick up dry cleaning", DueDate: day(2)},
	} {
		if _, err := st.CreateReminder(r); err != nil {
			return err
		}
	}
	done, err := st.CreateReminder(store.Reminder{Title: "Draft Atlas launch checklist", DueDate: day(-4), ContactID: id("Alex Rivera")})
	if err != nil {
		return err
	}
	if _, err := st.SetReminderDone(done.ID, true); err != nil {
		return err
	}

	for _, in := range []store.Interaction{
		{ContactID: id("Alex Rivera"), Kind: "meeting", Date: day(-6), Summary: "Atlas kickoff. Alex wants a beta in three weeks; design review is the long pole."},
		{ContactID: id("Alex Rivera"), Kind: "message", Date: day(-2), Summary: "Alex says legal signed off on the Atlas terms."},
		{ContactID: id("Priya Shah"), Kind: "meeting", Date: day(-10), Summary: "Quarterly check-in. Asked about taking on the platform migration next quarter."},
		{ContactID: id("Marcus Lee"), Kind: "call", Date: day(-5), Summary: "Marcus is happy with phase one; wants pricing for phase two by end of week."},
		{ContactID: id("Dana Whitfield"), Kind: "call", Date: day(-21), Summary: "Caught up for an hour. She's thinking of visiting next month."},
	} {
		if _, err := st.CreateInteraction(in); err != nil {
			return err
		}
	}

	for _, n := range []store.Note{
		{ContactID: id("Alex Rivera"), Title: "Atlas", Body: "Launch target is mid-November. Alex owns the rollout; I own the migration tooling."},
		{ContactID: id("Dana Whitfield"), Body: "Allergic to shellfish. Loves the ramen place on 5th."},
		{Title: "Gift ideas", Body: "Mom: gardening gloves, the new mystery novel she mentioned."},
	} {
		if _, err := st.CreateNote(n); err != nil {
			return err
		}
	}
	return nil
}
