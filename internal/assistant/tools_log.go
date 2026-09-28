package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jwald3/tandem/internal/store"
)

// Tools for interactions and notes: the record of what happened and what the
// user wants to remember.

var listInteractionsTool = tool{
	name:        "list_interactions",
	description: "List logged interactions (meetings, calls, emails, messages), most recent first. Filter by contact or text.",
	props: map[string]any{
		"contact": contactProp("Only interactions with this contact (id or name)"),
		"query":   prop("string", "Text to search for in the summary or contact name"),
		"limit":   prop("integer", "Max results (default 30)"),
	},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Contact contactRef `json:"contact"`
			Query   string     `json:"query"`
			Limit   int        `json:"limit"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		f := store.InteractionFilter{Query: in.Query, Limit: in.Limit}
		if f.Limit <= 0 {
			f.Limit = 30
		}
		var err error
		if f.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		list, err := a.store.ListInteractions(f)
		if err != nil {
			return "", false, err
		}
		return listOut("Interactions", "No matching interactions.", list, interactionLine), false, nil
	},
}

var logInteractionTool = tool{
	name:        "log_interaction",
	description: "Record something that already happened: a meeting, call, email or message, usually with a contact.",
	props: map[string]any{
		"summary": prop("string", "What happened and anything notable that was said or decided"),
		"kind":    enumProp(store.InteractionKinds, "The kind of interaction (default other)"),
		"date":    prop("string", "YYYY-MM-DD it happened; omit for today"),
		"contact": contactProp("Who it was with: id (preferred) or name"),
	},
	required: []string{"summary"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Summary string     `json:"summary"`
			Kind    string     `json:"kind"`
			Date    string     `json:"date"`
			Contact contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(in.Summary) == "" {
			return "", false, errors.New("summary is required")
		}
		it := store.Interaction{Summary: in.Summary, Kind: in.Kind}
		var err error
		if it.Date, err = checkDate("date", in.Date); err != nil {
			return "", false, err
		}
		if it.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		it, err = a.store.CreateInteraction(it)
		if err != nil {
			return "", false, err
		}
		return "Logged interaction " + interactionLine(it), true, nil
	},
}

var updateInteractionTool = tool{
	name:        "update_interaction",
	description: "Change a logged interaction. Only the fields you pass change; \"none\" for contact unlinks it.",
	props: map[string]any{
		"id":      idProp("interaction"),
		"summary": prop("string", "Replacement summary"),
		"kind":    enumProp(store.InteractionKinds, "New kind"),
		"date":    prop("string", "New date, YYYY-MM-DD"),
		"contact": contactProp("Contact id or name, or \"none\" to unlink"),
	},
	required: []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID      int64       `json:"id"`
			Summary *string     `json:"summary"`
			Kind    *string     `json:"kind"`
			Date    *string     `json:"date"`
			Contact *contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		it, err := a.store.GetInteraction(in.ID)
		if errors.Is(err, store.ErrNotFound) {
			return "", false, fmt.Errorf("no interaction with id %d", in.ID)
		} else if err != nil {
			return "", false, err
		}
		if in.Summary != nil {
			if strings.TrimSpace(*in.Summary) == "" {
				return "", false, errors.New("summary can't be empty")
			}
			it.Summary = *in.Summary
		}
		if in.Kind != nil {
			it.Kind = *in.Kind
		}
		if in.Date != nil {
			if it.Date, err = checkDate("date", *in.Date); err != nil {
				return "", false, err
			}
		}
		if in.Contact != nil {
			if it.ContactID, err = a.optionalContact(*in.Contact); err != nil {
				return "", false, err
			}
		}
		it, err = a.store.UpdateInteraction(it)
		if err != nil {
			return "", false, err
		}
		return "Updated interaction " + interactionLine(it), true, nil
	},
}

var deleteInteractionTool = tool{
	name:        "delete_interaction",
	description: "Delete a logged interaction permanently.",
	props:       map[string]any{"id": idProp("interaction")},
	required:    []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID int64 `json:"id"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if err := a.store.DeleteInteraction(in.ID); errors.Is(err, store.ErrNotFound) {
			return "", false, fmt.Errorf("no interaction with id %d", in.ID)
		} else if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("Deleted interaction #%d.", in.ID), true, nil
	},
}

var listNotesTool = tool{
	name:        "list_notes",
	description: "List notes, most recently updated first. Filter by contact or text.",
	props: map[string]any{
		"contact": contactProp("Only notes about this contact (id or name)"),
		"query":   prop("string", "Text to search for in the title, body or contact name"),
		"limit":   prop("integer", "Max results (default 30)"),
	},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Contact contactRef `json:"contact"`
			Query   string     `json:"query"`
			Limit   int        `json:"limit"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		f := store.NoteFilter{Query: in.Query, Limit: in.Limit}
		if f.Limit <= 0 {
			f.Limit = 30
		}
		var err error
		if f.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		notes, err := a.store.ListNotes(f)
		if err != nil {
			return "", false, err
		}
		return listOut("Notes", "No matching notes.", notes, noteLine), false, nil
	},
}

var addNoteTool = tool{
	name:        "add_note",
	description: "Save a note: information worth remembering, optionally about a contact.",
	props: map[string]any{
		"body":    prop("string", "The note text"),
		"title":   prop("string", "Optional short title or topic, e.g. a project name"),
		"contact": contactProp(""),
	},
	required: []string{"body"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Body    string     `json:"body"`
			Title   string     `json:"title"`
			Contact contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(in.Body) == "" {
			return "", false, errors.New("body is required")
		}
		n := store.Note{Body: in.Body, Title: in.Title}
		var err error
		if n.ContactID, err = a.optionalContact(in.Contact); err != nil {
			return "", false, err
		}
		n, err = a.store.CreateNote(n)
		if err != nil {
			return "", false, err
		}
		return "Saved note " + noteLine(n), true, nil
	},
}

var updateNoteTool = tool{
	name:        "update_note",
	description: "Change a note. Only the fields you pass change; \"none\" for contact unlinks it. To add to a note, pass the full new body.",
	props: map[string]any{
		"id":      idProp("note"),
		"title":   prop("string", "New title (\"\" to clear)"),
		"body":    prop("string", "Replacement body"),
		"contact": contactProp("Contact id or name, or \"none\" to unlink"),
	},
	required: []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID      int64       `json:"id"`
			Title   *string     `json:"title"`
			Body    *string     `json:"body"`
			Contact *contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		n, err := a.store.GetNote(in.ID)
		if errors.Is(err, store.ErrNotFound) {
			return "", false, fmt.Errorf("no note with id %d", in.ID)
		} else if err != nil {
			return "", false, err
		}
		if in.Title != nil {
			n.Title = *in.Title
		}
		if in.Body != nil {
			if strings.TrimSpace(*in.Body) == "" {
				return "", false, errors.New("body can't be empty")
			}
			n.Body = *in.Body
		}
		if in.Contact != nil {
			if n.ContactID, err = a.optionalContact(*in.Contact); err != nil {
				return "", false, err
			}
		}
		n, err = a.store.UpdateNote(n)
		if err != nil {
			return "", false, err
		}
		return "Updated note " + noteLine(n), true, nil
	},
}

var deleteNoteTool = tool{
	name:        "delete_note",
	description: "Delete a note permanently.",
	props:       map[string]any{"id": idProp("note")},
	required:    []string{"id"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			ID int64 `json:"id"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if err := a.store.DeleteNote(in.ID); errors.Is(err, store.ErrNotFound) {
			return "", false, fmt.Errorf("no note with id %d", in.ID)
		} else if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("Deleted note #%d.", in.ID), true, nil
	},
}

var searchTool = tool{
	name:        "search",
	description: "Search everything at once (contacts, reminders including completed ones, interactions and notes) for a word or phrase, like a project name or topic.",
	props:       map[string]any{"query": prop("string", "Word or phrase to look for")},
	required:    []string{"query"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Query string `json:"query"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(in.Query) == "" {
			return "", false, errors.New("query is required")
		}
		const each = 15
		contacts, _ := a.store.ListContacts(in.Query)
		reminders, _ := a.store.ListReminders(store.ReminderFilter{Status: store.ReminderAll, Query: in.Query, Limit: each})
		ints, _ := a.store.ListInteractions(store.InteractionFilter{Query: in.Query, Limit: each})
		notes, _ := a.store.ListNotes(store.NoteFilter{Query: in.Query, Limit: each})
		if len(contacts)+len(reminders)+len(ints)+len(notes) == 0 {
			return fmt.Sprintf("Nothing matches %q.", in.Query), false, nil
		}
		if len(contacts) > each {
			contacts = contacts[:each]
		}
		var sb strings.Builder
		sb.WriteString(listOut("Contacts", "", contacts, contactLabel))
		sb.WriteString(listOut("Reminders", "", reminders, reminderLine))
		sb.WriteString(listOut("Interactions", "", ints, interactionLine))
		sb.WriteString(listOut("Notes", "", notes, noteLine))
		return sb.String(), false, nil
	},
}
