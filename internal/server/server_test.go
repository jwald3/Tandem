package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/seed"
	"github.com/jwald3/tandem/internal/store"
)

// newTestApp builds an App with a real in-temp store and the real templates,
// but no agent (we don't hit the Anthropic API here).
func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	// Close the DB before TempDir cleanup so Windows can delete the file.
	t.Cleanup(func() { st.Close() })
	app, err := New(st, "")
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// get runs a GET through the real router.
func get(app *App, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
	return rr
}

// post submits a form through the real router.
func post(app *App, path string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, req)
	return rr
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// getPage asserts a full page rendered completely (a template error midway
// leaves the document truncated) and returns its body.
func getPage(t *testing.T, app *App, path string) string {
	t.Helper()
	rr := get(app, path)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "</html>") {
		t.Fatalf("GET %s: status %d, complete=%v\n%s", path, rr.Code, strings.Contains(body, "</html>"), body)
	}
	return body
}

func TestEveryPageRenders(t *testing.T) {
	app := newTestApp(t)
	// Empty database first: every page must handle having nothing.
	for _, p := range []string{"/", "/reminders", "/reminders?view=done", "/contacts", "/interactions", "/notes"} {
		getPage(t, app, p)
	}

	if err := seed.Demo(app.store); err != nil {
		t.Fatal(err)
	}
	checks := map[string][]string{
		"/":                       {"Agenda", "Meet with Alex about Atlas", "Overdue", "Send Marcus the revised proposal", "Add your Anthropic API key"},
		"/reminders":              {"Overdue", "Today", "No date", "Renew passport", `href="/contacts/1"`, "Bring the updated rollout timeline."},
		"/reminders?view=done":    {"Draft Atlas launch checklist"},
		"/contacts":               {"Alex Rivera", "Product Manager, Northwind · coworker", "AR"},
		"/contacts?q=northwind":   {"Alex Rivera", "Priya Shah"},
		"/contacts/1":             {"Alex Rivera", "Leads the Atlas launch", "Meet with Alex about Atlas", "Atlas kickoff", "Launch target is mid-November", "Completed (1)"},
		"/interactions":           {"Atlas kickoff", "kind-meeting", "Marcus is happy"},
		"/interactions?contact=1": {"Atlas kickoff"},
		"/notes":                  {"Gift ideas", "Allergic to shellfish"},
		"/notes?q=ramen":          {"Allergic to shellfish"},
	}
	for path, wants := range checks {
		body := getPage(t, app, path)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: missing %q", path, want)
			}
		}
	}
	if body := getPage(t, app, "/contacts?q=northwind"); strings.Contains(body, "Dana Whitfield") {
		t.Error("contact search should filter")
	}
	if body := getPage(t, app, "/interactions?contact=1"); strings.Contains(body, "Marcus is happy") {
		t.Error("interaction contact filter should filter")
	}
	if get(app, "/contacts/999").Code != http.StatusNotFound {
		t.Error("missing contact should 404")
	}
}

func TestContactFormFlow(t *testing.T) {
	app := newTestApp(t)

	rr := post(app, "/contacts", url.Values{"name": {"Alex Rivera"}, "relationship": {"coworker"}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/contacts/1" {
		t.Fatalf("create contact: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := post(app, "/contacts", url.Values{"name": {"  "}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("blank name should be rejected: %d", rr.Code)
	}

	// Add a reminder, interaction and note from the contact's page.
	back := url.Values{"next": {"/contacts/1"}, "contact_id": {"1"}}
	form := func(extra url.Values) url.Values {
		v := url.Values{}
		for k, vs := range back {
			v[k] = vs
		}
		for k, vs := range extra {
			v[k] = vs
		}
		return v
	}
	for path, f := range map[string]url.Values{
		"/reminders":    form(url.Values{"title": {"Meet about Atlas"}, "due_date": {"2026-09-28"}, "due_time": {"14:00"}}),
		"/interactions": form(url.Values{"summary": {"Kickoff call"}, "kind": {"call"}}),
		"/notes":        form(url.Values{"body": {"Owns the Atlas rollout"}}),
	} {
		rr := post(app, path, f)
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/contacts/1" {
			t.Fatalf("POST %s: %d -> %s (%s)", path, rr.Code, rr.Header().Get("Location"), rr.Body.String())
		}
	}
	body := getPage(t, app, "/contacts/1")
	for _, want := range []string{"Meet about Atlas", "2:00 PM", "Kickoff call", "Owns the Atlas rollout"} {
		if !strings.Contains(body, want) {
			t.Errorf("contact page missing %q", want)
		}
	}
	in, _ := app.store.ListInteractions(store.InteractionFilter{})
	if len(in) != 1 || in[0].Date != dates.Today() {
		t.Fatalf("interaction date should default to today: %+v", in)
	}

	// Edit the contact and the reminder.
	post(app, "/contacts/1", url.Values{"name": {"Alex Rivera"}, "company": {"Northwind"}, "title": {"PM"}})
	if c, _ := app.store.GetContact(1); c.Company != "Northwind" || c.Relationship != "" {
		t.Fatalf("edit replaces all fields: %+v", c)
	}
	post(app, "/reminders/1", url.Values{"title": {"Meet Alex about Atlas"}, "due_date": {"2026-09-29"}, "contact_id": {"1"}})
	if r, _ := app.store.GetReminder(1); r.Title != "Meet Alex about Atlas" || r.DueDate != "2026-09-29" || r.DueTime != "" {
		t.Fatalf("edit reminder: %+v", r)
	}

	// Toggle done and back.
	post(app, "/reminders/1/toggle", url.Values{"next": {"/reminders"}})
	if r, _ := app.store.GetReminder(1); !r.Done() {
		t.Fatal("toggle should complete")
	}
	post(app, "/reminders/1/toggle", url.Values{})
	if r, _ := app.store.GetReminder(1); r.Done() {
		t.Fatal("second toggle should reopen")
	}

	// Deleting the contact keeps its items, unlinked.
	rr = post(app, "/contacts/1/delete", url.Values{})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/contacts" {
		t.Fatalf("delete contact: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if r, err := app.store.GetReminder(1); err != nil || r.ContactID != 0 {
		t.Fatalf("reminder should survive unlinked: %+v %v", r, err)
	}

	// Validation.
	if rr := post(app, "/reminders", url.Values{"title": {"x"}, "due_date": {"2026-02-30"}}); rr.Code != http.StatusBadRequest {
		t.Errorf("bad date should 400, got %d", rr.Code)
	}
	if rr := post(app, "/notes/99", url.Values{"body": {"x"}}); rr.Code != http.StatusNotFound {
		t.Errorf("editing a missing note should 404, got %d", rr.Code)
	}
}

func TestRedirectBackStaysOnSite(t *testing.T) {
	app := newTestApp(t)
	for next, want := range map[string]string{
		"/contacts/1":      "/contacts/1",
		"//evil.example":   "/notes",
		`/\evil.example`:   "/notes",
		"https://evil.com": "/notes",
		"":                 "/notes",
	} {
		rr := post(app, "/notes", url.Values{"body": {"hi"}, "next": {next}})
		if got := rr.Header().Get("Location"); got != want {
			t.Errorf("next=%q redirected to %q, want %q", next, got, want)
		}
	}
}

func TestAgendaToggleOverHTMX(t *testing.T) {
	app := newTestApp(t)
	r, _ := app.store.CreateReminder(store.Reminder{Title: "Call Mom", DueDate: dates.Today()})
	rr := post(app, "/reminders/"+itoa(r.ID)+"/toggle", url.Values{}, "HX-Request", "true")
	body := rr.Body.String()
	if rr.Code != 200 || !strings.Contains(body, `id="agenda"`) || strings.Contains(body, "Call Mom") {
		t.Fatalf("HTMX toggle should return the refreshed agenda without the completed item: %d\n%s", rr.Code, body)
	}
	if body := get(app, "/agenda").Body.String(); !strings.Contains(body, "Nothing due") {
		t.Fatalf("agenda fragment: %s", body)
	}
}

func TestUserTextIsEscaped(t *testing.T) {
	app := newTestApp(t)
	evil := `<script>alert(1)</script>`
	c, _ := app.store.CreateContact(store.Contact{Name: evil, About: evil})
	app.store.CreateReminder(store.Reminder{Title: evil, Notes: evil, DueDate: dates.Today(), ContactID: c.ID})
	app.store.CreateInteraction(store.Interaction{Summary: evil, ContactID: c.ID})
	app.store.CreateNote(store.Note{Title: evil, Body: evil, ContactID: c.ID})
	for _, p := range []string{"/", "/reminders", "/contacts", "/contacts/" + itoa(c.ID), "/interactions", "/notes", "/?prompt=" + url.QueryEscape(evil)} {
		if body := getPage(t, app, p); strings.Contains(body, evil) {
			t.Errorf("%s renders user text unescaped", p)
		}
	}
}

func TestChatReplyLifecycle(t *testing.T) {
	app := newTestApp(t)

	// Without a key, sending explains how to enable chat.
	if body := post(app, "/chat", url.Values{"message": {"hi"}}).Body.String(); !strings.Contains(body, "Chat is disabled") {
		t.Fatalf("chat without a key: %s", body)
	}

	tid, _ := app.store.CreateThread("t")
	app.store.AddChatMessage(tid, "user", "remind me to call Alex")
	pid, _ := app.store.AddPendingAssistant(tid)

	body := get(app, "/chat/msg/"+itoa(pid)).Body.String()
	if !strings.Contains(body, `hx-get="/chat/msg/`+itoa(pid)+`"`) {
		t.Fatalf("pending poll should return a self-polling placeholder: %s", body)
	}
	if body := getPage(t, app, "/c/"+itoa(tid)); !strings.Contains(body, `hx-get="/chat/msg/`+itoa(pid)+`"`) {
		t.Fatal("reloading the thread should resume polling the pending reply")
	}

	app.store.FinishChatMessage(pid, "Added: **call Alex**, Mon Sep 28.", store.StatusDone, true)
	rr := get(app, "/chat/msg/"+itoa(pid))
	if strings.Contains(rr.Body.String(), `hx-get="/chat/msg/`) || !strings.Contains(rr.Body.String(), "<strong>call Alex</strong>") {
		t.Fatalf("finished reply should render markdown and stop polling: %s", rr.Body.String())
	}
	if rr.Header().Get("HX-Trigger") != "data-changed" {
		t.Fatalf("a reply that changed data should trigger an agenda refresh, got %q", rr.Header().Get("HX-Trigger"))
	}

	// A failed reply offers Retry and keeps the error detail as a tooltip.
	fid, _ := app.store.AddPendingAssistant(tid)
	app.store.FinishChatMessage(fid, "Claude API error (529): Overloaded", store.StatusError, false)
	body = get(app, "/chat/msg/"+itoa(fid)).Body.String()
	if !strings.Contains(body, "/retry") || !strings.Contains(body, "Overloaded") {
		t.Fatalf("error bubble: %s", body)
	}
}
