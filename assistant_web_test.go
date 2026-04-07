package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebCapabilitySearchReturnsStructuredResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "army basic training" {
			t.Fatalf("unexpected query: %q", got)
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Bing</title>
    <link>https://www.bing.com</link>
    <description>Search feed</description>
    <item>
      <title>Army training guide</title>
      <link>https://example.com/guide</link>
      <description><![CDATA[<p>Ten weeks of training.</p>]]></description>
      <pubDate>Mon, 07 Apr 2026 10:00:00 +0000</pubDate>
    </item>
    <item>
      <title>Preparing for basic training</title>
      <link>https://example.com/prep</link>
      <description><![CDATA[<div>What to pack and expect.</div>]]></description>
      <pubDate>Mon, 07 Apr 2026 11:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`)
	}))
	defer server.Close()

	capability := &WebCapability{
		BaseURL:   server.URL,
		Client:    server.Client(),
		UserAgent: "jot-test-agent",
	}
	result, err := capability.Execute("web.search", map[string]any{
		"query":       "army basic training",
		"max_results": 2,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", result.Data)
	}
	if got := data["query"]; got != "army basic training" {
		t.Fatalf("unexpected query data: %#v", got)
	}
	if got := data["source"]; got != "Bing RSS" {
		t.Fatalf("unexpected source: %#v", got)
	}
	if got := result.Text; !strings.Contains(got, "Top results") {
		t.Fatalf("expected summary text to mention top results, got %q", got)
	}
	results, ok := data["results"].([]map[string]any)
	if !ok {
		t.Fatalf("expected typed results, got %T", data["results"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if got := results[0]["title"]; got != "Army training guide" {
		t.Fatalf("unexpected first result title: %#v", got)
	}
	if got := results[0]["url"]; got != "https://example.com/guide" {
		t.Fatalf("unexpected first result url: %#v", got)
	}
	if got := results[0]["source"]; got != "Bing RSS" {
		t.Fatalf("unexpected first result source: %#v", got)
	}
	if got := results[0]["snippet"]; got != "Ten weeks of training." {
		t.Fatalf("unexpected first result snippet: %#v", got)
	}
	if got := results[0]["publishedAt"]; got == "" {
		t.Fatal("expected publishedAt to be populated")
	}
}

func TestWebCapabilityFetchExtractsReadableText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
  <head>
    <title>Basic Training Prep</title>
    <style>body { color: red; }</style>
    <script>var secret = 42;</script>
  </head>
  <body>
    <h1>Prep Checklist</h1>
    <p>Read the welcome packet.</p>
    <div>Pack the right gear.</div>
  </body>
</html>`)
	}))
	defer server.Close()

	capability := &WebCapability{
		Client:    server.Client(),
		UserAgent: "jot-test-agent",
	}
	result, err := capability.Execute("web.fetch", map[string]any{
		"url":       server.URL,
		"max_chars": 2000,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", result.Data)
	}
	if got := data["title"]; got != "Basic Training Prep" {
		t.Fatalf("unexpected title: %#v", got)
	}
	text := fmt.Sprint(data["text"])
	if !strings.Contains(text, "Prep Checklist") || !strings.Contains(text, "Read the welcome packet.") {
		t.Fatalf("expected visible page text, got %q", text)
	}
	if strings.Contains(text, "secret = 42") || strings.Contains(text, "color: red") {
		t.Fatalf("expected script/style content to be stripped, got %q", text)
	}
	if got := data["sourceType"]; got != "web_page" {
		t.Fatalf("unexpected sourceType: %#v", got)
	}
	if got := data["statusCode"]; got != 200 {
		t.Fatalf("unexpected status code: %#v", got)
	}
}

func TestBuildAssistantCapabilities_IncludesWebCapability(t *testing.T) {
	caps, err := buildAssistantCapabilities(AssistantConfig{
		Provider:          "ollama",
		Model:             "llama3.2",
		OllamaURL:         "http://localhost:11434",
		GmailTokenPath:    filepath.Join(t.TempDir(), "gmail_token.json"),
		GmailCredPath:     filepath.Join(t.TempDir(), "gmail_credentials.json"),
		MemoryPath:        filepath.Join(t.TempDir(), "assistant_memory.json"),
		AttachmentSaveDir: filepath.Join(t.TempDir(), "attachments"),
	}, "web", &sequentialTestProvider{})
	if err != nil {
		t.Fatalf("buildAssistantCapabilities returned error: %v", err)
	}
	found := false
	for _, cap := range caps {
		if _, ok := cap.(*WebCapability); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected web capability to be included")
	}
}
