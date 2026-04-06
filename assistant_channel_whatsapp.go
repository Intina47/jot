package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type WhatsAppCapability struct {
	Config     AssistantConfig
	Provider   ModelProvider
	Gmail      *GmailCapability
	Adapter    AssistantChannelAdapter
	MemoryPath string
	ProgressFn func(string)
}

func NewWhatsAppCapability(cfg AssistantConfig, provider ModelProvider, gmail *GmailCapability) (*WhatsAppCapability, error) {
	adapter, err := newAssistantChannelAdapter(cfg, assistantChannelWhatsApp)
	if err != nil {
		return nil, err
	}
	return &WhatsAppCapability{
		Config:     cfg,
		Provider:   provider,
		Gmail:      gmail,
		Adapter:    adapter,
		MemoryPath: strings.TrimSpace(cfg.MemoryPath),
	}, nil
}

func (w *WhatsAppCapability) Name() string { return "whatsapp" }

func (w *WhatsAppCapability) Description() string {
	return "Read WhatsApp DM threads and prepare confirmation-gated replies using shared memory and local channel adapters."
}

func (w *WhatsAppCapability) Tools() []Tool {
	return []Tool{
		{Name: "whatsapp.status", Description: "Check whether the WhatsApp adapter is connected and ready.", ParamSchema: `{}`},
		{Name: "whatsapp.list_threads", Description: "List recent WhatsApp direct-message threads.", ParamSchema: `{"type":"object","properties":{"limit":{"type":"integer"}}}`},
		{Name: "whatsapp.read_thread", Description: "Read one WhatsApp thread with recent messages.", ParamSchema: `{"type":"object","properties":{"thread_id":{"type":"string"},"limit":{"type":"integer"}},"required":["thread_id"]}`},
		{Name: "whatsapp.draft_reply", Description: "Prepare a WhatsApp reply from a thread using memory/persona context. Set send=true only after confirmation.", ParamSchema: `{"type":"object","properties":{"thread_id":{"type":"string"},"body":{"type":"string"},"instructions":{"type":"string"},"contact":{"type":"string"},"send":{"type":"boolean"}},"required":["thread_id"]}`},
	}
}

func (w *WhatsAppCapability) SetProgressReporter(fn func(string)) {
	w.ProgressFn = fn
}

func (w *WhatsAppCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	ctx := context.Background()
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "whatsapp.status":
		status, err := w.Adapter.Status(ctx)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		text := "WhatsApp adapter not connected."
		if status.Connected {
			text = "WhatsApp adapter connected."
			if label := strings.TrimSpace(status.AccountLabel); label != "" {
				text += " Account: " + label + "."
			}
		}
		return ToolResult{Success: true, Text: text, Data: map[string]any{
			"connected": status.Connected,
			"account":   status.AccountLabel,
			"detail":    status.Detail,
		}}, nil
	case "whatsapp.list_threads":
		limit := assistantClampIntParam(params, 10, 1, 30)
		if w.ProgressFn != nil {
			w.ProgressFn("reading WhatsApp...")
		}
		threads, err := w.Adapter.ListThreads(ctx, limit)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{Success: true, Text: fmt.Sprintf("Found %d WhatsApp thread(s).", len(threads)), Data: threads}, nil
	case "whatsapp.read_thread":
		threadID := strings.TrimSpace(firstStringParam(params, "thread_id", "id"))
		if threadID == "" {
			err := fmt.Errorf("thread_id is required")
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if w.ProgressFn != nil {
			w.ProgressFn("reading WhatsApp thread...")
		}
		thread, err := w.Adapter.ReadThread(ctx, threadID, assistantClampIntParam(params, 20, 1, 60))
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{Success: true, Text: "Read WhatsApp thread.", Data: thread}, nil
	case "whatsapp.draft_reply":
		return w.executeDraftReply(ctx, params)
	default:
		err := fmt.Errorf("unknown tool %q", toolName)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
}

func (w *WhatsAppCapability) executeDraftReply(ctx context.Context, params map[string]any) (ToolResult, error) {
	threadID := strings.TrimSpace(firstStringParam(params, "thread_id", "id"))
	if threadID == "" {
		err := fmt.Errorf("thread_id is required")
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if w.ProgressFn != nil {
		w.ProgressFn("reading WhatsApp thread...")
	}
	thread, err := w.Adapter.ReadThread(ctx, threadID, 24)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	body := strings.TrimSpace(firstStringParam(params, "body"))
	instructions := strings.TrimSpace(firstStringParam(params, "instructions"))
	if body == "" {
		if w.ProgressFn != nil {
			w.ProgressFn("drafting WhatsApp reply...")
		}
		body, err = w.generateReplyDraft(thread, assistantDefaultString(firstStringParam(params, "contact"), thread.ContactLabel), instructions)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
	}
	send := paramBool(params, "send", "deliver")
	if send {
		result, err := w.Adapter.SendMessage(ctx, threadID, body)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		return ToolResult{Success: true, Text: "WhatsApp reply sent.", Data: map[string]any{
			"sent":         true,
			"thread_id":    threadID,
			"contact":      assistantDefaultString(thread.ContactLabel, firstStringParam(params, "contact")),
			"preview":      body,
			"body":         body,
			"send_result":  result,
			"send_allowed": true,
			"channel":      "whatsapp",
		}}, nil
	}
	return ToolResult{Success: true, Text: "WhatsApp reply drafted.", Data: map[string]any{
		"sent":         false,
		"thread_id":    threadID,
		"contact":      assistantDefaultString(thread.ContactLabel, firstStringParam(params, "contact")),
		"preview":      body,
		"body":         body,
		"send_allowed": true,
		"channel":      "whatsapp",
	}}, nil
}

func (w *WhatsAppCapability) generateReplyDraft(thread AssistantChannelThread, contact string, instructions string) (string, error) {
	if w.Provider == nil {
		return "", ErrAssistantNoProvider
	}
	memory, _ := LoadAssistantMemoryAt(w.MemoryPath)
	payload, err := json.Marshal(map[string]any{
		"channel":          "whatsapp",
		"contact":          strings.TrimSpace(contact),
		"instructions":     strings.TrimSpace(instructions),
		"thread":           assistantChannelThreadPromptInput(thread),
		"memory":           assistantMemoryPromptFacts(memory, contact, 14),
		"currentSituation": assistantMemoryCurrentSituation(memory),
	})
	if err != nil {
		return "", err
	}
	resp, err := w.Provider.Chat([]Message{
		{Role: "system", Content: strings.TrimSpace(`You draft a WhatsApp reply for Ntina.
Return exactly one JSON object and nothing else.
Schema:
{"body":"reply text only","importance":"low|medium|high","shouldEscalate":true|false,"escalationSummary":"brief summary"}
Rules:
- Sound like Ntina, not a corporate bot.
- Use memory only when it is relevant to the conversation.
- Keep the reply natural and concise.
- Do not mention being an assistant unless the instructions explicitly say to.
- If the latest message is casual and low-stakes, match the tone naturally.
- If the conversation involves money, conflict, logistics, official matters, or uncertainty, set shouldEscalate=true.`)},
		{Role: "user", Content: string(payload)},
	}, nil)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(strings.TrimSpace(resp))), &parsed); err != nil {
		return "", err
	}
	parsed.Body = strings.TrimSpace(parsed.Body)
	if parsed.Body == "" {
		return "", fmt.Errorf("provider returned an empty WhatsApp draft")
	}
	return parsed.Body, nil
}

func assistantChannelThreadPromptInput(thread AssistantChannelThread) map[string]any {
	items := make([]map[string]any, 0, len(thread.Messages))
	for _, msg := range thread.Messages {
		items = append(items, map[string]any{
			"id":         strings.TrimSpace(msg.ID),
			"sender":     strings.TrimSpace(msg.SenderLabel),
			"senderId":   strings.TrimSpace(msg.SenderID),
			"text":       truncateForPrompt(strings.TrimSpace(msg.Text), 800),
			"sentBySelf": msg.SentBySelf,
			"timestamp":  msg.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	return map[string]any{
		"threadId":     strings.TrimSpace(thread.ThreadID),
		"contactId":    strings.TrimSpace(thread.ContactID),
		"contactLabel": strings.TrimSpace(thread.ContactLabel),
		"snippet":      truncateForPrompt(strings.TrimSpace(thread.Snippet), 240),
		"messages":     items,
	}
}

func assistantMemoryPromptFacts(memory *AssistantMemory, contact string, limit int) []map[string]any {
	if memory == nil {
		return nil
	}
	contactID := memory.ResolveContact(strings.TrimSpace(contact))
	out := make([]map[string]any, 0)
	for _, fact := range memory.BestFacts() {
		if fact.ContactID != "" && fact.ContactID != contactID {
			continue
		}
		out = append(out, map[string]any{
			"contactId":    fact.ContactID,
			"key":          fact.Key,
			"value":        truncateForPrompt(fact.Value, 180),
			"sourceType":   fact.SourceType,
			"confidence":   fact.Confidence,
			"verification": fact.Verification,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func assistantMemoryCurrentSituation(memory *AssistantMemory) string {
	if memory == nil {
		return ""
	}
	var lines []string
	for _, key := range []string{"current_situation", "upcoming_event", "availability", "training_start"} {
		for _, fact := range memory.FactsByKey(key) {
			if fact.ContactID != "" {
				continue
			}
			lines = append(lines, fact.Key+": "+fact.Value)
		}
	}
	return strings.Join(lines, "; ")
}

func assistantClampIntParam(params map[string]any, fallback, minValue, maxValue int) int {
	value := fallback
	if n := assistantIntValue(params["limit"]); n > 0 {
		value = n
	}
	if value < minValue {
		value = minValue
	}
	if value > maxValue {
		value = maxValue
	}
	return value
}
