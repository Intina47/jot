package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type AssistantChannelMessage struct {
	ID          string    `json:"id"`
	ThreadID    string    `json:"threadId,omitempty"`
	SenderID    string    `json:"senderId,omitempty"`
	SenderLabel string    `json:"senderLabel,omitempty"`
	Text        string    `json:"text,omitempty"`
	SentBySelf  bool      `json:"sentBySelf,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
}

type AssistantChannelThread struct {
	ThreadID      string                    `json:"threadId"`
	ContactID     string                    `json:"contactId,omitempty"`
	ContactLabel  string                    `json:"contactLabel,omitempty"`
	Snippet       string                    `json:"snippet,omitempty"`
	LastMessageAt time.Time                 `json:"lastMessageAt,omitempty"`
	UnreadCount   int                       `json:"unreadCount,omitempty"`
	Messages      []AssistantChannelMessage `json:"messages,omitempty"`
}

type AssistantChannelAdapterStatus struct {
	Connected    bool   `json:"connected"`
	AccountLabel string `json:"accountLabel,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

type AssistantChannelSendResult struct {
	ID       string    `json:"id,omitempty"`
	ThreadID string    `json:"threadId,omitempty"`
	SentAt   time.Time `json:"sentAt,omitempty"`
}

type AssistantChannelAdapter interface {
	Channel() string
	Status(ctx context.Context) (AssistantChannelAdapterStatus, error)
	ListThreads(ctx context.Context, limit int) ([]AssistantChannelThread, error)
	ReadThread(ctx context.Context, threadID string, limit int) (AssistantChannelThread, error)
	SendMessage(ctx context.Context, threadID string, body string) (AssistantChannelSendResult, error)
}

type assistantChannelBridgeRequest struct {
	Channel string         `json:"channel"`
	Action  string         `json:"action"`
	Params  map[string]any `json:"params,omitempty"`
}

type assistantChannelBridgeResponse struct {
	OK      bool                          `json:"ok"`
	Error   string                        `json:"error,omitempty"`
	Status  AssistantChannelAdapterStatus `json:"status,omitempty"`
	Threads []AssistantChannelThread      `json:"threads,omitempty"`
	Thread  AssistantChannelThread        `json:"thread,omitempty"`
	Send    AssistantChannelSendResult    `json:"send,omitempty"`
}

type AssistantChannelCommandAdapter struct {
	channel        string
	command        string
	args           []string
	bridgeStateDir string
}

func newAssistantChannelAdapter(cfg AssistantConfig, channel string) (AssistantChannelAdapter, error) {
	channel = assistantNormalizeChannelName(channel)
	if channel == "" {
		return nil, fmt.Errorf("unknown channel")
	}
	settings := assistantChannelConfig(cfg, channel)
	command := strings.TrimSpace(settings.BridgeCommand)
	if command == "" {
		return nil, fmt.Errorf("%s native bridge is not configured", assistantChannelDisplayName(channel))
	}
	return &AssistantChannelCommandAdapter{
		channel:        channel,
		command:        command,
		args:           append([]string(nil), settings.BridgeArgs...),
		bridgeStateDir: strings.TrimSpace(settings.BridgeStateDir),
	}, nil
}

func (a *AssistantChannelCommandAdapter) Channel() string {
	if a == nil {
		return ""
	}
	return a.channel
}

func (a *AssistantChannelCommandAdapter) Status(ctx context.Context) (AssistantChannelAdapterStatus, error) {
	resp, err := a.call(ctx, "status", nil)
	if err != nil {
		return AssistantChannelAdapterStatus{}, err
	}
	return resp.Status, nil
}

func (a *AssistantChannelCommandAdapter) ListThreads(ctx context.Context, limit int) ([]AssistantChannelThread, error) {
	resp, err := a.call(ctx, "list_threads", map[string]any{"limit": limit})
	if err != nil {
		return nil, err
	}
	return resp.Threads, nil
}

func (a *AssistantChannelCommandAdapter) ReadThread(ctx context.Context, threadID string, limit int) (AssistantChannelThread, error) {
	resp, err := a.call(ctx, "read_thread", map[string]any{
		"thread_id": strings.TrimSpace(threadID),
		"limit":     limit,
	})
	if err != nil {
		return AssistantChannelThread{}, err
	}
	return resp.Thread, nil
}

func (a *AssistantChannelCommandAdapter) SendMessage(ctx context.Context, threadID string, body string) (AssistantChannelSendResult, error) {
	resp, err := a.call(ctx, "send_message", map[string]any{
		"thread_id": strings.TrimSpace(threadID),
		"body":      strings.TrimSpace(body),
	})
	if err != nil {
		return AssistantChannelSendResult{}, err
	}
	return resp.Send, nil
}

func (a *AssistantChannelCommandAdapter) call(ctx context.Context, action string, params map[string]any) (assistantChannelBridgeResponse, error) {
	if a == nil {
		return assistantChannelBridgeResponse{}, fmt.Errorf("channel adapter is nil")
	}
	req := assistantChannelBridgeRequest{
		Channel: a.channel,
		Action:  strings.TrimSpace(action),
		Params:  params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return assistantChannelBridgeResponse{}, err
	}
	cmd := exec.CommandContext(ctx, a.command, a.args...)
	cmd.Env = assistantChannelBridgeEnv(a.channel, a.bridgeStateDir)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText != "" {
			return assistantChannelBridgeResponse{}, fmt.Errorf("%w: %s", err, errText)
		}
		return assistantChannelBridgeResponse{}, err
	}
	var resp assistantChannelBridgeResponse
	if err := json.Unmarshal(trimUTF8BOM(stdout.Bytes()), &resp); err != nil {
		return assistantChannelBridgeResponse{}, err
	}
	if !resp.OK {
		message := strings.TrimSpace(resp.Error)
		if message == "" {
			message = "channel bridge returned an error"
		}
		return assistantChannelBridgeResponse{}, fmt.Errorf("%s", message)
	}
	return resp, nil
}

func assistantChannelBridgeEnv(channel, bridgeStateDir string) []string {
	env := append([]string(nil), os.Environ()...)
	if assistantNormalizeChannelName(channel) == assistantChannelWhatsApp {
		if stateDir := strings.TrimSpace(bridgeStateDir); stateDir != "" {
			env = append(env, "JOT_WHATSAPP_BRIDGE_DIR="+stateDir)
		}
	}
	return env
}

func assistantChannelThreadTranscript(thread AssistantChannelThread, limit int) string {
	if limit <= 0 || limit > len(thread.Messages) {
		limit = len(thread.Messages)
	}
	if limit == 0 {
		return ""
	}
	start := len(thread.Messages) - limit
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	for _, item := range thread.Messages[start:] {
		label := strings.TrimSpace(item.SenderLabel)
		if label == "" {
			if item.SentBySelf {
				label = "Ntina"
			} else {
				label = strings.TrimSpace(thread.ContactLabel)
			}
		}
		if label == "" {
			label = "Unknown"
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(item.Text))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
