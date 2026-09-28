package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// Tools for reminders.

var listRemindersTool = tool{
	name:        "list_reminders",
	description: "List reminders. Defaults to open ones, soonest first (undated last). Filter by contact, due-date range, or text.",
	props: map[string]any{
		"status":  enumProp([]string{"open", "done", "all"}, "Which reminders (default open)"),
		"contact": contactProp("Only reminders linked to this contact (id or name)"),
		"from":    prop("string", "Earliest due date, YYYY-MM-DD (inclusive). Setting from/to excludes undated reminders."),
		"to":      prop("string", "Latest due date, YYYY-MM-DD (inclusive)"),
		"query":   prop("string", "Text to search for in the title, notes or contact name"),
		"limit":   prop("integer", "Max results (default 50)"),
	},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Status  string     `json:"status"`
			Contact contactRef `json:"contact"`
			From    string     `json:"from"`
			To      string     `json:"to"`
			Query   string     `json:"query"`
			Limit   int        `json:"limit"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		f := store.ReminderFilter{Status: in.Status, Query: in.Query, Limit: in.Limit}
		if f.Limit <= 0 {
			f.Limit = 50
		}
		var err error
		if f.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		if f.From, err = checkDate("from", in.From); err != nil {
			return "", false, err
		}
		if f.To, err = checkDate("to", in.To); err != nil {
			return "", false, err
		}
		rs, err := a.store.ListReminders(f)
		if err != nil {
			return "", false, err
		}
		out := listOut("Reminders", "No matching reminders.", rs, reminderLine)
		if len(rs) == f.Limit {
			out += "(limit reached; there may be more)\n"
		}
		return out, false, nil
	},
}

var createReminderTool = tool{
	name:        "create_reminder",
	description: "Create a reminder: something to do or attend, optionally on a date and time, optionally about a contact.",
	props: map[string]any{
		"title":    prop("string", "Short description, e.g. \"Meet with Alex about Atlas\""),
		"due_date": prop("string", "YYYY-MM-DD. Omit for an open-ended to-do."),
		"due_time": prop("string", "HH:MM, 24-hour. Only if the user gave a time."),
		"notes":    prop("string", "Extra detail worth keeping with the reminder"),
		"contact":  contactProp(""),
	},
	required: []string{"title"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Title   string     `json:"title"`
			DueDate string     `json:"due_date"`
			DueTime string     `json:"due_time"`
			Notes   string     `json:"notes"`
			Contact contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(in.Title) == "" {
			return "", false, errors.New("title is required")
		}
		r := store.Reminder{Title: in.Title, Notes: in.Notes}
		var err error
		if r.DueDate, r.DueTime, err = checkDue(in.DueDate, in.DueTime); err != nil {
			return "", false, err
		}
		if r.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		r, err = a.store.CreateReminder(r)
		if err != nil {
			return "", false, err
		}
		return "Created reminder " + reminderLine(r), true, nil
	},
}

var updateReminderTool = tool{
	name:        "update_reminder",
	description: "Change a reminder. Only the fields you pass change. Pass an empty string for due_date, due_time or notes to clear it, and \"none\" for contact to unlink it.",
	props: map[string]any{
		"id":       idProp("reminder"),
		"title":    prop("string", "New title"),
		"due_date": prop("string", "YYYY-MM-DD, or \"\" to remove the date"),
		"due_time": prop("string", "HH:MM 24-hour, or \"\" to make it all-day"),
		"notes":    prop("string", "Replacement notes"),
		"contact":  contactProp("Contact id or name, or \"none\" to unlink"),
	},
	required: []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID      int64       `json:"id"`
			Title   *string     `json:"title"`
			DueDate *string     `json:"due_date"`
			DueTime *string     `json:"due_time"`
			Notes   *string     `json:"notes"`
			Contact *contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		r, err := a.getReminder(in.ID)
		if err != nil {
			return "", false, err
		}
		if in.Title != nil {
			if strings.TrimSpace(*in.Title) == "" {
				return "", false, errors.New("title can't be empty")
			}
			r.Title = *in.Title
		}
		if in.Notes != nil {
			r.Notes = *in.Notes
		}
		date, tm := r.DueDate, r.DueTime
		if in.DueDate != nil {
			date = *in.DueDate
			if date == "" {
				tm = "" // no date means no time either
			}
		}
		if in.DueTime != nil {
			tm = *in.DueTime
		}
		if r.DueDate, r.DueTime, err = checkDue(date, tm); err != nil {
			return "", false, err
		}
		if in.Contact != nil {
			if r.ContactID, err = a.optionalContact(*in.Contact); err != nil {
				return "", false, err
			}
		}
		r, err = a.store.UpdateReminder(r)
		if err != nil {
			return "", false, err
		}
		return "Updated reminder " + reminderLine(r), true, nil
	},
}

var completeReminderTool = tool{
	name:        "complete_reminder",
	description: "Mark a reminder done, or reopen it with done=false.",
	props: map[string]any{
		"id":   idProp("reminder"),
		"done": prop("boolean", "true to complete (default), false to reopen"),
	},
	required: []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID   int64 `json:"id"`
			Done *bool `json:"done"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		done := in.Done == nil || *in.Done
		if _, err := a.getReminder(in.ID); err != nil {
			return "", false, err
		}
		r, err := a.store.SetReminderDone(in.ID, done)
		if err != nil {
			return "", false, err
		}
		if done {
			return "Completed reminder " + reminderLine(r), true, nil
		}
		return "Reopened reminder " + reminderLine(r), true, nil
	},
}

var deleteReminderTool = tool{
	name:        "delete_reminder",
	description: "Delete a reminder permanently. To finish one, use complete_reminder instead.",
	props:       map[string]any{"id": idProp("reminder")},
	required:    []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID int64 `json:"id"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		r, err := a.getReminder(in.ID)
		if err != nil {
			return "", false, err
		}
		if err := a.store.DeleteReminder(r.ID); err != nil {
			return "", false, err
		}
		return fmt.Sprintf("Deleted reminder #%d %s.", r.ID, r.Title), true, nil
	},
}

func (a *Agent) getReminder(id int64) (store.Reminder, error) {
	r, err := a.store.GetReminder(id)
	if errors.Is(err, store.ErrNotFound) {
		return r, fmt.Errorf("no reminder with id %d", id)
	}
	return r, err
}

// checkDue validates a due date and time together: a time needs a date.
func checkDue(date, tm string) (string, string, error) {
	date, err := checkDate("due_date", date)
	if err != nil {
		return "", "", err
	}
	tm, err = dates.NormalizeTime(tm)
	if err != nil {
		return "", "", err
	}
	if tm != "" && date == "" {
		return "", "", errors.New("due_time needs a due_date")
	}
	return date, tm, nil
}
