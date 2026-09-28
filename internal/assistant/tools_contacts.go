package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// Tools for contacts.

// contactFields are the editable fields shared by create_contact and
// update_contact.
func contactFields() map[string]any {
	return map[string]any{
		"relationship": prop("string", "How the user knows them, lowercase: coworker, friend, family, client, neighbor..."),
		"company":      prop("string", "Where they work"),
		"title":        prop("string", "Their job title or role"),
		"email":        prop("string", "Email address"),
		"phone":        prop("string", "Phone number"),
		"birthday":     prop("string", "YYYY-MM-DD, or --MM-DD if the year is unknown"),
		"about":        prop("string", "A short summary of who they are to the user (a sentence or two). Longer details go in notes."),
	}
}

var searchContactsTool = tool{
	name:        "search_contacts",
	description: "Find contacts by name, company, title, relationship, email or about text. An empty query lists everyone. Each result shows open reminders and the last interaction date.",
	props:       map[string]any{"query": prop("string", "Text to search for; omit to list all contacts")},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Query string `json:"query"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		cs, err := a.store.ListContacts(in.Query)
		if err != nil {
			return "", false, err
		}
		return listOut("Contacts", "No matching contacts.", cs, func(c store.Contact) string {
			s := contactLabel(c)
			if c.OpenReminders > 0 {
				s += fmt.Sprintf(" · %d open reminders", c.OpenReminders)
				if c.NextDue != "" {
					s += ", next " + dates.Long(c.NextDue)
				}
			}
			if c.LastInteraction != "" {
				s += " · last interaction " + dates.Long(c.LastInteraction)
			}
			return s
		}), false, nil
	},
}

var getContactTool = tool{
	name:        "get_contact",
	description: "Get everything about one contact: their details, open reminders, recently completed reminders, recent interactions and notes.",
	props:       map[string]any{"contact": contactProp("The contact's id (preferred) or name")},
	required:    []string{"contact"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Contact contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		c, err := a.resolveContact(in.Contact)
		if err != nil {
			return "", false, err
		}
		var sb strings.Builder
		sb.WriteString(contactLabel(c) + "\n")
		for _, f := range []struct{ label, val string }{
			{"Email", c.Email}, {"Phone", c.Phone}, {"Birthday", c.Birthday}, {"About", c.About},
		} {
			if f.val != "" {
				fmt.Fprintf(&sb, "%s: %s\n", f.label, f.val)
			}
		}
		open, _ := a.store.ListReminders(store.ReminderFilter{ContactID: c.ID})
		done, _ := a.store.ListReminders(store.ReminderFilter{ContactID: c.ID, Status: store.ReminderDone, Limit: 5})
		ints, _ := a.store.ListInteractions(store.InteractionFilter{ContactID: c.ID, Limit: 15})
		notes, _ := a.store.ListNotes(store.NoteFilter{ContactID: c.ID, Limit: 15})
		sb.WriteString("\n" + listOut("Open reminders", "No open reminders.", open, reminderLine))
		if len(done) > 0 {
			sb.WriteString("\n" + listOut("Recently completed", "", done, reminderLine))
		}
		sb.WriteString("\n" + listOut("Recent interactions", "No interactions logged.", ints, interactionLine))
		sb.WriteString("\n" + listOut("Notes", "No notes.", notes, noteLine))
		return sb.String(), false, nil
	},
}

var createContactTool = tool{
	name:        "create_contact",
	description: "Add a new contact. Check the contact list first so you don't create a duplicate.",
	props: func() map[string]any {
		p := contactFields()
		p["name"] = prop("string", "Full name as the user gave it")
		p["allow_duplicate"] = prop("boolean", "Set true only if the user confirmed this is a different person with the same name as an existing contact")
		return p
	}(),
	required: []string{"name"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			store.Contact
			AllowDuplicate bool `json:"allow_duplicate"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		c := in.Contact
		c.ID = 0
		if strings.TrimSpace(c.Name) == "" {
			return "", false, fmt.Errorf("name is required")
		}
		if !in.AllowDuplicate {
			if existing, _ := a.store.FindContacts(c.Name); len(existing) > 0 && strings.EqualFold(existing[0].Name, strings.TrimSpace(c.Name)) {
				return "", false, fmt.Errorf("a contact named %s already exists: %s. Use update_contact to change them, or set allow_duplicate if the user confirms it's a different person", existing[0].Name, contactLabel(existing[0]))
			}
		}
		c, err := a.store.CreateContact(c)
		if err != nil {
			return "", false, err
		}
		return "Created contact " + contactLabel(c) + ".", true, nil
	},
}

var updateContactTool = tool{
	name:        "update_contact",
	description: "Change a contact's details. Only the fields you pass change; pass an empty string to clear a field.",
	props: func() map[string]any {
		p := contactFields()
		p["contact"] = contactProp("The contact to change: id (preferred) or current name")
		p["name"] = prop("string", "New name")
		return p
	}(),
	required: []string{"contact"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Contact      contactRef `json:"contact"`
			Name         *string    `json:"name"`
			Relationship *string    `json:"relationship"`
			Company      *string    `json:"company"`
			Title        *string    `json:"title"`
			Email        *string    `json:"email"`
			Phone        *string    `json:"phone"`
			Birthday     *string    `json:"birthday"`
			About        *string    `json:"about"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		c, err := a.resolveContact(in.Contact)
		if err != nil {
			return "", false, err
		}
		for dst, src := range map[*string]*string{
			&c.Name: in.Name, &c.Relationship: in.Relationship, &c.Company: in.Company, &c.Title: in.Title,
			&c.Email: in.Email, &c.Phone: in.Phone, &c.Birthday: in.Birthday, &c.About: in.About,
		} {
			if src != nil {
				*dst = *src
			}
		}
		if strings.TrimSpace(c.Name) == "" {
			return "", false, fmt.Errorf("a contact's name can't be empty")
		}
		c, err = a.store.UpdateContact(c)
		if err != nil {
			return "", false, err
		}
		return "Updated contact " + contactLabel(c) + ".", true, nil
	},
}

var deleteContactTool = tool{
	name:        "delete_contact",
	description: "Delete a contact. Their reminders, interactions and notes are kept but unlinked. Confirm with the user first unless they explicitly asked.",
	props:       map[string]any{"contact": contactProp("The contact's id (preferred) or name")},
	required:    []string{"contact"},
	run: func(a *Agent, input json.RawMessage) (string, bool, error) {
		var in struct {
			Contact contactRef `json:"contact"`
		}
		if err := decode(input, &in); err != nil {
			return "", false, err
		}
		c, err := a.resolveContact(in.Contact)
		if err != nil {
			return "", false, err
		}
		if err := a.store.DeleteContact(c.ID); err != nil {
			return "", false, err
		}
		return fmt.Sprintf("Deleted contact %s. Their reminders, interactions and notes were kept, unlinked.", c.Name), true, nil
	},
}
