package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

type whatsappAdapterStub struct {
	status     AssistantChannelAdapterStatus
	threads    []AssistantChannelThread
	thread     AssistantChannelThread
	sentThread string
	sentBody   string
}

func (w *whatsappAdapterStub) Channel() string { return "whatsapp" }
func (w *whatsappAdapterStub) Status(ctx context.Context) (AssistantChannelAdapterStatus, error) {
	return w.status, nil
}
func (w *whatsappAdapterStub) ListThreads(ctx context.Context, limit int) ([]AssistantChannelThread, error) {
	return w.threads, nil
}
func (w *whatsappAdapterStub) ReadThread(ctx context.Context, threadID string, limit int) (AssistantChannelThread, error) {
	return w.thread, nil
}
func (w *whatsappAdapterStub) SendMessage(ctx context.Context, threadID string, body string) (AssistantChannelSendResult, error) {
	w.sentThread = threadID
	w.sentBody = body
	return AssistantChannelSendResult{ID: "msg-1", ThreadID: threadID, SentAt: time.Now().UTC()}, nil
}

func TestBuildAssistantCapabilities_SkipsWhatsAppWithoutBridge(t *testing.T) {
	cfg := AssistantConfig{
		Provider: "ollama",
		Model:    "test-model",
		Channels: map[string]AssistantChannelConfig{
			assistantChannelWhatsApp: {
				Kind:      assistantChannelWhatsApp,
				Enabled:   true,
				Connected: true,
			},
		},
	}
	caps, err := buildAssistantCapabilities(cfg, "", &sequentialTestProvider{})
	if err != nil {
		t.Fatalf("buildAssistantCapabilities returned error: %v", err)
	}
	for _, cap := range caps {
		if cap.Name() == "whatsapp" {
			t.Fatal("expected whatsapp capability to be skipped when native bridge is not configured")
		}
	}
}

func TestWhatsAppDraftReplyUsesMemoryAndDoesNotSendByDefault(t *testing.T) {
	tmp := t.TempDir()
	memory := NewAssistantMemoryAt(tmp + "/assistant_memory.json")
	if _, err := memory.LinkContactAlias("palma", "Palma"); err != nil {
		t.Fatalf("LinkContactAlias returned error: %v", err)
	}
	if _, err := memory.AddObservation(MemoryObservation{
		ContactID:    "palma",
		ContactAlias: "Palma",
		Key:          "relationship",
		Value:        "friendly and informal",
		SourceType:   "user",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
	}); err != nil {
		t.Fatalf("AddObservation returned error: %v", err)
	}
	provider := &sequentialTestProvider{responses: []string{
		`{"body":"hey palma, yes i'm in. sounds good.","importance":"low","shouldEscalate":false,"escalationSummary":""}`,
	}}
	adapter := &whatsappAdapterStub{
		thread: AssistantChannelThread{
			ThreadID:     "thread-1",
			ContactID:    "palma",
			ContactLabel: "Palma",
			Messages: []AssistantChannelMessage{
				{ID: "m1", SenderLabel: "Palma", Text: "hey are you coming?", Timestamp: time.Now().Add(-5 * time.Minute)},
			},
		},
	}
	cap := &WhatsAppCapability{
		Config:     AssistantConfig{},
		Provider:   provider,
		Adapter:    adapter,
		MemoryPath: tmp + "/assistant_memory.json",
	}
	result, err := cap.Execute("whatsapp.draft_reply", map[string]any{"thread_id": "thread-1"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", result.Data)
	}
	if assistantBoolValue(data["sent"]) {
		t.Fatal("expected draft reply not to send by default")
	}
	if got := assistantStringValue(data["preview"]); !strings.Contains(got, "yes i'm in") {
		t.Fatalf("unexpected preview: %q", got)
	}
	if adapter.sentBody != "" {
		t.Fatalf("expected adapter not to send, got %q", adapter.sentBody)
	}
}

func TestWhatsAppDraftReplyCanSendAfterConfirmation(t *testing.T) {
	adapter := &whatsappAdapterStub{
		thread: AssistantChannelThread{
			ThreadID:     "thread-1",
			ContactLabel: "Palma",
		},
	}
	cap := &WhatsAppCapability{
		Provider: &sequentialTestProvider{},
		Adapter:  adapter,
	}
	result, err := cap.Execute("whatsapp.draft_reply", map[string]any{
		"thread_id": "thread-1",
		"body":      "sounds good, see you there",
		"send":      true,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}
	if adapter.sentThread != "thread-1" {
		t.Fatalf("expected thread-1, got %q", adapter.sentThread)
	}
	if adapter.sentBody != "sounds good, see you there" {
		t.Fatalf("unexpected sent body: %q", adapter.sentBody)
	}
}
