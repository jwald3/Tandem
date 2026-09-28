package server

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"sort"
	"strings"
	"unicode"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/markdown"
	"github.com/jwald3/tandem/internal/store"
	"github.com/jwald3/tandem/web"
)

// parseTemplates loads every page and fragment template.
func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseFS(web.Templates, "*.html")
}

// assetVersion is a short content hash of the embedded static assets, computed
// once at startup. It's appended to /static/ links (?v=...) so a changed CSS or
// JS file gets a new URL and browsers fetch it instead of serving a stale copy.
var assetVersion = hashStatic(web.Static)

// hashStatic returns the first 10 hex chars of a SHA-256 over every static
// file's path and contents, in sorted path order for a stable result.
func hashStatic(fsys fs.FS) string {
	h := sha256.New()
	var paths []string
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		io.WriteString(h, p)
		if f, err := fsys.Open(p); err == nil {
			io.Copy(h, f)
			f.Close()
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// rowData hands a list item to a row template along with what its edit form
// needs: the contact picker options and where to return after a post.
type rowData struct {
	I        any
	Contacts []store.Contact
	Next     string
	Kinds    []string
}

// pickerData drives the contact <select>.
type pickerData struct {
	Contacts []store.Contact
	Selected int64
}

// templateFuncs returns the helper functions available in every template.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"nl2br":    nl2br,
		"md":       markdown.Render,
		"icon":     icon,
		"assetver": func() string { return assetVersion },
		"imgs":     func(imgs []store.ChatImage) template.HTML { return template.HTML(imagesHTML(imgs)) },
		"errorbubble": func(m store.ChatMessage) template.HTML {
			return template.HTML(errorBubbleHTML(m.ID, m.Content))
		},
		"pretty":   dates.Pretty,
		"rel":      dates.Relative,
		"weekday":  dates.Weekday,
		"clock":    dates.Clock,
		"day":      func(ts string) string { return ts[:min(10, len(ts))] }, // date part of a timestamp
		"initials": initials,
		"isToday":  func(d string) bool { return d == dates.Today() },
		"row": func(item any, contacts []store.Contact, next string) rowData {
			return rowData{I: item, Contacts: contacts, Next: next, Kinds: store.InteractionKinds}
		},
		"picker": func(contacts []store.Contact, selected int64) pickerData {
			return pickerData{Contacts: contacts, Selected: selected}
		},
		"group": func(title string, items []store.Reminder) reminderGroup {
			return reminderGroup{Title: title, Items: items}
		},
	}
}

// reminderGroup is a titled run of reminders ("Overdue", "Today"...).
type reminderGroup struct {
	Title string
	Items []store.Reminder
}

// nl2br escapes text and converts newlines to <br> for safe HTML display.
func nl2br(s string) template.HTML {
	return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(s), "\n", "<br>"))
}

// initials returns up to two initials for an avatar, e.g. "AR".
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				out = append(out, unicode.ToUpper(r))
				break
			}
		}
	}
	switch len(out) {
	case 0:
		return "?"
	case 1:
		return string(out)
	}
	return string(out[0]) + string(out[len(out)-1])
}
