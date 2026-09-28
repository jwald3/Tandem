package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
)

// tool pairs a tool's schema (what Claude sees) with its implementation.
// Tools are defined next to the data they touch, in tools_*.go.
type tool struct {
	name        string
	description string
	props       map[string]any
	required    []string
	// run executes a call and returns a text result for Claude, plus whether
	// it changed the user's data.
	run func(a *Agent, input json.RawMessage) (result string, mutated bool, err error)
}

// allTools is every tool, in the order the API sees them.
var allTools = []tool{
	searchContactsTool, getContactTool, createContactTool, updateContactTool, deleteContactTool,
	listRemindersTool, createReminderTool, updateReminderTool, completeReminderTool, deleteReminderTool,
	listInteractionsTool, logInteractionTool, updateInteractionTool, deleteInteractionTool,
	listNotesTool, addNoteTool, updateNoteTool, deleteNoteTool,
	searchTool,
}

var toolsByName = func() map[string]tool {
	m := make(map[string]tool, len(allTools))
	for _, t := range allTools {
		m[t.name] = t
	}
	return m
}()

// toolParams returns the tool definitions sent with every request. The order
// is fixed, which keeps the cached prefix stable.
func toolParams() []anthropic.BetaToolUnionParam {
	out := make([]anthropic.BetaToolUnionParam, len(allTools))
	for i, t := range allTools {
		out[i] = anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        t.name,
			Description: anthropic.String(t.description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: t.props, Required: t.required},
		}}
	}
	return out
}

// --- Schema helpers ---

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

func enumProp(values []string, description string) map[string]any {
	p := prop("string", description)
	p["enum"] = values
	return p
}

func idProp(what string) map[string]any { return prop("integer", "The "+what+"'s id") }

func contactProp(description string) map[string]any {
	if description == "" {
		description = "The contact this is about: their id (preferred) or name."
	}
	return map[string]any{"type": []string{"string", "integer"}, "description": description}
}

// --- Input helpers ---

// contactRef is a contact given as an id or a name. Claude may send either a
// JSON number or a string.
type contactRef string

func (c *contactRef) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*c = contactRef(n.String())
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("contact must be an id or a name")
	}
	*c = contactRef(s)
	return nil
}

// isNone reports whether a ref means "no contact" ("", "none", "null").
func (c contactRef) isNone() bool {
	switch strings.ToLower(strings.TrimSpace(string(c))) {
	case "", "none", "null", "0":
		return true
	}
	return false
}

// decode unmarshals a tool's input.
func decode(input json.RawMessage, v any) error {
	if err := json.Unmarshal(input, v); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return nil
}

// resolveContact finds exactly one contact for an id or name, or explains why
// it couldn't, in a way that tells Claude what to do next.
func (a *Agent) resolveContact(ref contactRef) (store.Contact, error) {
	s := strings.TrimSpace(string(ref))
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		c, err := a.store.GetContact(id)
		if errors.Is(err, store.ErrNotFound) {
			return c, fmt.Errorf("no contact with id %d", id)
		}
		return c, err
	}
	matches, err := a.store.FindContacts(s)
	if err != nil {
		return store.Contact{}, err
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return store.Contact{}, fmt.Errorf("no contact matches %q. Check search_contacts, or add them with create_contact first", s)
	}
	var names []string
	for i, m := range matches {
		if i == 8 {
			names = append(names, fmt.Sprintf("and %d more", len(matches)-i))
			break
		}
		names = append(names, contactLabel(m))
	}
	return store.Contact{}, fmt.Errorf("%q matches several contacts: %s. Ask the user which one, then pass the id", s, strings.Join(names, "; "))
}

// optionalContact resolves a ref that may be omitted; 0 means no contact.
func (a *Agent) optionalContact(ref contactRef) (int64, error) {
	if ref.isNone() {
		return 0, nil
	}
	c, err := a.resolveContact(ref)
	return c.ID, err
}

// checkDate validates an optional YYYY-MM-DD date.
func checkDate(field, d string) (string, error) {
	d = strings.TrimSpace(d)
	if d != "" && !dates.Valid(d) {
		return "", fmt.Errorf("%s %q is not a valid YYYY-MM-DD date", field, d)
	}
	return d, nil
}

// --- Output formatting ---

func contactLabel(c store.Contact) string {
	s := fmt.Sprintf("%s (id %d", c.Name, c.ID)
	if sub := c.Subtitle(); sub != "" {
		s += "; " + sub
	}
	return s + ")"
}

func withContact(name string, id int64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf(" · with %s (contact %d)", name, id)
}

// reminderLine renders a reminder on one line, e.g.
// "#4 Meet about Atlas — Monday, 2026-09-28 at 2:00 PM · with Alex Rivera (contact 3)".
func reminderLine(r store.Reminder) string {
	s := fmt.Sprintf("#%d %s", r.ID, r.Title)
	switch {
	case r.DueDate == "":
		s += " — no date"
	default:
		s += " — " + dates.Long(r.DueDate)
		if r.DueTime != "" {
			s += " at " + dates.Clock(r.DueTime)
		}
		if r.Overdue() {
			s += " (OVERDUE)"
		}
	}
	s += withContact(r.ContactName, r.ContactID)
	if r.Done() {
		s += " · done " + r.DoneAt[:min(10, len(r.DoneAt))]
	}
	if r.Notes != "" {
		s += " · notes: " + oneLine(r.Notes)
	}
	return s
}

func interactionLine(in store.Interaction) string {
	return fmt.Sprintf("#%d %s %s%s: %s", in.ID, dates.Long(in.Date), in.Kind, withContact(in.ContactName, in.ContactID), oneLine(in.Summary))
}

func noteLine(n store.Note) string {
	s := fmt.Sprintf("#%d (%s)", n.ID, n.UpdatedAt[:min(10, len(n.UpdatedAt))])
	if n.Title != "" {
		s += " " + n.Title + ":"
	}
	s += " " + oneLine(n.Body) + withContact(n.ContactName, n.ContactID)
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// listOut renders a heading plus one line per item, or empty when there are none.
func listOut[T any](heading, empty string, items []T, line func(T) string) string {
	if len(items) == 0 {
		return empty
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%d):\n", heading, len(items))
	for _, it := range items {
		sb.WriteString(line(it) + "\n")
	}
	return sb.String()
}
