package store

import (
	"database/sql"
	"strings"
)

// Contact is a person the user wants to keep track of.
type Contact struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Relationship string `json:"relationship"`
	Company      string `json:"company"`
	Title        string `json:"title"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Birthday     string `json:"birthday"`
	About        string `json:"about"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`

	// Summary fields, filled by ListContacts only.
	OpenReminders   int    `json:"open_reminders,omitempty"`
	NextDue         string `json:"next_due,omitempty"`         // earliest open reminder date
	LastInteraction string `json:"last_interaction,omitempty"` // most recent interaction date
}

// Subtitle is "Title, Company · relationship" with the blanks left out.
func (c Contact) Subtitle() string {
	var parts []string
	job := c.Title
	if c.Company != "" {
		if job != "" {
			job += ", "
		}
		job += c.Company
	}
	if job != "" {
		parts = append(parts, job)
	}
	if c.Relationship != "" {
		parts = append(parts, c.Relationship)
	}
	return strings.Join(parts, " · ")
}

// clean trims every text field.
func (c *Contact) clean() {
	for _, f := range []*string{&c.Name, &c.Relationship, &c.Company, &c.Title, &c.Email, &c.Phone, &c.Birthday, &c.About} {
		*f = strings.TrimSpace(*f)
	}
}

const contactCols = `id, name, relationship, company, title, email, phone, birthday, about, created_at, updated_at`

func scanContact(row interface{ Scan(...any) error }, extra ...any) (Contact, error) {
	var c Contact
	dest := append([]any{&c.ID, &c.Name, &c.Relationship, &c.Company, &c.Title, &c.Email, &c.Phone, &c.Birthday, &c.About, &c.CreatedAt, &c.UpdatedAt}, extra...)
	err := row.Scan(dest...)
	return c, err
}

// CreateContact inserts a contact and returns it with its id.
func (s *Store) CreateContact(c Contact) (Contact, error) {
	c.clean()
	res, err := s.db.Exec(`
INSERT INTO contacts (name, relationship, company, title, email, phone, birthday, about)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Relationship, c.Company, c.Title, c.Email, c.Phone, c.Birthday, c.About)
	if err != nil {
		return Contact{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetContact(id)
}

// UpdateContact replaces every editable field of contact c.ID.
func (s *Store) UpdateContact(c Contact) (Contact, error) {
	c.clean()
	err := checkAffected(s.db.Exec(`
UPDATE contacts SET name = ?, relationship = ?, company = ?, title = ?, email = ?,
       phone = ?, birthday = ?, about = ?, updated_at = datetime('now', 'localtime')
WHERE id = ?`,
		c.Name, c.Relationship, c.Company, c.Title, c.Email, c.Phone, c.Birthday, c.About, c.ID))
	if err != nil {
		return Contact{}, err
	}
	return s.GetContact(c.ID)
}

// GetContact returns one contact by id, or ErrNotFound.
func (s *Store) GetContact(id int64) (Contact, error) {
	c, err := scanContact(s.db.QueryRow(`SELECT `+contactCols+` FROM contacts WHERE id = ?`, id))
	return c, notFound(err)
}

// DeleteContact removes a contact. Its reminders, interactions and notes are
// kept, unlinked.
func (s *Store) DeleteContact(id int64) error {
	return checkAffected(s.db.Exec(`DELETE FROM contacts WHERE id = ?`, id))
}

// ListContacts returns contacts alphabetically, optionally filtered by a
// search over name, company, title, relationship, email and about. Each comes
// with its open-reminder count, next due date and last interaction date.
func (s *Store) ListContacts(query string) ([]Contact, error) {
	q := `
SELECT ` + contactCols + `,
  (SELECT COUNT(1) FROM reminders r WHERE r.contact_id = contacts.id AND r.done_at IS NULL),
  COALESCE((SELECT MIN(due_date) FROM reminders r WHERE r.contact_id = contacts.id AND r.done_at IS NULL AND r.due_date <> ''), ''),
  COALESCE((SELECT MAX(date) FROM interactions i WHERE i.contact_id = contacts.id), '')
FROM contacts`
	var args []any
	if strings.TrimSpace(query) != "" {
		q += ` WHERE name LIKE ?1 ESCAPE '\' OR company LIKE ?1 ESCAPE '\' OR title LIKE ?1 ESCAPE '\'
            OR relationship LIKE ?1 ESCAPE '\' OR email LIKE ?1 ESCAPE '\' OR about LIKE ?1 ESCAPE '\'`
		args = append(args, likeArg(query))
	}
	q += ` ORDER BY name COLLATE NOCASE, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var open int
		var next, last string
		c, err := scanContact(rows, &open, &next, &last)
		if err != nil {
			return nil, err
		}
		c.OpenReminders, c.NextDue, c.LastInteraction = open, next, last
		out = append(out, c)
	}
	return out, rows.Err()
}

// FindContacts resolves a name the way a person would say it. An exact
// (case-insensitive) full-name match wins outright; otherwise it returns every
// contact whose name contains the text, or whose name contains every word of
// it ("al rivera" finds "Alex Rivera").
func (s *Store) FindContacts(name string) ([]Contact, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return nil, nil
	}
	exact, err := s.queryContacts(`SELECT `+contactCols+` FROM contacts WHERE name = ? COLLATE NOCASE ORDER BY id`, name)
	if err != nil || len(exact) > 0 {
		return exact, err
	}
	words := strings.Fields(name)
	where := make([]string, len(words))
	args := make([]any, len(words))
	for i, w := range words {
		where[i] = `name LIKE ? ESCAPE '\'`
		args[i] = likeArg(w)
	}
	return s.queryContacts(`SELECT `+contactCols+` FROM contacts WHERE `+strings.Join(where, " AND ")+` ORDER BY name COLLATE NOCASE, id`, args...)
}

func (s *Store) queryContacts(query string, args ...any) ([]Contact, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// contactName returns a contact's name for a nullable id column.
func contactName(id sql.NullInt64, name sql.NullString) (int64, string) {
	if !id.Valid {
		return 0, ""
	}
	return id.Int64, name.String
}
