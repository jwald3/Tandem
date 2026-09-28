package store

import (
	"database/sql"
	"strings"

	"github.com/jwald3/tandem/internal/dates"
)

// Note is a free-form piece of information, optionally about a contact.
type Note struct {
	ID          int64  `json:"id"`
	ContactID   int64  `json:"contact_id"`
	ContactName string `json:"contact_name"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"` // local "YYYY-MM-DD HH:MM:SS"
	UpdatedAt   string `json:"updated_at"`
}

// NoteFilter narrows ListNotes. Zero values mean "no filter".
type NoteFilter struct {
	ContactID int64
	Query     string // text search over title, body and contact name
	Limit     int    // default 200
}

const noteSelect = `
SELECT n.id, n.contact_id, c.name, n.title, n.body, n.created_at, n.updated_at
FROM notes n LEFT JOIN contacts c ON c.id = n.contact_id`

func scanNote(row interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var cid sql.NullInt64
	var cname sql.NullString
	err := row.Scan(&n.ID, &cid, &cname, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt)
	n.ContactID, n.ContactName = contactName(cid, cname)
	return n, err
}

// CreateNote inserts a note.
func (s *Store) CreateNote(n Note) (Note, error) {
	res, err := s.db.Exec(`INSERT INTO notes (contact_id, title, body) VALUES (?, ?, ?)`,
		nullID(n.ContactID), strings.TrimSpace(n.Title), strings.TrimSpace(n.Body))
	if err != nil {
		return Note{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetNote(id)
}

// UpdateNote replaces a note's contact, title and body.
func (s *Store) UpdateNote(n Note) (Note, error) {
	err := checkAffected(s.db.Exec(`UPDATE notes SET contact_id = ?, title = ?, body = ?, updated_at = datetime('now', 'localtime') WHERE id = ?`,
		nullID(n.ContactID), strings.TrimSpace(n.Title), strings.TrimSpace(n.Body), n.ID))
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(n.ID)
}

// GetNote returns one note by id, or ErrNotFound.
func (s *Store) GetNote(id int64) (Note, error) {
	n, err := scanNote(s.db.QueryRow(noteSelect+` WHERE n.id = ?`, id))
	return n, notFound(err)
}

// DeleteNote removes a note.
func (s *Store) DeleteNote(id int64) error {
	return checkAffected(s.db.Exec(`DELETE FROM notes WHERE id = ?`, id))
}

// ListNotes returns notes matching f, most recently updated first.
func (s *Store) ListNotes(f NoteFilter) ([]Note, error) {
	var where []string
	var args []any
	if f.ContactID != 0 {
		where = append(where, `n.contact_id = ?`)
		args = append(args, f.ContactID)
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, `(n.title LIKE ? ESCAPE '\' OR n.body LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')`)
		like := likeArg(f.Query)
		args = append(args, like, like, like)
	}
	q := noteSelect
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	q += ` ORDER BY n.updated_at DESC, n.id DESC LIMIT ?`
	args = append(args, f.Limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// orToday returns date, or today's date when it's blank.
func orToday(date string) string {
	if date == "" {
		return dates.Today()
	}
	return date
}
