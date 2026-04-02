package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewModelProvider_Ollama(t *testing.T) {
	provider, err := NewModelProvider(AssistantConfig{
		Provider:  "ollama",
		Model:     "llama3.2",
		OllamaURL: "http://localhost:11434",
	})
	if err != nil {
		t.Fatalf("NewModelProvider returned error: %v", err)
	}
	if provider.Name() != "ollama" {
		t.Fatalf("expected ollama provider, got %q", provider.Name())
	}
}

func TestNewModelProvider_UnknownProvider(t *testing.T) {
	if _, err := NewModelProvider(AssistantConfig{Provider: "bogus"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestMapNLToGmailQuery_Unread(t *testing.T) {
	if got := mapNLToGmailQuery("unread emails from today"); got != "is:unread newer_than:1d" {
		t.Fatalf("expected unread today mapping, got %q", got)
	}
}

func TestMapNLToGmailQuery_FromSender(t *testing.T) {
	if got := mapNLToGmailQuery("emails from Alice"); got != "from:alice" {
		t.Fatalf("expected sender mapping, got %q", got)
	}
}

func TestMapNLToGmailQuery_AttachmentsThisMonth(t *testing.T) {
	if got := mapNLToGmailQuery("invoice attachments this month"); got != "has:attachment invoice newer_than:30d" {
		t.Fatalf("expected attachments mapping, got %q", got)
	}
}

func TestMapNLToGmailQuery_ImportantEmails(t *testing.T) {
	if got := mapNLToGmailQuery("important emails"); got != "is:important is:unread" {
		t.Fatalf("expected important mapping, got %q", got)
	}
}

func TestAttachmentReader_PDF_CanRead(t *testing.T) {
	reader := pdfAttachmentReader{}
	if !reader.CanRead(AttachmentMeta{Filename: "report.pdf", MimeType: "application/pdf"}) {
		t.Fatal("expected pdf reader to accept pdf attachment")
	}
}

func TestAttachmentReader_CSV_ParsesRows(t *testing.T) {
	reader := csvAttachmentReader{}
	content, err := reader.Read([]byte("name,amount\nalice,10\n"), AttachmentMeta{Filename: "report.csv", MimeType: "text/csv"})
	if err != nil {
		t.Fatalf("csv reader returned error: %v", err)
	}
	if len(content.Tables) != 2 {
		t.Fatalf("expected 2 csv rows, got %d", len(content.Tables))
	}
	if content.Tables[1][0] != "alice" {
		t.Fatalf("expected second row first column to be alice, got %q", content.Tables[1][0])
	}
}

func TestAttachmentReader_JSON_PrettyPrints(t *testing.T) {
	reader := jsonAttachmentReader{}
	content, err := reader.Read([]byte(`{"b":2,"a":1}`), AttachmentMeta{Filename: "payload.json", MimeType: "application/json"})
	if err != nil {
		t.Fatalf("json reader returned error: %v", err)
	}
	if !strings.Contains(content.Text, "\n") {
		t.Fatalf("expected pretty-printed json, got %q", content.Text)
	}
	if content.Metadata["type"] != "application/json" {
		t.Fatalf("expected json metadata, got %#v", content.Metadata)
	}
}

func TestAttachmentReader_XML_ExtractsText(t *testing.T) {
	reader := xmlAttachmentReader{}
	content, err := reader.Read([]byte(`<root><item>Hello</item><item>World</item></root>`), AttachmentMeta{Filename: "note.xml", MimeType: "application/xml"})
	if err != nil {
		t.Fatalf("xml reader returned error: %v", err)
	}
	if !strings.Contains(content.Text, "Hello") || !strings.Contains(content.Text, "World") {
		t.Fatalf("expected xml text extraction, got %q", content.Text)
	}
}

func TestAttachmentReader_Docx_ExtractsText(t *testing.T) {
	reader := docxAttachmentReader{}
	data := buildTestDOCX(t, `<w:document><w:body><w:p><w:r><w:t>Hello world</w:t></w:r></w:p></w:body></w:document>`)
	content, err := reader.Read(data, AttachmentMeta{Filename: "note.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"})
	if err != nil {
		t.Fatalf("docx reader returned error: %v", err)
	}
	if !strings.Contains(content.Text, "Hello world") {
		t.Fatalf("expected docx text extraction, got %q", content.Text)
	}
}

func TestAttachmentReader_XLSX_ExtractsRows(t *testing.T) {
	reader := xlsxAttachmentReader{}
	data := buildTestXLSX(t)
	content, err := reader.Read(data, AttachmentMeta{Filename: "sheet.xlsx", MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"})
	if err != nil {
		t.Fatalf("xlsx reader returned error: %v", err)
	}
	if len(content.Tables) != 2 {
		t.Fatalf("expected 2 xlsx rows, got %d", len(content.Tables))
	}
	if content.Tables[0][0] != "Name" || content.Tables[1][0] != "Alice" {
		t.Fatalf("unexpected xlsx rows: %#v", content.Tables)
	}
}

func TestAttachmentReader_Unknown_ReturnsFalse(t *testing.T) {
	readers := DefaultAttachmentReaders()
	meta := AttachmentMeta{Filename: "archive.bin", MimeType: "application/octet-stream"}
	for _, reader := range readers {
		if reader.CanRead(meta) {
			t.Fatalf("unexpected reader matched unknown attachment: %#v", reader)
		}
	}
}

func TestExtractActions_FindsDeadlines(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	actions := ExtractActionsAt("Please finish the report by tomorrow.", now)
	if len(actions.Deadlines) == 0 {
		t.Fatal("expected at least one deadline")
	}
}

func TestExtractActions_FindsMeetingRequest(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	actions := ExtractActionsAt("Can we schedule a sync tomorrow at 3pm with Alice in Zoom?", now)
	if len(actions.MeetingReqs) == 0 {
		t.Fatal("expected at least one meeting request")
	}
}

func TestExtractActions_FindsActionItems(t *testing.T) {
	actions := ExtractActionsAt("- follow up with finance\nPlease send the updated budget.", time.Now())
	if len(actions.ActionItems) == 0 {
		t.Fatal("expected at least one action item")
	}
}

func TestExtractActions_IgnoresBoilerplateAndNewsletterCopy(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	text := strings.Join([]string{
		"unsubscribe from this newsletter",
		"If this was you, you don't need to do anything.",
		"Privacy Policy: https://example.com/privacy",
		"Built-in auth and full-stack web apps in minutes.",
	}, "\n")
	actions := ExtractActionsAt(text, now)
	if len(actions.ActionItems) != 0 {
		t.Fatalf("expected no action items from boilerplate, got %#v", actions.ActionItems)
	}
	if len(actions.Deadlines) != 0 {
		t.Fatalf("expected no deadlines from boilerplate, got %#v", actions.Deadlines)
	}
}

func TestExtractActions_PreservesShortImperativeRequest(t *testing.T) {
	now := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
	actions := ExtractActionsAt("Please send the updated budget by tomorrow.", now)
	if len(actions.ActionItems) == 0 {
		t.Fatal("expected imperative request to remain an action item")
	}
	if len(actions.Deadlines) == 0 {
		t.Fatal("expected deadline to still be detected")
	}
}

func TestAssistantFallbackSemanticSummary_UsesSubjectsNotBoilerplate(t *testing.T) {
	summary := assistantFallbackSemanticSummary("unread emails from today", []NormalizedEmail{
		{
			From:    `"Google" <no-reply@accounts.google.com>`,
			Subject: "Security alert",
		},
		{
			From:    `"Palma Codes" <palma@example.com>`,
			Subject: "Meeting",
		},
	})
	if !strings.Contains(summary.Summary, "2 unread emails from today.") {
		t.Fatalf("unexpected fallback summary: %#v", summary)
	}
	if !strings.Contains(summary.Summary, "Google") || !strings.Contains(summary.Summary, "Meeting") {
		t.Fatalf("expected fallback summary to mention top subjects, got %#v", summary)
	}
}

func TestAssistantSemanticActionsToViews_MapsPriority(t *testing.T) {
	views := assistantSemanticActionsToViews([]AssistantSemanticAction{{
		Title:    "Book weekly meeting",
		Detail:   "Palma asked for a Saturday 10pm to 11pm invite.",
		Priority: "high",
		Kind:     "meeting",
	}})
	if len(views) != 1 {
		t.Fatalf("expected 1 action view, got %d", len(views))
	}
	if views[0].Status != "high priority" {
		t.Fatalf("expected high priority label, got %#v", views[0])
	}
}

func TestRenderAssistantTurn_TextUsesStructuredCards(t *testing.T) {
	now := time.Date(2026, time.April, 2, 13, 0, 0, 0, time.UTC)
	result := &AssistantTurnResult{
		Input:     "summarize my unread emails from today",
		FinalText: "Alice is asking to reschedule the auth refactor review. Ben moved standup to 3pm.",
		ToolCalls: []AssistantToolCall{{
			Tool:   "gmail.search",
			Params: map[string]any{"query": "is:unread newer_than:1d", "max": 10},
		}},
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{
				Tool:   "gmail.search",
				Params: map[string]any{"query": "is:unread newer_than:1d", "max": 10},
			},
			Result: ToolResult{
				Success: true,
				Data: []NormalizedEmail{
					{
						ID:      "msg-1",
						From:    `"Alice Chen" <alice@example.com>`,
						Subject: "Re: auth refactor — can we push to Friday?",
						Snippet: "Can we move the review to Friday?",
						Date:    now,
						Unread:  true,
					},
					{
						ID:      "msg-2",
						From:    `"Ben K." <ben@example.com>`,
						Subject: "standup moved to 3pm",
						Snippet: "calendar invite sent",
						Date:    now.Add(-20 * time.Minute),
						Unread:  true,
					},
				},
			},
		}},
	}

	rendered, err := RenderAssistantTurn(result.Input, result, nil, "text", now)
	if err != nil {
		t.Fatalf("RenderAssistantTurn returned error: %v", err)
	}
	if !strings.Contains(rendered, "reading Gmail - searching unread, today...") {
		t.Fatalf("expected Gmail status line, got %q", rendered)
	}
	if !strings.Contains(rendered, "Gmail · 2 unread") {
		t.Fatalf("expected unread card heading, got %q", rendered)
	}
	if !strings.Contains(rendered, "Alice is asking to reschedule the auth refactor review. Ben moved standup to 3pm.") {
		t.Fatalf("expected final summary text, got %q", rendered)
	}
}

func TestAssistantSupplementalFinalTextCard_AttachmentSavePrompt(t *testing.T) {
	card, ok := assistantSupplementalFinalTextCard(`I found 3 attachments. Reply "all" to download everything to ~/invoices/2026-03, or say save to ./dir.`)
	if !ok {
		t.Fatal("expected attachment save prompt card")
	}
	if card.Kind != "attachment-save" {
		t.Fatalf("unexpected card kind: %#v", card)
	}
	if card.Body != "save to ~/invoices/2026-03?" {
		t.Fatalf("unexpected card body: %#v", card)
	}
	if len(card.Buttons) != 2 {
		t.Fatalf("expected save/skip buttons, got %#v", card.Buttons)
	}
}

func TestRunTurn_EmitsLiveStatusWithoutRepeatingInFinalRender(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"from:*.mod.gov.uk\",\"max\":10}",
		"Here is the summary.",
	}}
	capability := fakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.search"}},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			return ToolResult{Success: true, Data: []NormalizedEmail{{
				ID:      "msg-1",
				From:    `"RAF" <test@mod.gov.uk>`,
				Subject: "Instructions",
				Snippet: "Read this first.",
				Date:    time.Date(2026, time.April, 2, 12, 13, 0, 0, time.UTC),
			}}}, nil
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var progress bytes.Buffer
	result, err := session.RunTurn(context.Background(), "read all the attachments from **.mod.gov.uk and give me a summary of what i should know", strings.NewReader(""), &progress, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !result.LiveStatus {
		t.Fatalf("expected turn to record live status emission, got %#v", result)
	}
	if !strings.Contains(progress.String(), "reading Gmail - from:*.mod.gov.uk...") {
		t.Fatalf("expected live progress output, got %q", progress.String())
	}

	rendered, err := RenderAssistantTurn(result.Input, result, nil, "text", time.Now())
	if err != nil {
		t.Fatalf("RenderAssistantTurn returned error: %v", err)
	}
	if strings.Contains(rendered, "reading Gmail - from:*.mod.gov.uk...") {
		t.Fatalf("expected final render to omit already-emitted status line, got %q", rendered)
	}
}

func TestRunTurn_EmitsAttachmentFileProgress(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.read_attachment\nPARAMS: {\"message_id\":\"msg-1\",\"read_all\":true}",
		"Done.",
	}}
	capability := &progressFakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.read_attachment"}},
		execute: func(report func(string), toolName string, params map[string]any) (ToolResult, error) {
			report("reading attachment 1/2: Annex B NEW REGISTRATION LETTER.docx...")
			report("✓ finished attachment 1/2: Annex B NEW REGISTRATION LETTER.docx")
			report("reading attachment 2/2: DGW.DLE Portal setup. 1.docx...")
			report("✓ finished attachment 2/2: DGW.DLE Portal setup. 1.docx")
			return ToolResult{Success: true, Data: map[string]any{"attachments": []string{"a", "b"}}, Text: "read 2 attachment(s)"}, nil
		},
	}

	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var progress bytes.Buffer
	if _, err := session.RunTurn(context.Background(), "read the attachments", strings.NewReader(""), &progress, time.Now); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	log := progress.String()
	if !strings.Contains(log, "reading attachment 1/2: Annex B NEW REGISTRATION LETTER.docx...") {
		t.Fatalf("expected first attachment progress, got %q", log)
	}
	if !strings.Contains(log, "✓ finished attachment 1/2: Annex B NEW REGISTRATION LETTER.docx") {
		t.Fatalf("expected first attachment completion, got %q", log)
	}
	if !strings.Contains(log, "reading attachment 2/2: DGW.DLE Portal setup. 1.docx...") {
		t.Fatalf("expected second attachment progress, got %q", log)
	}
	if !strings.Contains(log, "✓ finished attachment 2/2: DGW.DLE Portal setup. 1.docx") {
		t.Fatalf("expected second attachment completion, got %q", log)
	}
}

func TestRunTurn_StreamsFinalResponseWithoutRepeatingInFinalRender(t *testing.T) {
	provider := &streamingTestProvider{responses: []string{
		"Here is a streamed answer.",
	}}
	session := NewAssistantSession(provider, []Capability{fakeAssistantCapability{name: "gmail", tools: []Tool{{Name: "gmail.search"}}}}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "say hi", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !result.StreamedFinal {
		t.Fatalf("expected streamed final response, got %#v", result)
	}
	if !strings.Contains(out.String(), "Here is a streamed answer.") {
		t.Fatalf("expected streamed answer in live output, got %q", out.String())
	}

	rendered, err := RenderAssistantTurn(result.Input, result, nil, "text", time.Now())
	if err != nil {
		t.Fatalf("RenderAssistantTurn returned error: %v", err)
	}
	if strings.TrimSpace(rendered) != "" {
		t.Fatalf("expected final render to suppress already-streamed plain text, got %q", rendered)
	}
}

func TestRunTurn_DoesNotStreamToolDirective(t *testing.T) {
	provider := &streamingTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"is:unread newer_than:1d\",\"max\":10}",
		"Done reading mail.",
	}}
	capability := fakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.search"}},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			return ToolResult{Success: true, Data: []NormalizedEmail{{ID: "msg-1", Subject: "Hi"}}}, nil
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "show unread", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if strings.Contains(out.String(), "TOOL: gmail.search") {
		t.Fatalf("expected tool directive to stay hidden, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Done reading mail.") {
		t.Fatalf("expected final answer to stream, got %q", out.String())
	}
	if !result.StreamedFinal {
		t.Fatalf("expected final answer to be marked streamed, got %#v", result)
	}
}

func TestRunTurn_StreamsStyledMarkdownPatches(t *testing.T) {
	provider := &streamingTestProvider{responses: []string{
		"# **Heading**\n- **Important item**\n\nThis is the summary.",
	}}
	session := NewAssistantSession(provider, []Capability{fakeAssistantCapability{name: "gmail", tools: []Tool{{Name: "gmail.search"}}}}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "summarize this", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	streamed := out.String()
	if strings.Contains(streamed, "# **Heading**") || strings.Contains(streamed, "**Important item**") {
		t.Fatalf("expected streamed output to be styled, not raw markdown, got %q", streamed)
	}
	for _, want := range []string{"Heading", "Important item", "This is the summary."} {
		if !strings.Contains(streamed, want) {
			t.Fatalf("expected streamed output to contain %q, got %q", want, streamed)
		}
	}
	if !strings.Contains(streamed, "\x1b[") {
		t.Fatalf("expected streamed output to contain ansi styling, got %q", streamed)
	}
	if !result.StreamedFinal {
		t.Fatalf("expected streamed final response, got %#v", result)
	}
}

func TestAssistantLiveResponseStreamer_CoalescesFlushesWithinCadence(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    90 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("First line.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	first := out.String()
	if !strings.Contains(first, "First line.") {
		t.Fatalf("expected first line to flush immediately, got %q", first)
	}

	current = current.Add(20 * time.Millisecond)
	if err := streamer.OnDelta("Second line.\n"); err != nil {
		t.Fatalf("OnDelta second returned error: %v", err)
	}
	if out.String() != first {
		t.Fatalf("expected second line to be buffered within cadence window, got %q", out.String())
	}

	current = current.Add(100 * time.Millisecond)
	if err := streamer.OnDelta("Third line.\n"); err != nil {
		t.Fatalf("OnDelta third returned error: %v", err)
	}
	flushed := out.String()
	if !strings.Contains(flushed, "Second line.") || !strings.Contains(flushed, "Third line.") {
		t.Fatalf("expected buffered lines to flush after cadence window, got %q", flushed)
	}
}

func TestAssistantLiveResponseStreamer_FinalFlushIgnoresCadence(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    90 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("First line.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	current = current.Add(20 * time.Millisecond)
	if err := streamer.OnDelta("Second line.\n"); err != nil {
		t.Fatalf("OnDelta second returned error: %v", err)
	}
	if strings.Contains(out.String(), "Second line.") {
		t.Fatalf("expected second line to remain buffered before finish, got %q", out.String())
	}

	if err := streamer.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Second line.") {
		t.Fatalf("expected final flush to ignore cadence window, got %q", out.String())
	}
}

func TestAssistantLiveResponseStreamer_UsesShorterCadenceForPlainProse(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    70 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("First sentence.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	first := out.String()

	current = current.Add(60 * time.Millisecond)
	if err := streamer.OnDelta("Second sentence.\n"); err != nil {
		t.Fatalf("OnDelta second returned error: %v", err)
	}
	if out.String() != first {
		t.Fatalf("expected prose update to remain buffered before short cadence expires, got %q", out.String())
	}

	current = current.Add(20 * time.Millisecond)
	if err := streamer.OnDelta("Third sentence.\n"); err != nil {
		t.Fatalf("OnDelta third returned error: %v", err)
	}
	flushed := out.String()
	if !strings.Contains(flushed, "Second sentence.") || !strings.Contains(flushed, "Third sentence.") {
		t.Fatalf("expected prose cadence flush after ~80ms, got %q", flushed)
	}
}

func TestAssistantLiveResponseStreamer_UsesLongerCadenceForMarkdownHeavyText(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    70 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("Intro sentence.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	first := out.String()

	current = current.Add(100 * time.Millisecond)
	if err := streamer.OnDelta("- **Important** follow-up item.\n"); err != nil {
		t.Fatalf("OnDelta second returned error: %v", err)
	}
	if out.String() != first {
		t.Fatalf("expected markdown-heavy update to remain buffered within longer cadence window, got %q", out.String())
	}

	current = current.Add(30 * time.Millisecond)
	if err := streamer.OnDelta("Another bullet detail.\n"); err != nil {
		t.Fatalf("OnDelta third returned error: %v", err)
	}
	flushed := out.String()
	if !strings.Contains(flushed, "Important") || !strings.Contains(flushed, "Another bullet detail.") {
		t.Fatalf("expected markdown-heavy flush after longer cadence, got %q", flushed)
	}
}

func TestAssistantLiveResponseStreamer_FlushesImmediatelyOnBlankLineBoundary(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    70 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("Opening line.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	first := out.String()

	current = current.Add(20 * time.Millisecond)
	if err := streamer.OnDelta("\n"); err != nil {
		t.Fatalf("OnDelta blank line returned error: %v", err)
	}
	if out.String() == first {
		t.Fatalf("expected blank line boundary to flush immediately, got %q", out.String())
	}
}

func TestAssistantLiveResponseStreamer_FlushesImmediatelyOnHeadingBoundary(t *testing.T) {
	current := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	streamer := &assistantLiveResponseStreamer{
		out:                   &out,
		now:                   func() time.Time { return current },
		plainFlushInterval:    70 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      1024,
	}

	if err := streamer.OnDelta("Intro text.\n"); err != nil {
		t.Fatalf("OnDelta first returned error: %v", err)
	}
	first := out.String()

	current = current.Add(20 * time.Millisecond)
	if err := streamer.OnDelta("## Heading\n"); err != nil {
		t.Fatalf("OnDelta heading returned error: %v", err)
	}
	if out.String() == first {
		t.Fatalf("expected heading boundary to flush immediately, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Heading") {
		t.Fatalf("expected heading to appear in flushed output, got %q", out.String())
	}
}

func TestRenderAssistantTerminalMarkdownLines_StripsMarkdownSyntax(t *testing.T) {
	lines := renderAssistantTerminalMarkdownLines(strings.Join([]string{
		"# **Complete RAF Basic Recruit Training Summary**",
		"",
		"## **Course Information**",
		"- **Course:** Basic Recruit Training Course (BRTC)",
		"1. **Wait for activation email** from `noreply@armymail.mod.uk`",
		"| Task | Deadline |",
		"|------|----------|",
		"| Activate DLE account | Within 7 days |",
	}, "\n"), "  ")
	rendered := strings.Join(lines, "\n")
	if strings.Contains(rendered, "#") || strings.Contains(rendered, "**") || strings.Contains(rendered, "`") {
		t.Fatalf("expected markdown syntax to be stripped, got %q", rendered)
	}
	if strings.Contains(rendered, "|------|") {
		t.Fatalf("expected markdown table divider to be removed, got %q", rendered)
	}
	for _, want := range []string{
		"Complete RAF Basic Recruit Training Summary",
		"Course Information",
		"Course:",
		"Wait for activation email",
		"noreply@armymail.mod.uk",
		"Activate DLE account",
		"Within 7 days",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered markdown to contain %q, got %q", want, rendered)
		}
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("expected terminal styling codes in rendered markdown, got %q", rendered)
	}
}

func TestGmailParallelMap_PreservesOrderAndSkipsFalse(t *testing.T) {
	values := gmailParallelMap([]int{1, 2, 3, 4}, 3, func(v int) (string, bool) {
		if v%2 == 0 {
			return "", false
		}
		return fmt.Sprintf("v%d", v), true
	})
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %#v", values)
	}
	if values[0] != "v1" || values[1] != "v3" {
		t.Fatalf("expected preserved order, got %#v", values)
	}
}

func TestRenderAssistantTerminalMarkdownLines_DoesNotForceWrapLongParagraphs(t *testing.T) {
	lines := renderAssistantTerminalMarkdownLines("This is a long paragraph that should wrap cleanly in the terminal without becoming one very wide unreadable line when attachment summaries get verbose.", "  ")
	if len(lines) != 1 {
		t.Fatalf("expected one line and natural terminal wrapping, got %#v", lines)
	}
	if !strings.Contains(lines[0], "attachment summaries get verbose") {
		t.Fatalf("expected full paragraph to remain on one rendered line, got %#v", lines)
	}
}

func TestAssistantRenderMarkdownTableRow_CollapsesWideRows(t *testing.T) {
	row := assistantRenderMarkdownTableRow("| Task | Deadline | Notes | Owner |")
	if !strings.Contains(row, "+1 more") {
		t.Fatalf("expected collapsed table row marker, got %q", row)
	}
}

func TestParseSemanticExtractedActions_ParsesJSON(t *testing.T) {
	actions, err := parseSemanticExtractedActions(`{
		"summary":"Reply to Palma and note the Thursday deadline.",
		"actionItems":["Reply to Palma about the meeting"],
		"deadlines":[{"task":"Send vaccination history","raw":"Thursday 9 April"}],
		"meetingReqs":[{"subject":"Weekly meeting","proposedTimes":["Saturday 10pm to 11pm"],"participants":["Palma"],"location":"Teams"}],
		"entities":[{"type":"person","value":"Palma"}]
	}`)
	if err != nil {
		t.Fatalf("parseSemanticExtractedActions returned error: %v", err)
	}
	if len(actions.ActionItems) != 1 || actions.ActionItems[0] != "Reply to Palma about the meeting" {
		t.Fatalf("unexpected action items: %#v", actions.ActionItems)
	}
	if len(actions.Deadlines) != 1 || actions.Deadlines[0].Raw != "Thursday 9 April" {
		t.Fatalf("unexpected deadlines: %#v", actions.Deadlines)
	}
	if len(actions.MeetingReqs) != 1 || len(actions.MeetingReqs[0].ProposedTimes) != 1 {
		t.Fatalf("unexpected meeting requests: %#v", actions.MeetingReqs)
	}
}

func TestAssistantPendingAttachmentDownload_All(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"has:attachment newer_than:30d\",\"max\":10}",
		`I found 2 attachments. Reply "all" to download them.`,
	}}
	capability := fakeAssistantCapability{
		name: "gmail",
		tools: []Tool{
			{Name: "gmail.search"},
			{Name: "gmail.download_attachment"},
		},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			switch toolName {
			case "gmail.search":
				return ToolResult{Success: true, Data: []NormalizedEmail{
					{
						ID:       "msg-1",
						ThreadID: "thread-1",
						Subject:  "Invoice March",
						From:     `"Stripe" <billing@example.com>`,
						Attachments: []AttachmentMeta{{
							Filename:     "invoice-march.pdf",
							AttachmentID: "att-1",
							MessageID:    "msg-1",
						}},
					},
					{
						ID:       "msg-2",
						ThreadID: "thread-2",
						Subject:  "Invoice April",
						From:     `"AWS" <billing@example.com>`,
						Attachments: []AttachmentMeta{{
							Filename:     "invoice-april.pdf",
							AttachmentID: "att-2",
							MessageID:    "msg-2",
						}},
					},
				}}, nil
			case "gmail.download_attachment":
				filename := paramString(params, "filename")
				messageID := paramString(params, "message_id")
				return ToolResult{
					Success: true,
					Data: gmailAttachmentDownloadResult{
						SavedPath: "./" + filename,
						Filename:  filename,
						MessageID: messageID,
						Files: []gmailAttachmentDownloadFile{{
							MessageID: messageID,
							Filename:  filename,
							SavedPath: "./" + filename,
						}},
					},
					Text: "saved ./" + filename,
				}, nil
			default:
				return ToolResult{}, nil
			}
		},
	}

	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{
		DefaultFormat:     "text",
		AttachmentSaveDir: ".",
		ConfirmByDefault:  true,
	})
	if _, err := session.RunTurn(context.Background(), "find invoice attachments from this month and save them", strings.NewReader(""), io.Discard, time.Now); err != nil {
		t.Fatalf("first turn returned error: %v", err)
	}
	if session.Pending == nil || session.Pending.Attachment == nil {
		t.Fatalf("expected pending attachment download, got %#v", session.Pending)
	}

	result, err := session.RunTurn(context.Background(), "all", strings.NewReader(""), io.Discard, time.Now)
	if err != nil {
		t.Fatalf("follow-up turn returned error: %v", err)
	}
	if len(result.Executions) != 2 {
		t.Fatalf("expected 2 download executions, got %d", len(result.Executions))
	}
	if !strings.Contains(strings.ToLower(result.FinalText), "saved 2 attachment") {
		t.Fatalf("expected download summary, got %q", result.FinalText)
	}
	if provider.calls != 2 {
		t.Fatalf("expected provider not to be called for follow-up, got %d calls", provider.calls)
	}
	if session.Pending != nil {
		t.Fatalf("expected pending state cleared after download, got %#v", session.Pending)
	}
}

func TestAssistantPendingDraftReply_SendAllowed(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.draft_reply\nPARAMS: {\"message_id\":\"msg-1\",\"body\":\"Sounds good to me.\"}",
		"Draft ready. Send it?",
	}}
	sendCalls := 0
	capability := fakeAssistantCapability{
		name: "gmail",
		tools: []Tool{
			{Name: "gmail.draft_reply"},
		},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			if toolName != "gmail.draft_reply" {
				return ToolResult{}, nil
			}
			if paramBool(params, "send") {
				sendCalls++
				return ToolResult{
					Success: true,
					Data: map[string]any{
						"sent": true,
					},
					Text: "reply sent to alice@example.com",
				}, nil
			}
			return ToolResult{
				Success: true,
				Data: map[string]any{
					"preview":      "Sounds good to me.",
					"body":         "Sounds good to me.",
					"message_id":   "msg-1",
					"reply_to":     "alice@example.com",
					"subject":      "Re: Schedule",
					"send_allowed": true,
				},
				Text: "draft prepared for alice@example.com",
			}, nil
		},
	}

	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{
		DefaultFormat:    "text",
		ConfirmByDefault: true,
	})
	if _, err := session.RunTurn(context.Background(), "read the latest thread from Alice and draft a reply", strings.NewReader(""), io.Discard, time.Now); err != nil {
		t.Fatalf("first turn returned error: %v", err)
	}
	if session.Pending == nil || session.Pending.DraftReply == nil {
		t.Fatalf("expected pending draft reply, got %#v", session.Pending)
	}

	result, err := session.RunTurn(context.Background(), "yes", strings.NewReader(""), io.Discard, time.Now)
	if err != nil {
		t.Fatalf("follow-up turn returned error: %v", err)
	}
	if sendCalls != 1 {
		t.Fatalf("expected one send call, got %d", sendCalls)
	}
	if !strings.Contains(strings.ToLower(result.FinalText), "reply sent") {
		t.Fatalf("expected send confirmation text, got %q", result.FinalText)
	}
	if provider.calls != 2 {
		t.Fatalf("expected provider not to be called for send follow-up, got %d calls", provider.calls)
	}
	if session.Pending != nil {
		t.Fatalf("expected pending state cleared after send, got %#v", session.Pending)
	}
}

func TestGmailNormalizeSendPermissionError_ReauthHint(t *testing.T) {
	err := gmailNormalizeSendPermissionError(errors.New("Request had insufficient authentication scopes."))
	if err == nil {
		t.Fatal("expected normalized error")
	}
	if !strings.Contains(err.Error(), "auth gmail") || !strings.Contains(err.Error(), "send access") {
		t.Fatalf("expected reauth hint, got %q", err.Error())
	}
}

func TestAssistantGroupPendingAttachmentSelections_GroupsWholeMessages(t *testing.T) {
	universe := []AssistantPendingAttachmentItem{
		{MessageID: "msg-1", Attachment: AttachmentMeta{AttachmentID: "a1"}},
		{MessageID: "msg-1", Attachment: AttachmentMeta{AttachmentID: "a2"}},
		{MessageID: "msg-2", Attachment: AttachmentMeta{AttachmentID: "b1"}},
	}
	selected := []AssistantPendingAttachmentItem{
		universe[0],
		universe[1],
		universe[2],
	}
	groups := assistantGroupPendingAttachmentSelections(selected, universe)
	if len(groups) != 2 {
		t.Fatalf("expected 2 message groups, got %d", len(groups))
	}
	if !groups[0].DownloadAll || !groups[1].DownloadAll {
		t.Fatalf("expected both groups to use download_all, got %#v", groups)
	}
}

func TestAssistantPendingAttachmentFromTurn_IgnoresCompletedReadAttachmentSummary(t *testing.T) {
	turn := &AssistantTurnResult{
		FinalText: "I read the attachment documents and summarized them above.",
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{Tool: "gmail.read_attachment"},
			Result: ToolResult{Success: true, Data: map[string]any{
				"attachments": []map[string]any{{"filename": "guide.docx"}},
			}},
		}},
	}
	pending := assistantPendingAttachmentFromTurn("read the attachments from the latest emails", turn, AssistantConfig{})
	if pending != nil {
		t.Fatalf("expected no pending attachment download, got %#v", pending)
	}
}

func TestAssistantPendingAttachment_UnrelatedFollowUpFallsThrough(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"has:attachment newer_than:30d\",\"max\":10}",
		`I found 2 attachment(s). Reply "all" to download them.`,
		"Deadlines: activate within 7 days.",
	}}
	capability := fakeAssistantCapability{
		name: "gmail",
		tools: []Tool{
			{Name: "gmail.search"},
			{Name: "gmail.download_attachment"},
		},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			switch toolName {
			case "gmail.search":
				return ToolResult{Success: true, Data: []NormalizedEmail{
					{
						ID:       "msg-1",
						ThreadID: "thread-1",
						Subject:  "Instructions",
						Attachments: []AttachmentMeta{{
							Filename:     "guide.docx",
							AttachmentID: "att-1",
							MessageID:    "msg-1",
						}},
					},
				}}, nil
			case "gmail.download_attachment":
				t.Fatal("download should not run for unrelated follow-up")
			}
			return ToolResult{}, nil
		},
	}

	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{
		DefaultFormat:    "text",
		ConfirmByDefault: true,
	})
	if _, err := session.RunTurn(context.Background(), "find the attachments from the latest email", strings.NewReader(""), io.Discard, time.Now); err != nil {
		t.Fatalf("first turn returned error: %v", err)
	}
	if session.Pending == nil || session.Pending.Attachment == nil {
		t.Fatalf("expected pending attachment download, got %#v", session.Pending)
	}

	result, err := session.RunTurn(context.Background(), "are there any deadlines i should be aware of?", strings.NewReader(""), io.Discard, time.Now)
	if err != nil {
		t.Fatalf("follow-up turn returned error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result.FinalText), "deadline") {
		t.Fatalf("expected unrelated follow-up to reach provider, got %q", result.FinalText)
	}
	if provider.calls != 3 {
		t.Fatalf("expected provider to handle unrelated follow-up, got %d calls", provider.calls)
	}
	if session.Pending != nil {
		t.Fatalf("expected stale pending state cleared, got %#v", session.Pending)
	}
}

func TestConfirmationRequired_SendEmail(t *testing.T) {
	if !shouldConfirmAssistantTool("gmail.send_email") {
		t.Fatal("expected send email to require confirmation")
	}
}

type sequentialTestProvider struct {
	responses []string
	calls     int
}

func (p *sequentialTestProvider) Name() string { return "test" }

func (p *sequentialTestProvider) Chat(messages []Message, tools []Tool) (string, error) {
	if p.calls >= len(p.responses) {
		return "done", nil
	}
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

func (p *sequentialTestProvider) IsAvailable() (bool, error) { return true, nil }

type streamingTestProvider struct {
	responses []string
	calls     int
}

func (p *streamingTestProvider) Name() string { return "streaming-test" }

func (p *streamingTestProvider) Chat(messages []Message, tools []Tool) (string, error) {
	if p.calls >= len(p.responses) {
		return "done", nil
	}
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

func (p *streamingTestProvider) ChatStream(messages []Message, tools []Tool, onDelta func(string) error) (string, error) {
	resp, err := p.Chat(messages, tools)
	if err != nil {
		return "", err
	}
	for _, chunk := range streamingChunks(resp) {
		if onDelta != nil {
			if err := onDelta(chunk); err != nil {
				return "", err
			}
		}
	}
	return resp, nil
}

func (p *streamingTestProvider) IsAvailable() (bool, error) { return true, nil }

func streamingChunks(text string) []string {
	runes := []rune(text)
	if len(runes) <= 3 {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)+2)/3)
	for i := 0; i < len(runes); i += 3 {
		end := i + 3
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

type fakeAssistantCapability struct {
	name    string
	tools   []Tool
	execute func(toolName string, params map[string]any) (ToolResult, error)
}

func (c fakeAssistantCapability) Name() string { return c.name }

func (c fakeAssistantCapability) Description() string { return c.name }

func (c fakeAssistantCapability) Tools() []Tool { return append([]Tool(nil), c.tools...) }

func (c fakeAssistantCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	if c.execute == nil {
		return ToolResult{}, nil
	}
	return c.execute(toolName, params)
}

type progressFakeAssistantCapability struct {
	name     string
	tools    []Tool
	progress func(string)
	execute  func(report func(string), toolName string, params map[string]any) (ToolResult, error)
}

func (c *progressFakeAssistantCapability) Name() string { return c.name }

func (c *progressFakeAssistantCapability) Description() string { return c.name }

func (c *progressFakeAssistantCapability) Tools() []Tool { return append([]Tool(nil), c.tools...) }

func (c *progressFakeAssistantCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	if c.execute == nil {
		return ToolResult{}, nil
	}
	return c.execute(c.progress, toolName, params)
}

func (c *progressFakeAssistantCapability) SetProgressReporter(fn func(string)) {
	c.progress = fn
}

func TestConfirmationRequired_DownloadAttachment_NotRequired(t *testing.T) {
	if shouldConfirmAssistantTool("gmail.download_attachment") {
		t.Fatal("expected attachment download to be read-only in confirmation helper")
	}
}

func TestConfirmationRequired_DeleteMessage_Required(t *testing.T) {
	if !shouldConfirmAssistantTool("gmail.delete_message") {
		t.Fatal("expected delete to require confirmation")
	}
	if !isDeleteAssistantOperation("gmail.delete_message") {
		t.Fatal("expected delete helper to detect delete operation")
	}
}

func TestParseAssistantToolCalls_InlineParams(t *testing.T) {
	parsed := ParseAssistantToolCalls(`TOOL: gmail.searchPARAMS: {"query":"is:unread newer_than:1d","max":10}`)
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d (%v)", len(parsed.ToolCalls), parsed.Warnings)
	}
	if parsed.ToolCalls[0].Tool != "gmail.search" {
		t.Fatalf("expected gmail.search, got %q", parsed.ToolCalls[0].Tool)
	}
	if got := parsed.ToolCalls[0].Params["query"]; got != "is:unread newer_than:1d" {
		t.Fatalf("expected query param, got %#v", got)
	}
}

func TestParseAssistantToolCalls_RecoversMalformedJSONWrapper(t *testing.T) {
	parsed := ParseAssistantToolCalls(`TOOL: gmail.searchPARAMS: {"json":"{"query":"","input":"Today","max":1}"}`)
	if len(parsed.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d (%v)", len(parsed.ToolCalls), parsed.Warnings)
	}
	if got := parsed.ToolCalls[0].Params["input"]; got != "Today" {
		t.Fatalf("expected recovered input param, got %#v", got)
	}
}

func TestNormalizeAssistantToolCalls_FallsBackForMissingReadMessageID(t *testing.T) {
	calls := normalizeAssistantToolCalls("do i have any new emails?", []AssistantToolCall{{
		Tool:   "gmail.read_message",
		Params: map[string]any{"id": "none specified"},
	}})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Tool != "gmail.search" {
		t.Fatalf("expected fallback gmail.search, got %q", calls[0].Tool)
	}
}

func TestGmailStripHTML_RemovesScriptAndStyle(t *testing.T) {
	input := `<html><head><style>body{display:none}</style></head><body><script>alert(1)</script><p>Hello<br>world</p></body></html>`
	got := gmailStripHTML(input)
	if strings.Contains(got, "alert") || strings.Contains(got, "display:none") {
		t.Fatalf("expected script/style content removed, got %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Fatalf("expected text preserved, got %q", got)
	}
}

func TestAssistantConfig_LoadsFromEnv(t *testing.T) {
	configRoot := t.TempDir()
	setAssistantConfigEnv(t, configRoot)
	t.Setenv("JOT_ASSISTANT_PROVIDER", "openai")
	t.Setenv("JOT_ASSISTANT_MODEL", "gpt-4.1")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test")

	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		t.Fatalf("LoadAssistantConfig returned error: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4.1" {
		t.Fatalf("expected env overrides, got %#v", cfg)
	}
	if cfg.OpenAIKey != "sk-test" || cfg.AnthropicKey != "anthropic-test" {
		t.Fatalf("expected API keys from env, got %#v", cfg)
	}
}

func TestAssistantConfig_DefaultsApplied(t *testing.T) {
	configRoot := t.TempDir()
	setAssistantConfigEnv(t, configRoot)

	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		t.Fatalf("LoadAssistantConfig returned error: %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Fatalf("expected default provider ollama, got %q", cfg.Provider)
	}
	if cfg.Model != "llama3.2" {
		t.Fatalf("expected default model llama3.2, got %q", cfg.Model)
	}
	if cfg.OllamaURL != "http://localhost:11434" {
		t.Fatalf("expected default ollama url, got %q", cfg.OllamaURL)
	}
	if cfg.DefaultFormat != "text" {
		t.Fatalf("expected default format text, got %q", cfg.DefaultFormat)
	}
}

func TestParseAssistantInvocation_OnboardingFlag(t *testing.T) {
	configRoot := t.TempDir()
	setAssistantConfigEnv(t, configRoot)

	inv, err := parseAssistantInvocation([]string{"--onboarding"})
	if err != nil {
		t.Fatalf("parseAssistantInvocation returned error: %v", err)
	}
	if !inv.Onboarding {
		t.Fatalf("expected onboarding flag to be set, got %#v", inv)
	}
}

func TestAssistantProviderConfigured_OllamaLocalDoesNotRequireKey(t *testing.T) {
	cfg := AssistantConfig{
		Provider:  "ollama",
		Model:     "llama3.2",
		OllamaURL: "http://localhost:11434",
	}
	if !assistantProviderConfigured(cfg) {
		t.Fatalf("expected local ollama config to count as configured, got %#v", cfg)
	}
}

func TestAssistantProviderConfigured_OllamaCloudRequiresKey(t *testing.T) {
	cfg := AssistantConfig{
		Provider:  "ollama",
		Model:     "glm-5:cloud",
		OllamaURL: "https://ollama.com",
	}
	if assistantProviderConfigured(cfg) {
		t.Fatalf("expected remote ollama config without key to be incomplete, got %#v", cfg)
	}
	cfg.OllamaAPIKey = "test-key"
	if !assistantProviderConfigured(cfg) {
		t.Fatalf("expected remote ollama config with key to count as configured, got %#v", cfg)
	}
}

func TestAssistantNeedsOnboarding_MissingGmailToken(t *testing.T) {
	cfg := AssistantConfig{
		Provider:       "ollama",
		Model:          "llama3.2",
		OllamaURL:      "http://localhost:11434",
		GmailTokenPath: filepath.Join(t.TempDir(), "gmail_token.json"),
	}
	if !assistantNeedsOnboarding(cfg) {
		t.Fatalf("expected missing gmail token to require onboarding, got %#v", cfg)
	}
}

func buildTestDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writer, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("Create docx entry: %v", err)
	}
	if _, err := writer.Write([]byte(documentXML)); err != nil {
		t.Fatalf("Write docx entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close docx zip: %v", err)
	}
	return buf.Bytes()
}

func buildTestXLSX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	write := func(name, body string) {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create xlsx entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatalf("Write xlsx entry %s: %v", name, err)
		}
	}

	write("xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
  <si><t>Name</t></si>
  <si><t>Amount</t></si>
  <si><t>Alice</t></si>
</sst>`)
	write("xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
      <c r="B1" t="s"><v>1</v></c>
    </row>
    <row r="2">
      <c r="A2" t="s"><v>2</v></c>
      <c r="B2"><v>42</v></c>
    </row>
  </sheetData>
</worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("Close xlsx zip: %v", err)
	}
	return buf.Bytes()
}

func setAssistantConfigEnv(t *testing.T, configRoot string) {
	t.Helper()
	t.Setenv("JOT_ASSISTANT_PROVIDER", "")
	t.Setenv("JOT_ASSISTANT_MODEL", "")
	t.Setenv("JOT_ASSISTANT_OLLAMA_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", configRoot)
	default:
		t.Setenv("XDG_CONFIG_HOME", configRoot)
		t.Setenv("HOME", configRoot)
	}

	jotDir := filepath.Join(configRoot, "jot")
	if runtime.GOOS != "windows" {
		jotDir = filepath.Join(configRoot, "jot")
	}
	if err := os.MkdirAll(jotDir, 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}
	data, err := json.Marshal(AssistantConfig{})
	if err != nil {
		t.Fatalf("Marshal config fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jotDir, "assistant.json"), data, 0o600); err != nil {
		t.Fatalf("Write config fixture: %v", err)
	}
}
