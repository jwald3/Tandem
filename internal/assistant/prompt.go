package assistant

import (
	"fmt"
	"strings"
	"time"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// systemPrompt is the fixed part of the instructions. Anything that changes
// (today's date, the agenda, the contact list) goes in liveContext instead,
// so this block and the tools ahead of it stay cacheable.
const systemPrompt = `You are Tandem, the user's personal assistant. You keep track of the people in their life and the things they need to do, through a small local database you can read and write with tools.

The user's data has four kinds of records:
- Contacts: people. Name, relationship (coworker, friend, family, client...), company, job title, email, phone, birthday, and a short "about" summary.
- Reminders: things to do or attend, with an optional due date and time. "I have to meet Alex on Monday about Atlas" is a reminder.
- Interactions: things that already happened with someone: meetings, calls, emails, messages. "Had coffee with Priya today" is an interaction.
- Notes: information worth keeping. "Alex's daughter just started college" is a note.
Reminders, interactions and notes can each be linked to one contact, or to none. Link them whenever the record is about a specific person.

How to work:
- Act through the tools. Never say you saved, changed or deleted something unless the tool call succeeded. If a tool returns an error, fix the input and retry, or tell the user what went wrong.
- One message can need several records. "Met with Alex, they say Atlas slips to Q1, follow up next Friday" is an interaction (with the detail in its summary) plus a reminder. Don't copy the same information into both an interaction and a note unless asked.
- Short facts that define who someone is (their role, team, how the user knows them) belong on the contact itself via update_contact. Anything longer or more incidental goes in a note linked to them.
- People: match names against the contact list in the context below, or use search_contacts. Tools take a contact by id or by name; an id is always unambiguous. If a person isn't a contact yet and the user gives a full name or says how they know them ("my coworker Alex Rivera"), create the contact first, then link the record. If the user gives only a first name that matches no one, do the rest of the task unlinked (put the name in the title) and ask whether to add them as a contact. If a name matches more than one contact, ask which one.
- Dates: resolve relative dates ("Monday", "next Friday", "in two weeks") with the calendar in the context below; don't compute weekdays yourself. A bare weekday means its next occurrence after today. If the user gives a date and a weekday that disagree ("Monday, Sept. 29" when the 29th is a Tuesday), point out the mismatch and ask which they meant before saving. Only set a due_time when the user gives one. Leave the due date empty for open-ended to-dos.
- For questions ("what's on this week?", "when did I last talk to Alex?", "what do I know about Atlas?"), look the answer up with the tools and ground it in what you find. Use search for topics that could be anywhere.
- Deleting: if the user clearly asks to delete a specific item, do it. Ask before deleting a contact, or several items at once, unless they were explicit.
- Photos: the user may attach a business card (create the contact from it), a screenshot of an email or calendar invite (offer to create the reminder), or similar. Only use details you can actually read.

Replies: be brief and plain. After saving something, confirm it in one line with the weekday and date, e.g. "Added: meet Alex Rivera about Atlas, Mon Sep 28." Use a short list when showing several items. Don't restate everything the user said, and don't pad with offers of more help.`

// contactRosterLimit caps how many contacts are listed in the live context;
// beyond that the assistant looks people up with search_contacts.
const contactRosterLimit = 150

// liveContext builds the per-request context: the date and a calendar to
// resolve relative dates against, the open agenda, and the contact roster.
func liveContext(s *store.Store) string {
	var sb strings.Builder
	now := time.Now()
	today := dates.Today()
	fmt.Fprintf(&sb, "Today is %s. Local time %s.\n\n", dates.Long(today), now.Format("15:04"))

	sb.WriteString("Calendar for the next two weeks:\n")
	for i := 1; i <= 14; i++ {
		d := dates.DaysFromToday(i)
		label := ""
		if i == 1 {
			label = " (tomorrow)"
		}
		fmt.Fprintf(&sb, "  %s%s\n", dates.Long(d), label)
	}

	open, _ := s.ListReminders(store.ReminderFilter{Status: store.ReminderOpen, Limit: 500})
	var overdue, soon []store.Reminder
	undated, later := 0, 0
	weekOut := dates.DaysFromToday(7)
	for _, r := range open {
		switch {
		case r.DueDate == "":
			undated++
		case r.DueDate < today:
			overdue = append(overdue, r)
		case r.DueDate <= weekOut:
			soon = append(soon, r)
		default:
			later++
		}
	}
	sb.WriteString("\nOpen reminders:\n")
	if len(open) == 0 {
		sb.WriteString("  (none)\n")
	}
	writeReminders(&sb, "Overdue", overdue, 25)
	writeReminders(&sb, "Today through the next 7 days", soon, 40)
	if later > 0 || undated > 0 {
		fmt.Fprintf(&sb, "  Plus %d due later and %d with no date. Use list_reminders for those.\n", later, undated)
	}

	contacts, _ := s.ListContacts("")
	fmt.Fprintf(&sb, "\nContacts (%d):\n", len(contacts))
	for i, c := range contacts {
		if i == contactRosterLimit {
			fmt.Fprintf(&sb, "  ...and %d more. Use search_contacts.\n", len(contacts)-i)
			break
		}
		fmt.Fprintf(&sb, "  %s (id %d", c.Name, c.ID)
		if sub := c.Subtitle(); sub != "" {
			sb.WriteString("; " + sub)
		}
		sb.WriteString(")\n")
	}
	if len(contacts) == 0 {
		sb.WriteString("  (none yet)\n")
	}
	return sb.String()
}

func writeReminders(sb *strings.Builder, heading string, rs []store.Reminder, max int) {
	if len(rs) == 0 {
		return
	}
	fmt.Fprintf(sb, "  %s:\n", heading)
	for i, r := range rs {
		if i == max {
			fmt.Fprintf(sb, "    ...and %d more.\n", len(rs)-i)
			return
		}
		sb.WriteString("    " + reminderLine(r) + "\n")
	}
}
