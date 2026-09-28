package assistant

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jwald3/tandem/internal/dates"
	"github.com/jwald3/tandem/internal/store"
	"github.com/jwald3/tandem/internal/testutil"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	// Close before TempDir cleanup so Windows can delete the file.
	t.Cleanup(func() { st.Close() })
	return st
}

// call runs a tool and fails the test on error.
func call(t *testing.T, a *Agent, name, input string) (string, bool) {
	t.Helper()
	out, mutated, err := a.runTool(name, json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s(%s): %v", name, input, err)
	}
	return out, mutated
}

// callErr runs a tool that is expected to fail and returns the error text.
func callErr(t *testing.T, a *Agent, name, input string) string {
	t.Helper()
	_, _, err := a.runTool(name, json.RawMessage(input))
	if err == nil {
		t.Fatalf("%s(%s) should have failed", name, input)
	}
	return err.Error()
}

func TestToolDefinitions(t *testing.T) {
	if len(toolsByName) != len(allTools) {
		t.Fatal("duplicate tool names")
	}
	for _, tl := range allTools {
		if tl.description == "" || tl.run == nil {
			t.Errorf("%s: missing description or implementation", tl.name)
		}
		for _, req := range tl.required {
			if _, ok := tl.props[req]; !ok {
				t.Errorf("%s: required field %q is not a property", tl.name, req)
			}
		}
	}
	if _, _, err := (&Agent{}).runTool("no_such_tool", nil); err == nil {
		t.Fatal("unknown tool should error")
	}
}

// The motivating example: a coworker, a meeting about a project, and
// everything else hanging off the contact.
func TestContactCentricWorkflow(t *testing.T) {
	a := &Agent{store: newTestStore(t)}

	out, mutated := call(t, a, "create_contact", `{"name":"Alex Rivera","relationship":"coworker"}`)
	if !mutated || !strings.Contains(out, "Alex Rivera (id 1; coworker)") {
		t.Fatalf("create_contact: %q", out)
	}
	if msg := callErr(t, a, "create_contact", `{"name":"alex rivera"}`); !strings.Contains(msg, "already exists") {
		t.Fatalf("duplicate contact should be refused: %s", msg)
	}

	// Link by partial name, not just id.
	out, _ = call(t, a, "create_reminder", `{"title":"Meet with Alex about Atlas","due_date":"2026-09-28","due_time":"2pm","contact":"alex"}`)
	if !strings.Contains(out, "Monday, 2026-09-28 at 2:00 PM") || !strings.Contains(out, "with Alex Rivera (contact 1)") {
		t.Fatalf("create_reminder should echo the weekday, time and contact: %q", out)
	}
	call(t, a, "log_interaction", `{"summary":"Intro chat about Atlas","kind":"call","date":"2026-09-21","contact":1}`)
	call(t, a, "add_note", `{"title":"Atlas","body":"Alex owns the Atlas rollout","contact":"Alex Rivera"}`)
	call(t, a, "update_contact", `{"contact":1,"company":"Acme","title":"PM"}`)

	out, mutated = call(t, a, "get_contact", `{"contact":"Rivera"}`)
	for _, want := range []string{"PM, Acme · coworker", "#1 Meet with Alex about Atlas", "Intro chat about Atlas", "Alex owns the Atlas rollout"} {
		if !strings.Contains(out, want) {
			t.Errorf("get_contact missing %q:\n%s", want, out)
		}
	}
	if mutated {
		t.Error("get_contact must not report a mutation")
	}

	out, _ = call(t, a, "search", `{"query":"atlas"}`)
	if !strings.Contains(out, "Reminders (1)") || !strings.Contains(out, "Interactions (1)") || !strings.Contains(out, "Notes (1)") {
		t.Fatalf("search should find the project everywhere:\n%s", out)
	}

	// Reschedule, then clear the date (which also clears the time), unlink, complete.
	out, _ = call(t, a, "update_reminder", `{"id":1,"due_date":"2026-09-29"}`)
	if !strings.Contains(out, "Tuesday, 2026-09-29 at 2:00 PM") {
		t.Fatalf("reschedule should keep the time: %q", out)
	}
	out, _ = call(t, a, "update_reminder", `{"id":1,"due_date":"","contact":"none"}`)
	if !strings.Contains(out, "no date") || strings.Contains(out, "Alex") && strings.Contains(out, "contact 1") {
		t.Fatalf("clearing date/contact: %q", out)
	}
	call(t, a, "complete_reminder", `{"id":1}`)
	if out, _ := call(t, a, "list_reminders", `{}`); !strings.Contains(out, "No matching reminders") {
		t.Fatalf("completed reminder should leave the open list: %q", out)
	}
	if out, _ := call(t, a, "list_reminders", `{"status":"done"}`); !strings.Contains(out, "#1") {
		t.Fatalf("done list: %q", out)
	}
}

func TestToolValidation(t *testing.T) {
	a := &Agent{store: newTestStore(t)}
	call(t, a, "create_contact", `{"name":"Alex Rivera"}`)
	call(t, a, "create_contact", `{"name":"Alexandra Diaz"}`)

	cases := map[string][2]string{
		"ambiguous name":   {"create_reminder", `{"title":"x","contact":"al"}`},
		"unknown name":     {"create_reminder", `{"title":"x","contact":"Zed"}`},
		"unknown id":       {"log_interaction", `{"summary":"x","contact":99}`},
		"bad date":         {"create_reminder", `{"title":"x","due_date":"2026-02-30"}`},
		"time without day": {"create_reminder", `{"title":"x","due_time":"09:00"}`},
		"bad time":         {"create_reminder", `{"title":"x","due_date":"2026-10-01","due_time":"25:00"}`},
		"missing reminder": {"complete_reminder", `{"id":42}`},
		"empty note":       {"add_note", `{"body":"  "}`},
	}
	for name, c := range cases {
		msg := callErr(t, a, c[0], c[1])
		if name == "ambiguous name" && !(strings.Contains(msg, "Alex Rivera (id 1)") && strings.Contains(msg, "Alexandra Diaz (id 2)")) {
			t.Errorf("ambiguity error should list the candidates with ids: %s", msg)
		}
	}
	// Nothing was created by the failed calls.
	if rs, _ := a.store.ListReminders(store.ReminderFilter{Status: store.ReminderAll}); len(rs) != 0 {
		t.Fatalf("failed calls must not write: %+v", rs)
	}
}

func TestBuildMessages(t *testing.T) {
	st := newTestStore(t)
	a := &Agent{store: st}
	tid, _ := st.CreateThread("t")
	st.AddChatMessageWithImages(tid, "user", "", []store.ChatImage{{MediaType: "image/png", Data: testutil.TinyPNG}})
	st.AddChatMessage(tid, "assistant", "That's Alex's business card.")
	st.AddChatMessage(tid, "user", "add him")
	failed, _ := st.AddPendingAssistant(tid)
	st.FinishChatMessage(failed, "boom", store.StatusError, false)
	history, _ := st.ListChatMessages(tid, 10)

	msgs := a.buildMessages(history, "try again", nil)
	raw, _ := json.Marshal(msgs)
	var decoded []struct {
		Role    string `json:"role"`
		Content []struct {
			Type   string `json:"type"`
			Text   string `json:"text"`
			Source *struct {
				Type, MediaType, Data string
			} `json:"source"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// The errored reply is skipped; the photo turn is [image, stand-in text].
	if len(decoded) != 4 {
		t.Fatalf("want 4 messages (errored reply skipped), got %d: %s", len(decoded), raw)
	}
	first := decoded[0].Content
	if len(first) != 2 || first[0].Type != "image" || first[0].Source == nil || first[0].Source.Data == "" || first[1].Text == "" {
		t.Fatalf("photo turn should be [image, caption]: %s", raw)
	}
	if decoded[1].Role != "assistant" || decoded[3].Content[0].Text != "try again" {
		t.Fatalf("wrong roles/order: %s", raw)
	}
}

func TestLiveContext(t *testing.T) {
	st := newTestStore(t)
	alex, _ := st.CreateContact(store.Contact{Name: "Alex Rivera", Relationship: "coworker"})
	st.CreateReminder(store.Reminder{Title: "Meet about Atlas", DueDate: dates.DaysFromToday(1), ContactID: alex.ID})
	st.CreateReminder(store.Reminder{Title: "Late thing", DueDate: dates.DaysFromToday(-2)})
	st.CreateReminder(store.Reminder{Title: "Someday", DueDate: ""})

	ctx := liveContext(st)
	for _, want := range []string{
		"Today is " + dates.Long(dates.Today()),
		dates.Long(dates.DaysFromToday(1)) + " (tomorrow)",
		dates.Long(dates.DaysFromToday(14)),
		"Overdue:", "#2 Late thing — " + dates.Long(dates.DaysFromToday(-2)) + " (OVERDUE)",
		"Meet about Atlas", "with Alex Rivera (contact 1)",
		"1 with no date",
		"Alex Rivera (id 1; coworker)",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("live context missing %q:\n%s", want, ctx)
		}
	}
}

// fakeAPI is a stand-in Messages API: it records each request and answers
// with the next scripted response.
type fakeAPI struct {
	mu       sync.Mutex
	replies  []string
	requests []map[string]any
	headers  []http.Header
}

func (f *fakeAPI) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request is not JSON: %s", body)
		}
		f.requests = append(f.requests, req)
		f.headers = append(f.headers, r.Header.Clone())
		if len(f.replies) == 0 {
			t.Error("unexpected extra request")
			w.WriteHeader(500)
			return
		}
		reply := f.replies[0]
		f.replies = f.replies[1:]
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(reply, "400 ") {
			w.WriteHeader(400)
			reply = reply[4:]
		}
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func msgJSON(stop string, content string) string {
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"` + stop +
		`","content":[` + content + `],"usage":{"input_tokens":10,"output_tokens":10}}`
}

func TestChatToolLoop(t *testing.T) {
	st := newTestStore(t)
	api := &fakeAPI{replies: []string{
		// Turn 1: think, then two parallel tool calls.
		msgJSON("tool_use", `
			{"type":"thinking","thinking":"","signature":"sig-abc"},
			{"type":"tool_use","id":"toolu_1","name":"create_contact","input":{"name":"Alex Rivera","relationship":"coworker"}},
			{"type":"tool_use","id":"toolu_2","name":"create_reminder","input":{"title":"Meet with Alex about Atlas","due_date":"2026-09-28","contact":"Zed"}}`),
		// Turn 2: after seeing the error, fix the second call.
		msgJSON("tool_use", `{"type":"tool_use","id":"toolu_3","name":"create_reminder","input":{"title":"Meet with Alex about Atlas","due_date":"2026-09-28","contact":"Alex Rivera"}}`),
		// Turn 3: final answer.
		msgJSON("end_turn", `{"type":"text","text":"Added: meet Alex Rivera about Atlas, Mon Sep 28."}`),
	}}
	srv := api.serve(t)
	a := New("sk-ant-test", srv.URL, st)

	reply, mutated, err := a.Chat(nil, "I have to meet my coworker Alex Rivera Monday about Atlas", nil)
	if err != nil || !mutated || !strings.Contains(reply, "Added: meet Alex Rivera") {
		t.Fatalf("Chat: reply=%q mutated=%v err=%v", reply, mutated, err)
	}
	if len(api.requests) != 3 {
		t.Fatalf("want 3 API calls, got %d", len(api.requests))
	}

	// Request shape: model, adaptive thinking, default fallbacks + beta header,
	// cached system prompt, the full tool list.
	req := api.requests[0]
	if req["model"] != "claude-opus-5" {
		t.Errorf("model = %v", req["model"])
	}
	if th, _ := req["thinking"].(map[string]any); th["type"] != "adaptive" {
		t.Errorf("thinking = %v", req["thinking"])
	}
	if req["fallbacks"] != "default" {
		t.Errorf("fallbacks = %v", req["fallbacks"])
	}
	if h := api.headers[0].Get("anthropic-beta"); !strings.Contains(h, "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta = %q", h)
	}
	if h := api.headers[0].Get("x-api-key"); h != "sk-ant-test" {
		t.Errorf("x-api-key = %q", h)
	}
	system, _ := req["system"].([]any)
	if len(system) != 2 || system[0].(map[string]any)["cache_control"] == nil {
		t.Errorf("system should be [cached prompt, live context]: %v", req["system"])
	}
	if tools, _ := req["tools"].([]any); len(tools) != len(allTools) {
		t.Errorf("sent %d tools, want %d", len(tools), len(allTools))
	}

	// Request 2 echoes the assistant turn (thinking signature intact) and
	// returns both tool results in one user message, the bad one as an error.
	msgs := api.requests[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("request 2 should have user, assistant, tool results; got %d", len(msgs))
	}
	echoed, _ := json.Marshal(msgs[1])
	if !strings.Contains(string(echoed), `"signature":"sig-abc"`) {
		t.Errorf("thinking block must be echoed verbatim: %s", echoed)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 {
		t.Fatalf("want 2 tool results in one turn, got %d", len(results))
	}
	ok, bad := results[0].(map[string]any), results[1].(map[string]any)
	if ok["tool_use_id"] != "toolu_1" || ok["is_error"] == true {
		t.Errorf("first result: %v", ok)
	}
	if bad["tool_use_id"] != "toolu_2" || bad["is_error"] != true || !strings.Contains(asText(bad["content"]), "no contact matches") {
		t.Errorf("second result should be an error explaining the bad contact: %v", bad)
	}

	rs, _ := st.ListReminders(store.ReminderFilter{})
	if len(rs) != 1 || rs[0].ContactName != "Alex Rivera" || rs[0].DueDate != "2026-09-28" {
		t.Fatalf("reminder not saved as expected: %+v", rs)
	}
}

// asText flattens a tool_result content field (string or [{type:text}]).
func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestChatRefusalAndErrors(t *testing.T) {
	st := newTestStore(t)
	api := &fakeAPI{replies: []string{
		msgJSON("refusal", ``),
		`400 {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`,
	}}
	a := New("sk-ant-test", api.serve(t).URL, st)

	reply, _, err := a.Chat(nil, "hi", nil)
	if err != nil || reply != "I can't help with that one." {
		t.Fatalf("refusal: %q %v", reply, err)
	}
	_, _, err = a.Chat(nil, "hi", nil)
	if err == nil || !strings.Contains(err.Error(), "(400): prompt is too long") {
		t.Fatalf("API errors should surface the API's message: %v", err)
	}
}
