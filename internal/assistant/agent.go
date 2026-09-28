// Package assistant is Tandem's AI assistant: the tools that let Claude read
// and write the user's contacts, reminders, interactions and notes, and the
// tool-use loop that ties them to the Anthropic Messages API.
package assistant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/jwald3/tandem/internal/store"
)

const (
	model      = "claude-opus-5"
	titleModel = "claude-haiku-4-5" // cheap model for conversation titles

	maxTurns  = 12 // safety cap on tool-use round trips per message
	maxTokens = 16000
	chatLimit = 5 * time.Minute // wall-clock cap on one reply
)

// Agent runs the Claude tool-use loop against the user's data.
type Agent struct {
	client anthropic.Client
	store  *store.Store
}

// New builds an agent. baseURL overrides the API host (for proxies and tests);
// "" means the real API.
func New(apiKey, baseURL string, st *store.Store) *Agent {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Agent{client: anthropic.NewClient(opts...), store: st}
}

// Chat runs a full turn: it sends the conversation to Claude, executes any
// tool calls, and loops until Claude produces a final answer. It returns the
// reply text and whether any tool changed the user's data (so the UI knows to
// refresh).
func (a *Agent) Chat(history []store.ChatMessage, userMsg string, images []store.ChatImage) (reply string, mutated bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), chatLimit)
	defer cancel()

	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		// The instructions are fixed, so they (and the tools rendered ahead of
		// them) are cached; the live context block after them changes daily.
		System: []anthropic.BetaTextBlockParam{
			{Text: systemPrompt, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()},
			{Text: liveContext(a.store)},
		},
		Messages: a.buildMessages(history, userMsg, images),
		Tools:    toolParams(),
		Thinking: anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}},
		// Caches the growing conversation across tool round trips.
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		// If a safety classifier declines, retry on Anthropic's recommended
		// fallback model instead of returning a refusal.
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := a.client.Beta.Messages.New(ctx, params)
		if err != nil {
			return "", mutated, friendlyError(err)
		}
		// Echo the assistant turn back verbatim: thinking and tool_use blocks
		// must be preserved for the next request.
		params.Messages = append(params.Messages, resp.ToParam())

		switch resp.StopReason {
		case anthropic.BetaStopReasonToolUse:
		case anthropic.BetaStopReasonRefusal:
			return "I can't help with that one.", mutated, nil
		default:
			return collectText(resp.Content), mutated, nil
		}

		// Run every tool call and return all results in one user turn.
		var results []anthropic.BetaContentBlockParamUnion
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			call := block.AsToolUse()
			out, didMutate, terr := a.runTool(call.Name, json.RawMessage(call.JSON.Input.Raw()))
			if didMutate {
				mutated = true
			}
			if terr != nil {
				results = append(results, anthropic.NewBetaToolResultBlock(call.ID, "error: "+terr.Error(), true))
				continue
			}
			results = append(results, anthropic.NewBetaToolResultBlock(call.ID, out, false))
		}
		params.Messages = append(params.Messages, anthropic.NewBetaUserMessage(results...))
	}
	return "I got stuck working through that. Could you rephrase?", mutated, nil
}

// runTool dispatches a tool call by name.
func (a *Agent) runTool(name string, input json.RawMessage) (result string, mutated bool, err error) {
	t, ok := toolsByName[name]
	if !ok {
		return "", false, fmt.Errorf("unknown tool %q", name)
	}
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	return t.run(a, input)
}

// buildMessages converts stored chat history plus the new user turn into API
// messages. User turns carry their photos as image blocks ahead of the text;
// earlier photos are reloaded from the store so follow-ups keep them in view.
func (a *Agent) buildMessages(history []store.ChatMessage, userMsg string, images []store.ChatImage) []anthropic.BetaMessageParam {
	msgs := make([]anthropic.BetaMessageParam, 0, len(history)+1)
	for _, m := range history {
		if m.Role == "assistant" {
			// Skip replies still generating or failed: they have no usable
			// text, and the API rejects empty assistant turns.
			if m.Pending() || m.Errored() || strings.TrimSpace(m.Content) == "" {
				continue
			}
			msgs = append(msgs, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(m.Content)},
			})
			continue
		}
		var imgs []store.ChatImage
		for _, ref := range m.Images {
			if ref.Data != nil {
				imgs = append(imgs, ref)
			} else if full, ok := a.store.GetChatImage(ref.ID); ok {
				imgs = append(imgs, full)
			}
		}
		if parts := userParts(m.Content, imgs); len(parts) > 0 {
			msgs = append(msgs, anthropic.NewBetaUserMessage(parts...))
		}
	}
	return append(msgs, anthropic.NewBetaUserMessage(userParts(userMsg, images)...))
}

// userParts lays out a user turn as [images..., text]. The API rejects empty
// text blocks, so a photo-only message carries a short stand-in caption.
func userParts(text string, images []store.ChatImage) []anthropic.BetaContentBlockParamUnion {
	parts := make([]anthropic.BetaContentBlockParamUnion, 0, len(images)+1)
	for _, img := range images {
		parts = append(parts, anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data:      base64.StdEncoding.EncodeToString(img.Data),
			MediaType: anthropic.BetaBase64ImageSourceMediaType(img.MediaType),
		}))
	}
	text = strings.TrimSpace(text)
	if text == "" && len(images) > 0 {
		text = "(photo attached, no caption)"
	}
	if text != "" {
		parts = append(parts, anthropic.NewBetaTextBlock(text))
	}
	return parts
}

// collectText joins a reply's text blocks.
func collectText(blocks []anthropic.BetaContentBlockUnion) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	if s := strings.TrimSpace(sb.String()); s != "" {
		return s
	}
	return "(no response)"
}

// friendlyError keeps the API's own message (it reads better in the chat than
// a wrapped HTTP dump) and names the status code.
func friendlyError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(apiErr.RawJSON()), &body) == nil && body.Error.Message != "" {
			return fmt.Errorf("Claude API error (%d): %s", apiErr.StatusCode, body.Error.Message)
		}
		return fmt.Errorf("Claude API error (%d)", apiErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Claude took too long to answer")
	}
	return err
}

// TitleFor asks the cheap model for a short conversation title from the
// opening exchange, like "Alex Atlas meeting". Returns "" on any failure.
func (a *Agent) TitleFor(userMsg, reply string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     titleModel,
		MaxTokens: 30,
		System: []anthropic.TextBlockParam{{
			Text: "Write a 2-5 word title for this personal-assistant conversation. Reply with the title only: no quotes, no trailing punctuation.",
		}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(
			"User: " + truncate(userMsg, 600) + "\n\nAssistant: " + truncate(reply, 600)))},
	})
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	title := strings.Trim(strings.TrimSpace(sb.String()), `"'.`)
	if title == "" || len(title) > 60 {
		return ""
	}
	return title
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
