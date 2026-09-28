package server

import (
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jwald3/tandem/internal/assistant"
	"github.com/jwald3/tandem/internal/markdown"
	"github.com/jwald3/tandem/internal/store"
)

// --- Chat tab (home): conversations ---

const chatDisabledMsg = "Chat is disabled. Add your Anthropic API key (API key button, bottom left) to enable the assistant."

// chatData is the model for the chat page.
type chatData struct {
	page
	ChatEnabled bool
	Settings    settingsData
	Threads     []store.ChatThread
	Thread      *store.ChatThread // nil = new, unsaved conversation
	Messages    []store.ChatMessage
	Agenda      agendaData
	Prompt      string // prefilled composer text (?prompt=...)
}

// threadListData renders the sidebar list with the active thread highlighted.
type threadListData struct {
	Threads  []store.ChatThread
	ActiveID int64
	OOB      bool // render as an htmx out-of-band swap
}

func (d chatData) ThreadList() threadListData {
	var id int64
	if d.Thread != nil {
		id = d.Thread.ID
	}
	return threadListData{Threads: d.Threads, ActiveID: id}
}

func (app *App) renderChat(w http.ResponseWriter, r *http.Request, thread *store.ChatThread) {
	threads, _ := app.store.ListThreads(200)
	data := chatData{
		page:        newPage("Chat", "chat"),
		ChatEnabled: app.chatEnabled(),
		Settings:    app.settingsData(),
		Threads:     threads,
		Thread:      thread,
		Agenda:      app.agendaData(),
		Prompt:      r.URL.Query().Get("prompt"),
	}
	if thread != nil {
		data.PageTitle = thread.Title
		data.Messages, _ = app.store.ListChatMessages(thread.ID, 500)
	}
	app.render(w, "chat.html", data)
}

// handleChatHome shows a fresh conversation (the thread is created on first send).
func (app *App) handleChatHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	app.renderChat(w, r, nil)
}

// handleChatThread shows an existing conversation.
func (app *App) handleChatThread(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, ok := app.store.GetThread(id)
	if !ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	app.renderChat(w, r, &t)
}

func (app *App) writeThreadList(w http.ResponseWriter, activeID int64, oob bool) {
	threads, _ := app.store.ListThreads(200)
	app.render(w, "thread_list.html", threadListData{Threads: threads, ActiveID: activeID, OOB: oob})
}

// handleDeleteThread removes a conversation. Deleting the one on screen sends
// the browser back to a new chat; otherwise just the sidebar is refreshed.
func (app *App) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if err := app.store.DeleteThread(id); err != nil {
		serverError(w, err)
		return
	}
	current, _ := strconv.ParseInt(r.FormValue("current"), 10, 64)
	if current == id {
		w.Header().Set("HX-Redirect", "/")
		return
	}
	app.writeThreadList(w, current, false)
}

// handleRenameThread sets a conversation's title.
func (app *App) handleRenameThread(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	if title := field(r, "title"); title != "" {
		if err := app.store.RenameThread(id, truncateTitle(title, 80)); err != nil {
			serverError(w, err)
			return
		}
	}
	current, _ := strconv.ParseInt(r.FormValue("current"), 10, 64)
	app.writeThreadList(w, current, false)
}

// truncateTitle shortens s to at most n runes, cutting at a word boundary.
func truncateTitle(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// --- Chat: sending a message and receiving the reply ---

func (app *App) handleChat(w http.ResponseWriter, r *http.Request) {
	agent := app.getAgent()
	if agent == nil {
		writeChatBubble(w, "assistant", chatDisabledMsg)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatFormBytes)
	if err := r.ParseMultipartForm(maxChatFormBytes); err != nil {
		// Plain (non-multipart) posts still work, e.g. from tests or curl.
		if !errors.Is(err, http.ErrNotMultipart) {
			http.Error(w, "That upload is too large. Try fewer or smaller photos.", http.StatusRequestEntityTooLarge)
			return
		}
		if err := r.ParseForm(); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	userMsg := field(r, "message")
	images, err := readChatImages(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if userMsg == "" && len(images) == 0 {
		return
	}

	// Resolve the thread, creating one on the first message of a new chat.
	threadID, _ := strconv.ParseInt(r.FormValue("thread_id"), 10, 64)
	isNew := false
	if _, ok := app.store.GetThread(threadID); !ok {
		title := truncateTitle(userMsg, 40)
		if title == "" {
			title = "Photo"
		}
		id, err := app.store.CreateThread(title)
		if err != nil {
			serverError(w, err)
			return
		}
		threadID, isNew = id, true
	}

	history, _ := app.store.ListChatMessages(threadID, 40)
	msgID, err := app.store.AddChatMessageWithImages(threadID, "user", userMsg, images)
	if err != nil {
		serverError(w, err)
		return
	}
	saved, _ := app.store.GetChatMessage(msgID)

	// Insert a pending assistant reply and generate it in the background, so the
	// answer completes and is saved even if the user navigates away or reloads.
	// The UI polls /chat/msg/{id} until it flips to done/error.
	pendingID, err := app.store.AddPendingAssistant(threadID)
	if err != nil {
		serverError(w, err)
		return
	}
	go app.generateReply(agent, threadID, pendingID, history, userMsg, images, isNew)

	if isNew {
		// Put the new thread in the URL so reload/back work like a normal page.
		w.Header().Set("HX-Push-Url", "/c/"+strconv.FormatInt(threadID, 10))
	}
	// The user bubble was shown optimistically client-side; re-render it
	// authoritatively, then a polling placeholder for the reply. Refresh the
	// sidebar and thread id out-of-band so follow-ups land in the same thread.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeUserBubble(w, saved)
	writePendingBubble(w, pendingID)
	w.Write([]byte(`<input type="hidden" name="thread_id" id="thread-id" value="` + strconv.FormatInt(threadID, 10) + `" hx-swap-oob="true">`))
	app.writeThreadList(w, threadID, true)
}

// generateReply runs the assistant's tool-use loop off the request path and
// writes the result into the pending row. It deliberately uses no request
// context, so a client disconnect (tab switch, reload, close) never cancels it.
func (app *App) generateReply(agent *assistant.Agent, threadID, pendingID int64, history []store.ChatMessage, userMsg string, images []store.ChatImage, isNew bool) {
	reply, mutated, err := agent.Chat(history, userMsg, images)
	status := store.StatusDone
	if err != nil {
		log.Printf("chat: %v", err)
		reply = err.Error()
		status = store.StatusError
	}
	if e := app.store.FinishChatMessage(pendingID, reply, status, mutated); e != nil {
		log.Printf("chat: save reply: %v", e)
	}
	if isNew && status == store.StatusDone {
		if userMsg == "" {
			userMsg = "(sent a photo)"
		}
		if title := agent.TitleFor(userMsg, reply); title != "" {
			_ = app.store.RenameThread(threadID, title)
		}
	}
}

// handleChatMessage is the poll endpoint for a single assistant reply. While
// the reply is pending it returns the same placeholder (which keeps polling);
// once it's done or errored it returns the final bubble with no poll trigger,
// so polling stops, plus an out-of-band sidebar refresh to pick up a new title.
// A reply that changed the user's data fires a "data-changed" event so the
// agenda panel reloads.
func (app *App) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	m, ok := app.store.GetChatMessage(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if m.Pending() {
		writePendingBubble(w, id)
		return
	}
	if m.Errored() {
		writeErrorBubble(w, id, m.Content)
		return
	}
	if m.Mutated {
		w.Header().Set("HX-Trigger", "data-changed")
	}
	writeChatBubble(w, "assistant", m.Content)
	app.writeThreadList(w, m.ThreadID, true)
}

// handleRetryChat regenerates a failed assistant reply in place: it re-runs the
// user turn that produced it, flipping the same message row back to pending and
// returning a polling placeholder, exactly like a fresh send.
func (app *App) handleRetryChat(w http.ResponseWriter, r *http.Request) {
	agent := app.getAgent()
	if agent == nil {
		writeChatBubble(w, "assistant", chatDisabledMsg)
		return
	}
	id, err := pathID(r)
	if err != nil {
		badRequest(w, "bad id")
		return
	}
	msg, ok := app.store.GetChatMessage(id)
	if !ok || msg.Role != "assistant" || !msg.Errored() {
		badRequest(w, "nothing to retry")
		return
	}
	user, ok := app.store.PrecedingUserMessage(msg.ThreadID, msg.ID)
	if !ok {
		badRequest(w, "no message to retry")
		return
	}
	images, _ := app.store.ImagesForMessage(user.ID)

	// History is everything before the user turn we're re-running.
	all, _ := app.store.ListChatMessages(msg.ThreadID, 500)
	var history []store.ChatMessage
	for _, m := range all {
		if m.ID >= user.ID {
			break
		}
		history = append(history, m)
	}

	if err := app.store.ResetToPending(msg.ID); err != nil {
		serverError(w, err)
		return
	}
	go app.generateReply(agent, msg.ThreadID, msg.ID, history, user.Content, images, false)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writePendingBubble(w, msg.ID)
}

// handleChatImage serves an attached photo for display in the conversation.
func (app *App) handleChatImage(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	img, ok := app.store.GetChatImage(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeImage(w, img.MediaType, img.Data)
}

// --- Chat bubbles ---

// writeChatBubble emits a single chat message bubble. Assistant messages are
// rendered as Markdown; user messages stay plain text.
func writeChatBubble(w http.ResponseWriter, role, content string) {
	var body string
	if role == "assistant" {
		body = string(markdown.Render(content))
	} else {
		body = strings.ReplaceAll(html.EscapeString(content), "\n", "<br>")
	}
	writeHTML(w, `<div class="bubble `+role+` md">`+body+`</div>`)
}

// writeUserBubble renders a stored user message, thumbnails first.
func writeUserBubble(w http.ResponseWriter, m store.ChatMessage) {
	body := imagesHTML(m.Images) + strings.ReplaceAll(html.EscapeString(m.Content), "\n", "<br>")
	writeHTML(w, `<div class="bubble user md">`+body+`</div>`)
}

// imagesHTML renders the thumbnail strip for a message's attached photos.
func imagesHTML(imgs []store.ChatImage) string {
	if len(imgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<div class="bubble-images">`)
	for _, img := range imgs {
		src := "/chat/img/" + strconv.FormatInt(img.ID, 10)
		sb.WriteString(`<a href="` + src + `" target="_blank" rel="noopener"><img src="` + src + `" alt="Attached photo" loading="lazy"></a>`)
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

// writeErrorBubble emits a failed reply: what went wrong and a Retry button
// that re-runs the last user turn in place.
func writeErrorBubble(w http.ResponseWriter, id int64, detail string) {
	writeHTML(w, errorBubbleHTML(id, detail))
}

// errorBubbleHTML is the markup for a failed reply, shared by the poll endpoint
// and the page-load template.
func errorBubbleHTML(id int64, detail string) string {
	sid := strconv.FormatInt(id, 10)
	title := ""
	if detail != "" {
		title = ` title="` + html.EscapeString(detail) + `"`
	}
	return `<div class="bubble assistant error" id="msg-` + sid + `">` +
		`<span class="error-text"` + title + `>That didn't go through.</span> ` +
		`<button type="button" class="btn tiny retry" ` +
		`hx-post="/chat/msg/` + sid + `/retry" ` +
		`hx-target="#msg-` + sid + `" hx-swap="outerHTML">Retry</button>` +
		`</div>`
}

// writePendingBubble emits the "thinking" placeholder for an in-flight reply.
// It polls GET /chat/msg/{id} every 1.5s and replaces itself with the result;
// because it re-renders from the database, it resumes automatically after a
// reload or when the user returns to the tab.
func writePendingBubble(w http.ResponseWriter, id int64) {
	sid := strconv.FormatInt(id, 10)
	w.Write([]byte(`<div class="bubble assistant pending" ` +
		`hx-get="/chat/msg/` + sid + `" hx-trigger="load delay:1500ms" ` +
		`hx-swap="outerHTML" hx-target="this">` +
		`<span class="typing"><i></i><i></i><i></i></span></div>`))
}
