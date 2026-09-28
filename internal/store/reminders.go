package store

import (
	"database/sql"
	"strings"

	"github.com/jwald3/tandem/internal/dates"
)

// Reminder is something the user wants to do or be nudged about, optionally
// on a date (and time) and optionally about a contact.
type Reminder struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Notes       string `json:"notes"`
	DueDate     string `json:"due_date"` // YYYY-MM-DD or ""
	DueTime     string `json:"due_time"` // HH:MM or ""
	ContactID   int64  `json:"contact_id"`
	ContactName string `json:"contact_name"`
	DoneAt      string `json:"done_at"` // "" while open
	CreatedAt   string `json:"created_at"`
}

// Done reports whether the reminder has been completed.
func (r Reminder) Done() bool { return r.DoneAt != "" }

// Overdue reports whether an open reminder's date has passed.
func (r Reminder) Overdue() bool {
	return !r.Done() && r.DueDate != "" && r.DueDate < dates.Today()
}

// Reminder list statuses.
const (
	ReminderOpen = "open"
	ReminderDone = "done"
	ReminderAll  = "all"
)

// ReminderFilter narrows ListReminders. Zero values mean "no filter".
type ReminderFilter struct {
	Status    string // ReminderOpen (default), ReminderDone or ReminderAll
	ContactID int64
	From, To  string // inclusive due-date range; excludes undated reminders when set
	Query     string // text search over title and notes
	Limit     int    // default 200
}

const reminderSelect = `
SELECT r.id, r.title, r.notes, r.due_date, r.due_time, r.contact_id, c.name,
       COALESCE(r.done_at, ''), r.created_at
FROM reminders r LEFT JOIN contacts c ON c.id = r.contact_id`

func scanReminder(row interface{ Scan(...any) error }) (Reminder, error) {
	var r Reminder
	var cid sql.NullInt64
	var cname sql.NullString
	err := row.Scan(&r.ID, &r.Title, &r.Notes, &r.DueDate, &r.DueTime, &cid, &cname, &r.DoneAt, &r.CreatedAt)
	r.ContactID, r.ContactName = contactName(cid, cname)
	return r, err
}

// CreateReminder inserts an open reminder.
func (s *Store) CreateReminder(r Reminder) (Reminder, error) {
	res, err := s.db.Exec(`
INSERT INTO reminders (title, notes, due_date, due_time, contact_id) VALUES (?, ?, ?, ?, ?)`,
		strings.TrimSpace(r.Title), strings.TrimSpace(r.Notes), r.DueDate, r.DueTime, nullID(r.ContactID))
	if err != nil {
		return Reminder{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetReminder(id)
}

// UpdateReminder replaces a reminder's title, notes, date, time and contact.
// Completion is changed with SetReminderDone.
func (s *Store) UpdateReminder(r Reminder) (Reminder, error) {
	err := checkAffected(s.db.Exec(`
UPDATE reminders SET title = ?, notes = ?, due_date = ?, due_time = ?, contact_id = ? WHERE id = ?`,
		strings.TrimSpace(r.Title), strings.TrimSpace(r.Notes), r.DueDate, r.DueTime, nullID(r.ContactID), r.ID))
	if err != nil {
		return Reminder{}, err
	}
	return s.GetReminder(r.ID)
}

// SetReminderDone marks a reminder complete (now) or reopens it.
func (s *Store) SetReminderDone(id int64, done bool) (Reminder, error) {
	q := `UPDATE reminders SET done_at = NULL WHERE id = ?`
	if done {
		// Keep the original completion time if it's already done.
		q = `UPDATE reminders SET done_at = COALESCE(done_at, datetime('now', 'localtime')) WHERE id = ?`
	}
	if err := checkAffected(s.db.Exec(q, id)); err != nil {
		return Reminder{}, err
	}
	return s.GetReminder(id)
}

// GetReminder returns one reminder by id, or ErrNotFound.
func (s *Store) GetReminder(id int64) (Reminder, error) {
	r, err := scanReminder(s.db.QueryRow(reminderSelect+` WHERE r.id = ?`, id))
	return r, notFound(err)
}

// DeleteReminder removes a reminder.
func (s *Store) DeleteReminder(id int64) error {
	return checkAffected(s.db.Exec(`DELETE FROM reminders WHERE id = ?`, id))
}

// ListReminders returns reminders matching f. Open reminders come soonest
// first (all-day before timed, undated last); completed ones most recently
// completed first.
func (s *Store) ListReminders(f ReminderFilter) ([]Reminder, error) {
	var where []string
	var args []any
	switch f.Status {
	case ReminderDone:
		where = append(where, `r.done_at IS NOT NULL`)
	case ReminderAll:
	default:
		where = append(where, `r.done_at IS NULL`)
	}
	if f.ContactID != 0 {
		where = append(where, `r.contact_id = ?`)
		args = append(args, f.ContactID)
	}
	if f.From != "" {
		where = append(where, `r.due_date <> '' AND r.due_date >= ?`)
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, `r.due_date <> '' AND r.due_date <= ?`)
		args = append(args, f.To)
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, `(r.title LIKE ? ESCAPE '\' OR r.notes LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')`)
		like := likeArg(f.Query)
		args = append(args, like, like, like)
	}
	q := reminderSelect
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	if f.Status == ReminderDone {
		q += ` ORDER BY r.done_at DESC, r.id DESC`
	} else {
		q += ` ORDER BY r.done_at IS NOT NULL, r.due_date = '', r.due_date, r.due_time, r.id`
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	q += ` LIMIT ?`
	args = append(args, f.Limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
