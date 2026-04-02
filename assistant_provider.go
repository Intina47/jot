package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelProvider is the provider-agnostic LLM interface used by the assistant
// runtime. The runtime owns prompting and tool execution; providers only supply
// model responses and availability checks.
type ModelProvider interface {
	Name() string
	Chat(messages []Message, tools []Tool) (string, error)
	IsAvailable() (bool, error)
}

type StreamingModelProvider interface {
	ChatStream(messages []Message, tools []Tool, onDelta func(string) error) (string, error)
}

// OllamaProvider talks to the local Ollama HTTP API.
type OllamaProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) IsAvailable() (bool, error) {
	client := &http.Client{Timeout: p.timeoutOrDefault(3 * time.Second)}
	req, err := http.NewRequest(http.MethodGet, p.baseURL()+"/api/tags", nil)
	if err != nil {
		return false, err
	}
	p.applyAuth(req)

	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}
	return true, nil
}

func (p *OllamaProvider) Chat(messages []Message, tools []Tool) (string, error) {
	return p.chat(messages, tools, nil)
}

func (p *OllamaProvider) ChatStream(messages []Message, tools []Tool, onDelta func(string) error) (string, error) {
	return p.chat(messages, tools, onDelta)
}

func (p *OllamaProvider) chat(messages []Message, tools []Tool, onDelta func(string) error) (string, error) {
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = defaultOllamaModel
	}

	requestMessages := make([]ollamaChatMessage, 0, len(messages)+1)
	requestMessages = append(requestMessages, ollamaChatMessage{
		Role:    "system",
		Content: ollamaSystemPrompt(tools),
	})
	for _, message := range messages {
		role := normalizeChatRole(message.Role)
		if role == "" {
			role = "user"
		}
		requestMessages = append(requestMessages, ollamaChatMessage{
			Role:    role,
			Content: message.Content,
		})
	}

	payload := ollamaChatRequest{
		Model:    model,
		Stream:   true,
		Messages: requestMessages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: p.timeoutOrDefault(2 * time.Minute)}
	req, err := http.NewRequest(http.MethodPost, p.baseURL()+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	p.applyAuth(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if len(data) == 0 {
			return "", fmt.Errorf("ollama chat request failed: %s", resp.Status)
		}
		return "", fmt.Errorf("ollama chat request failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	return readOllamaChatResponse(resp.Body, onDelta)
}

func (p *OllamaProvider) baseURL() string {
	baseURL := strings.TrimSpace(p.BaseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return strings.TrimRight(baseURL, "/")
}

func (p *OllamaProvider) timeoutOrDefault(defaultTimeout time.Duration) time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultTimeout
}

func (p *OllamaProvider) applyAuth(req *http.Request) {
	if req == nil {
		return
	}
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

const defaultOllamaModel = "llama3.2"

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Stream   bool                `json:"stream"`
	Messages []ollamaChatMessage `json:"messages"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message  ollamaChatMessage `json:"message"`
	Response string            `json:"response"`
	Done     bool              `json:"done"`
	Error    string            `json:"error"`
}

func (r ollamaChatResponse) content() string {
	if strings.TrimSpace(r.Message.Content) != "" {
		return r.Message.Content
	}
	return r.Response
}

func (r ollamaChatResponse) responseError() error {
	if strings.TrimSpace(r.Error) == "" {
		return nil
	}
	return errors.New(r.Error)
}

func parseOllamaStream(data []byte) (string, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", true, nil
	}

	if text, ok, err := parseOllamaStreamLines(data); err != nil {
		return "", false, err
	} else if ok {
		return text, true, nil
	}

	return parseOllamaStreamDecoder(data)
}

func parseOllamaStreamLines(data []byte) (string, bool, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var parts []string
	var sawChunk bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if line == "" || line == "[DONE]" {
				continue
			}
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return "", false, nil
		}
		if err := chunk.responseError(); err != nil {
			return "", false, err
		}

		content := chunk.content()
		if content != "" {
			parts = append(parts, content)
		}
		sawChunk = true
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	if !sawChunk {
		return "", false, nil
	}
	return strings.Join(parts, ""), true, nil
}

func parseOllamaStreamDecoder(data []byte) (string, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var parts []string
	var sawChunk bool

	for {
		var chunk ollamaChatResponse
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", false, err
		}
		if err := chunk.responseError(); err != nil {
			return "", false, err
		}
		content := chunk.content()
		if content != "" {
			parts = append(parts, content)
		}
		sawChunk = true
	}

	if !sawChunk {
		return "", false, nil
	}
	return strings.Join(parts, ""), true, nil
}

func readOllamaChatResponse(body io.Reader, onDelta func(string) error) (string, error) {
	if body == nil {
		return "", errors.New("ollama chat response body is nil")
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var parts []string
	var sawChunk bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if line == "" || line == "[DONE]" {
				continue
			}
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return readOllamaChatFallback(line, scanner, onDelta)
		}
		if err := chunk.responseError(); err != nil {
			return "", err
		}

		content := chunk.content()
		if content != "" {
			parts = append(parts, content)
			if onDelta != nil {
				if err := onDelta(content); err != nil {
					return "", err
				}
			}
		}
		sawChunk = true
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if sawChunk {
		return strings.Join(parts, ""), nil
	}
	return "", errors.New("unable to parse ollama chat response")
}

func readOllamaChatFallback(firstLine string, scanner *bufio.Scanner, onDelta func(string) error) (string, error) {
	var data bytes.Buffer
	data.WriteString(firstLine)
	for scanner.Scan() {
		data.WriteByte('\n')
		data.WriteString(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	raw := data.Bytes()
	if text, ok, err := parseOllamaStream(raw); err != nil {
		return "", err
	} else if ok {
		if onDelta != nil && text != "" {
			if err := onDelta(text); err != nil {
				return "", err
			}
		}
		return text, nil
	}

	var single ollamaChatResponse
	if err := json.Unmarshal(raw, &single); err == nil {
		if err := single.responseError(); err != nil {
			return "", err
		}
		text := single.content()
		if onDelta != nil && text != "" {
			if err := onDelta(text); err != nil {
				return "", err
			}
		}
		return text, nil
	}

	return "", errors.New("unable to parse ollama chat response")
}

func normalizeChatRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "user", "assistant", "tool":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "user"
	}
}

func ollamaSystemPrompt(tools []Tool) string {
	var b strings.Builder
	b.WriteString("You are Jot Assistant, a CLI-native agent that reads, reasons, and acts with the user's control.\n")
	b.WriteString("Return plain text unless you need a tool.\n")
	b.WriteString("To call a tool, emit exactly this format and nothing else:\n")
	b.WriteString("TOOL: <capability.tool_name>\n")
	b.WriteString("PARAMS: {\"query\":\"is:unread newer_than:1d\",\"max\":10}\n")
	b.WriteString("Do not wrap tool calls in markdown. Do not add commentary around tool directives.\n")
	b.WriteString("PARAMS must be a plain JSON object. Never put JSON inside a `json` string field.\n")
	b.WriteString("Always place TOOL and PARAMS on separate lines.\n")
	b.WriteString("Use at most one tool call per response.\n")
	if len(tools) > 0 {
		b.WriteString("\nAvailable tools:\n")
		for _, tool := range tools {
			b.WriteString("- ")
			b.WriteString(tool.Name)
			if tool.Description != "" {
				b.WriteString(": ")
				b.WriteString(tool.Description)
			}
			if strings.TrimSpace(tool.ParamSchema) != "" {
				b.WriteString(" | schema: ")
				b.WriteString(tool.ParamSchema)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// OpenAIProvider and AnthropicProvider are scaffolds for future provider work.
// Their Chat implementations intentionally return a not-implemented error.
type OpenAIProvider struct {
	APIKey string
	Model  string
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Chat(messages []Message, tools []Tool) (string, error) {
	return "", errors.New("OpenAI provider not yet implemented")
}

func (p *OpenAIProvider) IsAvailable() (bool, error) {
	return strings.TrimSpace(p.APIKey) != "", nil
}

type AnthropicProvider struct {
	APIKey string
	Model  string
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Chat(messages []Message, tools []Tool) (string, error) {
	return "", errors.New("Anthropic provider not yet implemented")
}

func (p *AnthropicProvider) IsAvailable() (bool, error) {
	return strings.TrimSpace(p.APIKey) != "", nil
}

func NewModelProvider(config AssistantConfig) (ModelProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	switch provider {
	case "", "ollama":
		model := strings.TrimSpace(config.Model)
		if model == "" {
			model = defaultOllamaModel
		}
		baseURL := strings.TrimSpace(config.OllamaURL)
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return &OllamaProvider{
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(config.OllamaAPIKey),
			Model:   model,
		}, nil
	case "openai":
		return &OpenAIProvider{
			APIKey: strings.TrimSpace(config.OpenAIKey),
			Model:  strings.TrimSpace(config.Model),
		}, nil
	case "anthropic":
		return &AnthropicProvider{
			APIKey: strings.TrimSpace(config.AnthropicKey),
			Model:  strings.TrimSpace(config.Model),
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q", config.Provider)
	}
}
