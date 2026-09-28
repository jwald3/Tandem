package store

import (
	"database/sql"
	"strings"
)

// InteractionKinds is the fixed vocabulary for Interaction.Kind.
var InteractionKinds = []string{"meeting", "call", "email", "message", "other"}

// NormalizeKind maps a free-form kind onto InteractionKinds ("other" if unknown).
func NormalizeKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	for _, k := range InteractionKinds {
		if kind == k {
			return k
		}
	}
	switch kind {
	case "text", "sms", "dm", "chat", "slack":
		return "message"
	case "phone", "phone call", "video call", "zoom":
		return "call"
	case "coffee", "lunch", "dinner", "in person", "in-person", "1:1", "one-on-one":
		return "meeting"
	}
	return "other"
}

// Interaction is something that happened between the user and (usually) a
// contact: a meeting, call, email...
type Interaction struct {
	ID          int64  `json:"id"`
	ContactID   int64  `json:"contact_id"`
	ContactName string `json:"contact_name"`
	Kind        string `json:"kind"`
	Date        string `json:"date"`
	Summary     string `json:"summary"`
	CreatedAt   string `json:"created_at"`
}

// InteractionFilter narrows ListInteractions. Zero values mean "no filter".
type InteractionFilter struct {
	ContactID int64
	Query     string // text search over summary and contact name
	Limit     int    // default 200
}

const interactionSelect = `
SELECT i.id, i.contact_id, c.name, i.kind, i.date, i.summary, i.created_at
FROM interactions i LEFT JOIN contacts c ON c.id = i.contact_id`

func scanInteraction(row interface{ Scan(...any) error }) (Interaction, error) {
	var in Interaction
	var cid sql.NullInt64
	var cname sql.NullString
	err := row.Scan(&in.ID, &cid, &cname, &in.Kind, &in.Date, &in.Summary, &in.CreatedAt)
	in.ContactID, in.ContactName = contactName(cid, cname)
	return in, err
}

// CreateInteraction records an interaction.
func (s *Store) CreateInteraction(in Interaction) (Interaction, error) {
	res, err := s.db.Exec(`INSERT INTO interactions (contact_id, kind, date, summary) VALUES (?, ?, ?, ?)`,
		nullID(in.ContactID), NormalizeKind(in.Kind), orToday(in.Date), strings.TrimSpace(in.Summary))
	if err != nil {
		return Interaction{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetInteraction(id)
}

// UpdateInteraction replaces an interaction's fields.
func (s *Store) UpdateInteraction(in Interaction) (Interaction, error) {
	err := checkAffected(s.db.Exec(`UPDATE interactions SET contact_id = ?, kind = ?, date = ?, summary = ? WHERE id = ?`,
		nullID(in.ContactID), NormalizeKind(in.Kind), orToday(in.Date), strings.TrimSpace(in.Summary), in.ID))
	if err != nil {
		return Interaction{}, err
	}
	return s.GetInteraction(in.ID)
}

// GetInteraction returns one interaction by id, or ErrNotFound.
func (s *Store) GetInteraction(id int64) (Interaction, error) {
	in, err := scanInteraction(s.db.QueryRow(interactionSelect+` WHERE i.id = ?`, id))
	return in, notFound(err)
}

// DeleteInteraction removes an interaction.
func (s *Store) DeleteInteraction(id int64) error {
	return checkAffected(s.db.Exec(`DELETE FROM interactions WHERE id = ?`, id))
}

// ListInteractions returns interactions matching f, most recent first.
func (s *Store) ListInteractions(f InteractionFilter) ([]Interaction, error) {
	var where []string
	var args []any
	if f.ContactID != 0 {
		where = append(where, `i.contact_id = ?`)
		args = append(args, f.ContactID)
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, `(i.summary LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')`)
		like := likeArg(f.Query)
		args = append(args, like, like)
	}
	q := interactionSelect
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	q += ` ORDER BY i.date DESC, i.id DESC LIMIT ?`
	args = append(args, f.Limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Interaction
	for rows.Next() {
		in, err := scanInteraction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
