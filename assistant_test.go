package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestMapNLToGmailQuery_PassportLookupPreservesTerms(t *testing.T) {
	if got := mapNLToGmailQuery("passport number"); got != "passport number" {
		t.Fatalf("expected passport lookup to preserve raw terms, got %q", got)
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

func TestReadAttachmentContent_ImageAttachmentNeedsOCR(t *testing.T) {
	content, err := ReadAttachmentContent([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}, AttachmentMeta{
		Filename: "passport.jpg",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("expected best-effort image extraction, got %v", err)
	}
	if content.Metadata["type"] != "image" {
		t.Fatalf("expected image metadata, got %#v", content.Metadata)
	}
	if strings.Contains(strings.ToLower(content.Text), "passport number") {
		t.Fatalf("expected no fabricated passport text from bland image bytes, got %q", content.Text)
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
	if strings.Contains(rendered, "Alice is asking to reschedule the auth refactor review. Ben moved standup to 3pm.") {
		t.Fatalf("expected duplicate prose summary to be suppressed once the Gmail card is rendered, got %q", rendered)
	}
}

func TestRenderAssistantTurn_FormFillUsesBrowserReviewCard(t *testing.T) {
	now := time.Date(2026, time.April, 3, 13, 0, 0, 0, time.UTC)
	result := &AssistantTurnResult{
		Input:     "help me fill the RSVP form",
		FinalText: "the form is open in the browser for review",
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{Tool: "gmail.fill_form"},
			Result: ToolResult{
				Success: true,
				Data: FormFillResult{
					FormTitle: "Event RSVP",
					Fields: []FilledField{
						{Field: FormField{Label: "Can you attend? *", Required: true}, Answer: "Yes"},
						{Field: FormField{Label: "Comments"}, Answer: ""},
					},
					Notes: []string{"1 field(s) still need your review or manual input in the browser"},
				},
			},
		}},
	}

	rendered, err := RenderAssistantTurn(result.Input, result, nil, "text", now)
	if err != nil {
		t.Fatalf("RenderAssistantTurn returned error: %v", err)
	}
	if !strings.Contains(rendered, "Form · browser review") || !strings.Contains(rendered, "Event RSVP") {
		t.Fatalf("expected browser review card, got %q", rendered)
	}
	if strings.Contains(rendered, "the form is open in the browser for review") {
		t.Fatalf("expected duplicate final prose to be suppressed once the form card is rendered, got %q", rendered)
	}
}

func TestRenderAssistantTurn_GmailCardKeepsActualAnswer(t *testing.T) {
	now := time.Date(2026, time.April, 5, 12, 0, 0, 0, time.UTC)
	result := &AssistantTurnResult{
		Input:     "what is my share code?",
		FinalText: "Your share code is:\n\nWBW 765 5GS",
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{Tool: "gmail.search"},
			Result: ToolResult{
				Success: true,
				Data: []NormalizedEmail{
					{
						ID:      "msg-1",
						From:    "Isaiah Ntina",
						Subject: "SHARE CODE - right to work - WBW 765 5GS",
						Date:    now.Add(-time.Hour),
					},
				},
			},
		}},
	}

	rendered, err := RenderAssistantTurn(result.Input, result, nil, "text", now)
	if err != nil {
		t.Fatalf("RenderAssistantTurn returned error: %v", err)
	}
	if !strings.Contains(rendered, "Gmail") {
		t.Fatalf("expected Gmail card heading, got %q", rendered)
	}
	if !strings.Contains(rendered, "WBW 765 5GS") {
		t.Fatalf("expected final answer to remain visible, got %q", rendered)
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

func TestRunTurn_DoesNotStreamToolDirectiveAndSuppressesPostToolStreaming(t *testing.T) {
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
	if strings.Contains(out.String(), "Done reading mail.") {
		t.Fatalf("expected post-tool final prose not to stream live once structured rendering will take over, got %q", out.String())
	}
	if result.StreamedFinal {
		t.Fatalf("expected post-tool final answer not to be marked streamed, got %#v", result)
	}
}

func TestRunTurn_DoesNotLeakProseBeforeToolDirective(t *testing.T) {
	provider := &streamingTestProvider{responses: []string{
		"I'll help you with that.\n\nTOOL: gmail.search\nPARAMS: {\"query\":\"from:palma\",\"max\":5}",
		"Done.",
	}}
	capability := fakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.search"}},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			return ToolResult{Success: true, Data: []NormalizedEmail{{ID: "msg-1", Subject: "event rsvp"}}}, nil
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	if _, err := session.RunTurn(context.Background(), "find palma's form", strings.NewReader(""), &out, time.Now); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if strings.Contains(out.String(), "I'll help you with that.") || strings.Contains(out.String(), "TOOL: gmail.search") {
		t.Fatalf("expected pre-tool chatter to stay hidden, got %q", out.String())
	}
}

func TestAssistantToolCompletesTurn_FormFill(t *testing.T) {
	if !assistantToolCompletesTurn(
		AssistantToolCall{Tool: "gmail.fill_form"},
		ToolResult{Success: true, Text: "the form is open in the browser for review"},
	) {
		t.Fatal("expected gmail.fill_form success to complete the turn")
	}
	if assistantToolCompletesTurn(
		AssistantToolCall{Tool: "gmail.fill_form"},
		ToolResult{Success: false, Error: "boom"},
	) {
		t.Fatal("expected failed gmail.fill_form not to complete the turn")
	}
}

func TestBuildSystemPrompt_AllowsDirectFormURLs(t *testing.T) {
	session := NewAssistantSession(&sequentialTestProvider{responses: []string{"ok"}}, nil, AssistantConfig{})
	prompt := session.BuildSystemPrompt(time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC))
	if !strings.Contains(prompt, "If the user gives you a direct form URL, call gmail.fill_form with form_url") {
		t.Fatalf("expected direct form URL guidance in system prompt, got %q", prompt)
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

func TestFindFormLinks_GoogleForm(t *testing.T) {
	email := NormalizedEmail{
		ID:       "msg-1",
		Subject:  "Pearson 786 arrivals",
		BodyText: "Please complete the arrivals form by Wednesday 8 April 2026. Fill in the form here.",
		Links: []EmailLink{{
			URL:     "https://docs.google.com/forms/d/e/example/viewform",
			Label:   "Pearson 786 MP Arrivals Form",
			Context: "Please complete the arrivals form by Wednesday 8 April 2026.",
		}},
	}
	links := FindFormLinks(email)
	if len(links) != 1 {
		t.Fatalf("expected 1 form link, got %#v", links)
	}
	if links[0].URL != "https://docs.google.com/forms/d/e/example/viewform" {
		t.Fatalf("unexpected form link: %#v", links[0])
	}
	if links[0].Deadline == "" {
		t.Fatalf("expected deadline extracted, got %#v", links[0])
	}
}

func TestClassifyField_ServiceNumber(t *testing.T) {
	field := FormField{Label: "Service Number"}
	if got := ClassifyField(field, "RAF arrivals form"); got != SemanticServiceNumber {
		t.Fatalf("expected service number semantic, got %q", got)
	}
}

func TestGoogleFormsExtractFieldsFromHTML(t *testing.T) {
	html := `<!doctype html><html><head><title>Pearson 786 MP Arrivals Form</title></head><body><script>FB_PUBLIC_LOAD_DATA_ = [null, [[101,"First Name",null,0,true],[102,"Arrival Date",null,9,true],[103,"Mode of Transport",null,2,[["Train"],["Car"],["Flight"]]]]];</script></body></html>`
	fields, err := googleFormsExtractFieldsFromHTML(html)
	if err != nil {
		t.Fatalf("googleFormsExtractFieldsFromHTML returned error: %v", err)
	}
	if len(fields) < 3 {
		t.Fatalf("expected at least 3 fields, got %#v", fields)
	}
	if fields[0].Label != "First Name" {
		t.Fatalf("unexpected first field: %#v", fields[0])
	}
}

func TestGoogleFormsExtractFieldsFromVisibleHTML(t *testing.T) {
	html := `<!doctype html><html><head><title>Contact information</title></head><body>
<div>Contact information</div>
<div>* Indica una domanda obbligatoria</div>
<div>Name *</div>
<div>La tua risposta</div>
<div>Email *</div>
<div>La tua risposta</div>
<div>Address *</div>
<div>La tua risposta</div>
<div>Phone number</div>
<div>La tua risposta</div>
<div>Comments</div>
<div>La tua risposta</div>
</body></html>`
	fields := googleFormsExtractFieldsFromVisibleHTML(html)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %#v", fields)
	}
	if fields[0].Label != "Name" || !fields[0].Required {
		t.Fatalf("unexpected first field: %#v", fields[0])
	}
	if fields[3].Label != "Phone number" {
		t.Fatalf("unexpected phone field: %#v", fields[3])
	}
}

func TestSearchForAnswer_UsesProviderJSON(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		`{"answer":"Train","source":"thread email 1","confidence":"HIGH","reasoning":"The email explicitly says train."}`,
	}}
	answer, source, confidence, reasoning := SearchForAnswer(
		SemanticTransport,
		FormField{Label: "Mode of Transport"},
		"Pearson arrivals form",
		[]NormalizedEmail{{Subject: "Travel", BodyText: "Mode of transport: Train"}},
		nil,
		nil,
		provider,
	)
	if answer != "Train" || source != "thread email 1" || confidence != ConfidenceHigh {
		t.Fatalf("unexpected answer tuple: %q %q %q", answer, source, confidence)
	}
	if reasoning == "" {
		t.Fatal("expected reasoning")
	}
}

func TestSearchForAnswer_UsesExactFactFromNotes(t *testing.T) {
	answer, source, confidence, reasoning := SearchForAnswer(
		SemanticUnknown,
		FormField{Label: "Passport number"},
		"",
		nil,
		nil,
		[]string{"passport number is A1234567"},
		nil,
	)
	if answer != "A1234567" || source != "instruction 1" || confidence != ConfidenceHigh {
		t.Fatalf("unexpected exact-fact extraction tuple: %q %q %q", answer, source, confidence)
	}
	if reasoning == "" {
		t.Fatal("expected reasoning for exact-fact extraction")
	}
}

func TestGmailSearchMessages_SortsNewestFirstAndRespectsLimit(t *testing.T) {
	mux := http.NewServeMux()
	gotQuery := ""
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		if gotQuery != "passport number" {
			t.Fatalf("expected search query to be preserved, got %q", gotQuery)
		}
		if got := r.URL.Query().Get("maxResults"); got != "2" {
			t.Fatalf("expected maxResults=2, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(gmailListMessagesResponse{
			Messages: []gmailMessageRef{
				{ID: "msg-new"},
				{ID: "msg-mid"},
				{ID: "msg-old"},
			},
			ResultSizeEstimate: 3,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-old", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gmailMessage{
			ID:           "msg-old",
			ThreadID:     "thread-1",
			InternalDate: "1710000000000",
			Snippet:      "old passport candidate",
			Payload: gmailMessagePart{
				Headers: []gmailHeader{{Name: "Subject", Value: "Old passport note"}},
			},
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-new", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gmailMessage{
			ID:           "msg-new",
			ThreadID:     "thread-1",
			InternalDate: "1730000000000",
			Snippet:      "new passport candidate",
			Payload: gmailMessagePart{
				Headers: []gmailHeader{{Name: "Subject", Value: "New passport note"}},
			},
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-mid", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gmailMessage{
			ID:           "msg-mid",
			ThreadID:     "thread-1",
			InternalDate: "1720000000000",
			Snippet:      "mid passport candidate",
			Payload: gmailMessagePart{
				Headers: []gmailHeader{{Name: "Subject", Value: "Mid passport note"}},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	gmail := &GmailCapability{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}
	results, err := gmail.searchMessages("passport number", 2)
	if err != nil {
		t.Fatalf("searchMessages returned error: %v", err)
	}
	if gotQuery == "" {
		t.Fatal("expected Gmail search query to be recorded")
	}
	if len(results) != 2 {
		t.Fatalf("expected search results to be capped at 2, got %#v", results)
	}
	if results[0].ID != "msg-new" || results[1].ID != "msg-mid" {
		t.Fatalf("expected results sorted newest-first, got %#v", results)
	}
}

func TestGmailSearchMessages_RanksSemanticallyRelevantCandidatesFirst(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "passport number" {
			t.Fatalf("expected search query to be preserved, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(gmailListMessagesResponse{
			Messages: []gmailMessageRef{
				{ID: "msg-newer-noisy"},
				{ID: "msg-older-passport"},
			},
			ResultSizeEstimate: 2,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-newer-noisy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gmailMessage{
			ID:           "msg-newer-noisy",
			ThreadID:     "thread-2",
			InternalDate: "1740000000000",
			Snippet:      "Weekly digest and notifications",
			Payload: gmailMessagePart{
				Headers: []gmailHeader{{Name: "Subject", Value: "Weekly digest"}},
			},
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/msg-older-passport", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gmailMessage{
			ID:           "msg-older-passport",
			ThreadID:     "thread-3",
			InternalDate: "1710000000000",
			Snippet:      "Your passport number is A1234567.",
			Payload: gmailMessagePart{
				Headers: []gmailHeader{{Name: "Subject", Value: "Passport confirmation"}},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	gmail := &GmailCapability{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}
	results, err := gmail.searchMessages("passport number", 5)
	if err != nil {
		t.Fatalf("searchMessages returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two search results, got %#v", results)
	}
	if results[0].ID != "msg-older-passport" {
		t.Fatalf("expected semantically relevant candidate first, got %#v", results)
	}
}

func TestRunTurn_PassportLookupStopsAfterFirstRelevantSearch(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"passport number\",\"max\":3}",
		`{"found":true,"value":"A1234567","confidence":"high","source":"email \"Passport reference\"","reason":"The snippet explicitly contains the passport number."}`,
	}}
	var searchCalls int
	capability := fakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.search"}},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			searchCalls++
			if searchCalls > 1 {
				t.Fatalf("expected a single search round, got %d", searchCalls)
			}
			return ToolResult{Success: true, Data: []NormalizedEmail{{
				ID:      "msg-passport",
				From:    `"Passport Office" <noreply@example.com>`,
				Subject: "Passport reference",
				Snippet: "Your passport number is A1234567.",
				Date:    time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC),
			}}}, nil
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "what is my passport number?", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if searchCalls != 1 {
		t.Fatalf("expected one Gmail search execution, got %d", searchCalls)
	}
	if provider.calls != 2 {
		t.Fatalf("expected tool call plus semantic answer, got %d", provider.calls)
	}
	if strings.Contains(strings.ToLower(result.FinalText), "exceeded") {
		t.Fatalf("expected graceful stopping, got %#v", result)
	}
	if !strings.Contains(result.FinalText, "A1234567") {
		t.Fatalf("expected passport number in final answer, got %#v", result.FinalText)
	}
}

func TestRunTurn_PassportLookupReadsTopAttachmentDeterministically(t *testing.T) {
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"passport number\",\"max\":3}",
		`{"found":false,"focusMessageIds":["msg-passport-images"],"followUpQueries":[],"reason":"The likely answer is in the attached scan."}`,
		`{"attachmentIds":["att-1"],"reason":"The first image is the likely passport scan."}`,
		`{"found":true,"value":"A1234567","confidence":"medium","source":"attachment \"IMG_0744.jpeg\"","reason":"The attachment text explicitly states the passport number."}`,
	}}
	var searchCalls int
	var readAttachmentCalls int
	capability := fakeAssistantCapability{
		name: "gmail",
		tools: []Tool{
			{Name: "gmail.search"},
			{Name: "gmail.read_attachment"},
		},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			switch toolName {
			case "gmail.search":
				searchCalls++
				return ToolResult{Success: true, Data: []NormalizedEmail{{
					ID:      "msg-passport-images",
					From:    `"Isaiah Ntina" <isaiah@example.com>`,
					Subject: "Passport BRP ID permit Work permit UK travel UK",
					Snippet: "Scans attached.",
					Date:    time.Date(2026, time.January, 25, 9, 0, 0, 0, time.UTC),
					Attachments: []AttachmentMeta{
						{Filename: "IMG_0744.jpeg", MimeType: "image/jpeg", AttachmentID: "att-1"},
						{Filename: "IMG_0745.jpeg", MimeType: "image/jpeg", AttachmentID: "att-2"},
					},
				}}}, nil
			case "gmail.read_attachment":
				readAttachmentCalls++
				return ToolResult{Success: true, Data: map[string]any{
					"attachments": []gmailAttachmentContentResult{
						{
							Subject: "Passport BRP ID permit Work permit UK travel UK",
							From:    `"Isaiah Ntina" <isaiah@example.com>`,
							Date:    time.Date(2026, time.January, 25, 9, 0, 0, 0, time.UTC),
							Attachment: AttachmentMeta{
								Filename: "IMG_0744.jpeg",
							},
							Content: AttachmentContent{
								Text: "Passport number: A1234567",
								Metadata: map[string]string{
									"type": "ocr/tesseract",
								},
							},
						},
					},
				}}, nil
			default:
				t.Fatalf("unexpected tool call %q", toolName)
				return ToolResult{}, nil
			}
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{DefaultFormat: "text"})
	session.Format = "text"

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "what is my passport number?", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if searchCalls != 1 {
		t.Fatalf("expected one search call, got %d", searchCalls)
	}
	if readAttachmentCalls != 1 {
		t.Fatalf("expected one attachment read call, got %d", readAttachmentCalls)
	}
	if provider.calls != 4 {
		t.Fatalf("expected tool call plus semantic plan/select/resolve, got %d", provider.calls)
	}
	if !strings.Contains(result.FinalText, "A1234567") {
		t.Fatalf("expected extracted passport number, got %#v", result.FinalText)
	}
}

func TestReadAttachmentContent_ImageAttachmentRecoversEmbeddedCommentText(t *testing.T) {
	data := jpegWithComment(t, "Passport number: A1234567")
	content, err := ReadAttachmentContent(data, AttachmentMeta{
		Filename: "passport.jpg",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("ReadAttachmentContent returned error: %v", err)
	}
	if !strings.Contains(content.Text, "Passport number: A1234567") {
		t.Fatalf("expected embedded image text to be recovered, got %q", content.Text)
	}
	if content.Metadata["recovered_text"] != "yes" {
		t.Fatalf("expected recovered_text metadata, got %#v", content.Metadata)
	}
}

func TestGmailAttachmentNeedsOCRFallback_ImagePlaceholder(t *testing.T) {
	meta := AttachmentMeta{Filename: "passport.jpg", MimeType: "image/jpeg"}
	content := AttachmentContent{
		Text:     "Image attachment",
		Warnings: []string{"No embedded text was recovered from the image bytes"},
		Metadata: map[string]string{"type": "image/jpeg"},
	}
	if !gmailAttachmentNeedsOCRFallback(content, meta) {
		t.Fatalf("expected OCR fallback for placeholder image content")
	}
}

func TestGmailAttachmentNeedsOCRFallback_RecoveredImageTextSkipsOCR(t *testing.T) {
	meta := AttachmentMeta{Filename: "passport.jpg", MimeType: "image/jpeg"}
	content := AttachmentContent{
		Text:     "Passport number: A1234567",
		Warnings: nil,
		Metadata: map[string]string{"type": "image/jpeg", "recovered_text": "yes"},
	}
	if gmailAttachmentNeedsOCRFallback(content, meta) {
		t.Fatalf("did not expect OCR fallback when image text was already recovered")
	}
}

func TestGmailRunWindowsOCR_Smoke(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only OCR fallback")
	}
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not available")
	}
	tempDir := t.TempDir()
	imagePath := filepath.Join(tempDir, "sample.png")
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$bmp = New-Object System.Drawing.Bitmap 600,120
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.Clear([System.Drawing.Color]::White)
$font = New-Object System.Drawing.Font('Arial', 28)
$g.DrawString('Passport number A1234567', $font, [System.Drawing.Brushes]::Black, 10, 30)
$bmp.Save('` + imagePath + `',[System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose()
$bmp.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create sample OCR image: %v: %s", err, string(output))
	}
	text, warnings, err := gmailRunWindowsOCR(context.Background(), imagePath, tempDir)
	if err != nil {
		t.Fatalf("gmailRunWindowsOCR returned error: %v", err)
	}
	if !strings.Contains(text, "A1234567") {
		t.Fatalf("expected OCR text to contain passport number, got %q", text)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected OCR warnings/metadata, got none")
	}
}

func TestBrowserFormFieldsFromSnapshot(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Contact information",
		Elements: []BrowserPageElement{
			{Label: "Name", Role: "input", Required: true, Visible: true},
			{Label: "Email", Role: "input", Required: true, Visible: true},
			{Label: "Address", Role: "textarea", Required: true, Visible: true},
			{Label: "Country", Role: "select", Options: []string{"UK", "Kenya"}, Visible: true},
			{Label: "Submit", Role: "button", Visible: true},
		},
	}
	fields := browserFormFieldsFromSnapshot(snapshot)
	if len(fields) != 4 {
		t.Fatalf("expected 4 form fields, got %#v", fields)
	}
	var sawTextarea bool
	var sawSelect bool
	for _, field := range fields {
		if field.Label == "Address" && field.Type == "textarea" {
			sawTextarea = true
		}
		if field.Label == "Country" && field.Type == "select" && len(field.Options) == 2 {
			sawSelect = true
		}
	}
	if !sawTextarea {
		t.Fatalf("expected textarea field, got %#v", fields)
	}
	if !sawSelect {
		t.Fatalf("expected select field with options, got %#v", fields)
	}
}

func TestBrowserFormFieldsFromSnapshot_GroupsSemanticRadioControls(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "T-Shirt Sign Up",
		Elements: []BrowserPageElement{
			{Label: "black", GroupLabel: "colour preference", Role: "radio", Visible: true},
			{Label: "white", GroupLabel: "colour preference", Role: "radio", Visible: true},
			{Label: "pink", GroupLabel: "colour preference", Role: "radio", Visible: true},
			{Label: "Name", Role: "input", Visible: true, Required: true},
			{Label: "XS", GroupLabel: "Shirt size", Role: "radio", Visible: true},
			{Label: "S", GroupLabel: "Shirt size", Role: "radio", Visible: true},
			{Label: "M", GroupLabel: "Shirt size", Role: "radio", Visible: true},
			{Label: "Other thoughts or comments", Role: "textbox", Visible: true},
		},
	}
	fields := browserFormFieldsFromSnapshot(snapshot)
	if len(fields) != 4 {
		t.Fatalf("expected 4 semantic fields, got %#v", fields)
	}
	var foundColour bool
	for _, field := range fields {
		if field.Label == "colour preference" {
			foundColour = true
			if field.Type != "radio" || len(field.Options) != 3 {
				t.Fatalf("unexpected colour field: %#v", field)
			}
		}
		if strings.Contains(strings.ToLower(field.Label), "forms") || strings.Contains(strings.ToLower(field.Label), "report") {
			t.Fatalf("unexpected noise field: %#v", field)
		}
	}
	if !foundColour {
		t.Fatalf("expected grouped colour field, got %#v", fields)
	}
}

func TestBrowserFormFieldsFromSnapshot_GroupsCheckboxControls(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "Mains", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true},
			{Label: "Salad", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true},
			{Label: "Dessert", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true},
			{Label: "Allergic to dairy and peanuts", Role: "textbox", Visible: true},
		},
	}
	fields := browserFormFieldsFromSnapshot(snapshot)
	var foundBring bool
	for _, field := range fields {
		if field.Label == "What will you be bringing?" {
			foundBring = true
			if field.Type != "checkbox" || len(field.Options) != 3 {
				t.Fatalf("unexpected checkbox group: %#v", field)
			}
		}
	}
	if !foundBring {
		t.Fatalf("expected checkbox group field, got %#v", fields)
	}
}

func TestSemanticCleanBrowserFormFields_RemovesOptionAndChromeNoise(t *testing.T) {
	fields := semanticCleanBrowserFormFields([]FormField{
		{Label: "Can you attend?", Type: "radio", Options: []string{"Yes, I'll be there", "Sorry, can't make it"}},
		{Label: "Yes, I'll be there", Type: "text"},
		{Label: "Sorry, can't make it", Type: "text"},
		{Label: "Other response", Type: "text"},
		{Label: "Does this form look suspicious? Report", Type: "text"},
		{Label: "What is your name?", Type: "text"},
	}, "Party Invite")
	if len(fields) != 2 {
		t.Fatalf("expected only the real question fields to remain, got %#v", fields)
	}
	for _, field := range fields {
		lower := strings.ToLower(field.Label)
		if strings.Contains(lower, "report") || strings.Contains(lower, "sorry") || strings.Contains(lower, "yes") || strings.Contains(lower, "other response") {
			t.Fatalf("unexpected noisy field survived cleanup: %#v", field)
		}
	}
}

func TestBuildBrowserFormPageModel_TracksRequiredCompletion(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "Ntina", GroupLabel: "What is your name?", Role: "textbox", Value: "Ntina", Visible: true},
			{Label: "Yes, I'll be there", GroupLabel: "Can you attend?", Role: "radio", Checked: true, Visible: true},
			{Label: "Sorry, can't make it", GroupLabel: "Can you attend?", Role: "radio", Visible: true},
			{Label: "Mains", GroupLabel: "What will you be bringing?", Role: "checkbox", Checked: true, Visible: true},
			{Label: "Salad", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true},
			{Label: "Submit", Role: "button", Visible: true},
		},
	}
	model := buildBrowserFormPageModel(snapshot, []FilledField{
		{Field: FormField{Label: "What is your name?", Type: "text", Required: true}, Answer: "Ntina"},
		{Field: FormField{Label: "Can you attend?", Type: "radio", Required: true}, Answer: "Yes, I'll be there"},
		{Field: FormField{Label: "What will you be bringing?", Type: "checkbox", Required: true}, Answer: "Mains"},
	})
	if model.RequiredTotal != 3 || model.RequiredAnswered != 3 {
		t.Fatalf("unexpected required counts: %#v", model)
	}
	if len(model.RequiredUnanswered) != 0 {
		t.Fatalf("unexpected unanswered required list: %#v", model.RequiredUnanswered)
	}
	if !model.SubmitAvailable {
		t.Fatalf("expected submit button to be detected, got %#v", model)
	}
}

func TestBrowserSelectedOptions_UsesCheckedState(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "Salad", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true, Checked: true},
			{Label: "Mains", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true, Checked: true},
			{Label: "Dessert", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true},
		},
	}
	selected := browserSelectedOptions(snapshot, FormField{Label: "What will you be bringing?", Type: "checkbox"})
	if len(selected) != 2 {
		t.Fatalf("expected two selected options, got %#v", selected)
	}
	want := map[string]bool{"Mains": true, "Salad": true}
	for _, option := range selected {
		if !want[option] {
			t.Fatalf("unexpected selected option %q in %#v", option, selected)
		}
		delete(want, option)
	}
	if len(want) != 0 {
		t.Fatalf("missing selected options: %#v", want)
	}
}

func TestBrowserFieldAnswerVerified_UsesCheckedAndSelectedState(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "Yes, I'll be there", GroupLabel: "Can you attend?", Role: "radio", Visible: true, Checked: true},
			{Label: "Mains", GroupLabel: "What will you be bringing?", Role: "checkbox", Visible: true, Checked: true},
		},
	}
	if !browserFieldAnswerVerified(snapshot, FormField{Label: "Can you attend?", Type: "radio"}, "Yes, I'll be there") {
		t.Fatal("expected radio answer to verify from checked state")
	}
	if !browserFieldAnswerVerified(snapshot, FormField{Label: "What will you be bringing?", Type: "checkbox"}, "Mains") {
		t.Fatal("expected checkbox answer to verify from checked state")
	}
	if browserFieldAnswerVerified(snapshot, FormField{Label: "What will you be bringing?", Type: "checkbox"}, "Salad") {
		t.Fatal("expected unchecked option not to verify")
	}
}

func TestBrowserPlannedFillActions_SkipsVerifiedAnswersFromPerception(t *testing.T) {
	model := BrowserFormPageModel{
		Questions: []BrowserFormQuestionState{
			{Field: FormField{Label: "Can you attend?", Type: "radio"}, Answer: "Yes, I'll be there", Verified: true, Visible: true},
			{Field: FormField{Label: "What will you be bringing?", Type: "checkbox"}, Answer: "Mains", Verified: true, Visible: true},
			{Field: FormField{Label: "Comments", Type: "textarea"}, Answer: "Please count me in", Verified: false, Visible: true},
		},
	}
	actions := browserPlannedFillActions([]FilledField{
		{Field: FormField{Label: "Can you attend?", Type: "radio"}, Answer: "Yes, I'll be there"},
		{Field: FormField{Label: "What will you be bringing?", Type: "checkbox"}, Answer: "Mains"},
		{Field: FormField{Label: "Comments", Type: "textarea"}, Answer: "Please count me in"},
	}, model)
	if len(actions) != 1 {
		t.Fatalf("expected only unresolved field to remain actionable, got %#v", actions)
	}
	if actions[0].Field.Label != "Comments" {
		t.Fatalf("expected comments field to remain actionable, got %#v", actions[0])
	}
}

func TestBrowserPlannedFillActions_FallsBackWhenPerceptionUnavailable(t *testing.T) {
	model := BrowserFormPageModel{
		Questions: []BrowserFormQuestionState{
			{Field: FormField{Label: "Can you attend?", Type: "radio"}, Answer: "Yes, I'll be there", Verified: false},
			{Field: FormField{Label: "What will you be bringing?", Type: "checkbox"}, Answer: "Mains", Verified: false},
		},
	}
	actions := browserPlannedFillActions([]FilledField{
		{Field: FormField{Label: "Can you attend?", Type: "radio"}, Answer: "Yes, I'll be there"},
		{Field: FormField{Label: "What will you be bringing?", Type: "checkbox"}, Answer: "Mains"},
	}, model)
	if len(actions) != 2 {
		t.Fatalf("expected all planned answers to remain actionable when perception is unavailable, got %#v", actions)
	}
}

func TestBrowserNextPlannedAction_PrioritizesRequiredVisibleQuestion(t *testing.T) {
	model := BrowserFormPageModel{
		Questions: []BrowserFormQuestionState{
			{Field: FormField{Label: "Optional notes", Type: "textarea"}, Visible: true, Required: false, Filled: false},
			{Field: FormField{Label: "Can you attend?", Type: "radio", Required: true}, Visible: true, Required: true, Filled: false},
		},
	}
	action, ok := browserNextPlannedAction([]FilledField{
		{Field: FormField{Label: "Optional notes", Type: "textarea"}, Answer: "See you there"},
		{Field: FormField{Label: "Can you attend?", Type: "radio", Required: true}, Answer: "Yes, I'll be there"},
	}, model)
	if !ok {
		t.Fatal("expected a planned action")
	}
	if action.Field.Field.Label != "Can you attend?" {
		t.Fatalf("expected required visible question first, got %#v", action)
	}
}

func TestBrowserNextPlannedAction_UsesVisibleQuestionLabelForRecovery(t *testing.T) {
	model := BrowserFormPageModel{
		Questions: []BrowserFormQuestionState{
			{
				Field:    FormField{Label: "26. Email address", Type: "email", Required: true},
				Visible:  true,
				Required: true,
			},
		},
	}
	action, ok := browserNextPlannedAction([]FilledField{
		{Field: FormField{Label: "Email address", Type: "email", Required: true}, Answer: "mambacodes47@gmail.com", Semantic: SemanticEmail},
	}, model)
	if !ok {
		t.Fatal("expected a planned action")
	}
	if action.TargetLabel != "26. Email address" {
		t.Fatalf("expected browser question label to drive recovery, got %#v", action)
	}
}

func TestBrowserCompletionAuditMessage_SurfacesRequiredGaps(t *testing.T) {
	msg := browserCompletionAuditMessage(BrowserFormPageModel{
		Questions: []BrowserFormQuestionState{
			{Field: FormField{Label: "Name", Required: true}, Verified: true, Required: true, Answer: "Ntina"},
			{Field: FormField{Label: "Bring a dish", Required: true}, Verified: false, Required: true},
		},
		RequiredUnanswered: []string{"Bring a dish"},
	})
	if !strings.Contains(msg, "required question") || !strings.Contains(msg, "Bring a dish") {
		t.Fatalf("unexpected audit message: %q", msg)
	}
}

func TestBuildBrowserFormPageModelWithVision_AddsMissingRequiredQuestion(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		URL:   "https://docs.google.com/forms/d/e/example/viewform",
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "What is your name?", Role: "textbox", Value: "Ntina", Visible: true},
			{Label: "Submit", Role: "button", Visible: true},
		},
	}
	provider := &visionTestProvider{response: `{
		"title":"Party Invite",
		"submitAvailable":true,
		"nextAvailable":false,
		"questions":[
			{"label":"What is your name?","required":true,"visible":true,"answered":true,"answer":"Ntina","confidence":"HIGH"},
			{"label":"What will you be bringing?","type":"checkbox","required":true,"visible":true,"answered":false,"options":["Mains","Salad","Dessert"],"confidence":"HIGH"}
		]
	}`}
	browser := browserPerceptionStub{
		snapshot: snapshot,
		perception: BrowserPerception{
			Snapshot:   snapshot,
			Screenshot: []byte{1, 2, 3},
			CapturedAt: time.Now(),
		},
	}
	model := buildBrowserFormPageModelWithVision(provider, browser, snapshot, []FilledField{
		{Field: FormField{Label: "What is your name?", Type: "text", Required: true}, Answer: "Ntina"},
	})
	if !model.VisionUsed || !model.SubmitAvailable {
		t.Fatalf("expected vision to be fused into the model, got %#v", model)
	}
	if len(model.RequiredUnanswered) != 1 || model.RequiredUnanswered[0] != "What will you be bringing?" {
		t.Fatalf("expected missing required question from vision, got %#v", model.RequiredUnanswered)
	}
}

func TestResolveAssistantFormContext_AllowsDirectURLWithoutGmail(t *testing.T) {
	call := AssistantToolCall{
		Tool: "gmail.fill_form",
		Params: map[string]any{
			"form_url": "https://example.com/form",
		},
	}
	baseEmail, threadEmails, recent, formURL, err := resolveAssistantFormContext(nil, call, nil, nil)
	if err != nil {
		t.Fatalf("resolveAssistantFormContext returned error: %v", err)
	}
	if formURL != "https://example.com/form" {
		t.Fatalf("unexpected formURL %q", formURL)
	}
	if baseEmail.Subject != "Direct form link" {
		t.Fatalf("unexpected base email %#v", baseEmail)
	}
	if len(threadEmails) != 0 || len(recent) != 0 {
		t.Fatalf("expected no gmail-derived context, got thread=%#v recent=%#v", threadEmails, recent)
	}
}

func TestBuildBrowserFormPageModelWithVision_PreservesMicrosoftHydrationAndNextPageState(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		URL:   "https://forms.cloud.microsoft/pages/responsepage.aspx?id=example",
		Title: "Microsoft Forms",
		Elements: []BrowserPageElement{
			{Label: "Single line text", Role: "textbox", Visible: true},
			{Label: "Next", Role: "button", Visible: true},
		},
	}
	provider := &visionTestProvider{response: `{
		"title":"Recruit Training Squadron Pearson 786 MP Arrivals Form",
		"sectionTitle":"Page 1 of 2",
		"submitAvailable":false,
		"nextAvailable":true,
		"questions":[
			{"label":"What is your name?","type":"text","required":true,"visible":true,"answered":true,"answer":"Ntina","confidence":"HIGH"},
			{"label":"Service Number","type":"text","required":true,"visible":true,"answered":false,"confidence":"LOW"}
		]
	}`}
	browser := browserPerceptionStub{
		snapshot: snapshot,
		perception: BrowserPerception{
			Snapshot:   snapshot,
			Screenshot: []byte{1, 2, 3},
			CapturedAt: time.Now(),
		},
	}
	model := buildBrowserFormPageModelWithVision(provider, browser, snapshot, []FilledField{
		{Field: FormField{Label: "What is your name?", Type: "text", Required: true}, Answer: "Ntina"},
	})
	if !model.VisionUsed {
		t.Fatalf("expected vision to be fused into the model, got %#v", model)
	}
	if !model.NextAvailable || model.SubmitAvailable {
		t.Fatalf("expected next-page gating without submit, got %#v", model)
	}
	if model.SectionTitle != "Page 1 of 2" {
		t.Fatalf("expected vision section title to survive fusion, got %#v", model)
	}
	if len(model.RequiredUnanswered) != 1 || model.RequiredUnanswered[0] != "Service Number" {
		t.Fatalf("expected Microsoft-like delayed hydration to surface missing required field, got %#v", model.RequiredUnanswered)
	}
}

func TestBrowserCompletionAuditMessage_GatesSubmitOnNextPage(t *testing.T) {
	msg := browserCompletionAuditMessage(BrowserFormPageModel{
		NextAvailable:      true,
		SubmitAvailable:    false,
		RequiredUnanswered: []string{"Service Number"},
	})
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "review") || !strings.Contains(lower, "browser") {
		t.Fatalf("expected review-oriented guidance, got %q", msg)
	}
	if strings.Contains(lower, "submit") {
		t.Fatalf("expected submit to remain gated when next page is available, got %q", msg)
	}
}

func TestBrowserFieldAnswerVerified_RejectsPartialTextMatches(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		Title: "Party Invite",
		Elements: []BrowserPageElement{
			{Label: "Ntina Morrison", GroupLabel: "What is your name?", Role: "textbox", Value: "Ntina Morrison", Visible: true},
		},
	}
	if browserFieldAnswerVerified(snapshot, FormField{Label: "What is your name?", Type: "text"}, "Ntina") {
		t.Fatal("expected partial text match not to count as verified")
	}
}

func TestNewBrowserFormPlan_ClassifiesSemanticQuestions(t *testing.T) {
	link := FormLink{URL: "https://docs.google.com/forms/d/e/example/viewform", Label: "Party Invite"}
	snapshot := BrowserPageSnapshot{Title: "Party Invite", Text: "party invite"}
	plan := NewBrowserFormPlan(link, snapshot, []FormField{
		{Label: "First name", Type: "text", Required: true},
		{Label: "Do you have any allergies or dietary restrictions?", Type: "textarea"},
		{Label: "Can you attend?", Type: "radio", Options: []string{"Yes, I'll be there", "Sorry, can't make it"}},
	})
	if !plan.ReadyToReview {
		t.Fatal("expected browser plan to be ready for review")
	}
	if plan.Title != "Party Invite" || plan.Link.URL == "" {
		t.Fatalf("unexpected browser plan metadata: %#v", plan)
	}
	seen := map[SemanticType]bool{}
	for _, field := range plan.Fields {
		seen[field.Semantic] = true
	}
	if !seen[SemanticFirstName] {
		t.Fatalf("expected name question to be classified semantically, got %#v", plan.Fields)
	}
	if !seen[SemanticFreeText] {
		t.Fatalf("expected free-text question to be classified semantically, got %#v", plan.Fields)
	}
	if !seen[SemanticUnknown] {
		t.Fatalf("expected unknown semantic for freeform attendance question, got %#v", plan.Fields)
	}
}

func TestBrowserFormReviewRoundTrip_PreservesCompletionAudit(t *testing.T) {
	result := FormFillResult{
		Link:      FormLink{URL: "https://docs.google.com/forms/d/e/example/viewform"},
		FormTitle: "Party Invite",
		Fields: []FilledField{
			{Field: FormField{Label: "What is your name?", Required: true}, Answer: "Ntina", Approved: true},
			{Field: FormField{Label: "Can you attend?", Required: true}, Answer: "Yes, I'll be there", Approved: true},
			{Field: FormField{Label: "What will you be bringing?", Required: true}, Answer: "Salad", Approved: true},
		},
		ReadyToSubmit: true,
		Notes:         []string{"all required fields filled"},
	}
	review := BrowserFormReviewFromResult(result)
	back := review.ToFormFillResult()
	if !review.ReadyToSubmit || !back.ReadyToSubmit {
		t.Fatalf("expected ready-to-submit round trip, got review=%#v back=%#v", review, back)
	}
	if len(back.Fields) != len(result.Fields) {
		t.Fatalf("expected fields to survive round trip, got %#v", back.Fields)
	}
	if assistantFormNeedsFollowUp(back) {
		t.Fatalf("expected no follow-up required for complete audit, got %#v", back)
	}
}

func TestAssistantFormNeedsFollowUp_DetectsIncompleteRequiredFields(t *testing.T) {
	result := FormFillResult{
		FormTitle: "Party Invite",
		Fields: []FilledField{
			{Field: FormField{Label: "What is your name?", Required: true}, Answer: "Ntina", Approved: true},
			{Field: FormField{Label: "Can you attend?", Required: true}, Answer: "", Approved: false},
		},
		Notes: []string{"1 field(s) still need your review or manual input in the browser"},
	}
	if !assistantFormNeedsFollowUp(result) {
		t.Fatalf("expected incomplete form to require follow-up, got %#v", result)
	}
}

func TestBrowserFormFieldsFromSnapshot_SupplementsGoogleFormsVisibleText(t *testing.T) {
	snapshot := BrowserPageSnapshot{
		URL:   "https://docs.google.com/forms/d/e/example/viewform",
		Title: "T-Shirt Sign Up",
		Text: strings.Join([]string{
			"T-Shirt Sign Up",
			"colour preference",
			"*",
			"black",
			"white",
			"pink",
			"Name",
			"*",
			"Shirt size",
			"XS",
			"S",
			"M",
			"L",
			"XL",
			"Other thoughts or comments",
		}, "\n"),
	}
	fields := browserFormFieldsFromSnapshot(snapshot)
	if len(fields) < 4 {
		t.Fatalf("expected supplemental Google Form fields, got %#v", fields)
	}
	var colour *FormField
	for i := range fields {
		if strings.EqualFold(fields[i].Label, "colour preference") {
			colour = &fields[i]
			break
		}
	}
	if colour == nil || colour.Type != "radio" || len(colour.Options) < 3 {
		t.Fatalf("expected colour preference radio field, got %#v", fields)
	}
}

func TestFormRequiresSignIn(t *testing.T) {
	if !formRequiresSignIn(BrowserPageSnapshot{Text: "Sign in to continue. To fill out this form, you must be signed in."}) {
		t.Fatal("expected sign-in gate to be detected")
	}
	if formRequiresSignIn(BrowserPageSnapshot{Text: "Contact information\nSubmit"}) {
		t.Fatal("expected normal form to not require sign-in")
	}
}

func TestChooseFormLink_SelectsInteractiveChoice(t *testing.T) {
	links := []FormLink{
		{URL: "https://example.com/first", Label: "First form"},
		{URL: "https://example.com/second", Label: "Second form"},
	}
	var out bytes.Buffer
	chosen, err := chooseFormLink(strings.NewReader("2\n"), &out, links)
	if err != nil {
		t.Fatalf("chooseFormLink returned error: %v", err)
	}
	if chosen.URL != "https://example.com/second" {
		t.Fatalf("expected second link, got %#v", chosen)
	}
	if !strings.Contains(out.String(), "form links found:") || !strings.Contains(out.String(), "choose form") {
		t.Fatalf("expected interactive chooser output, got %q", out.String())
	}
}

func TestChooseFormLink_InvalidChoiceDefaultsToFirst(t *testing.T) {
	links := []FormLink{
		{URL: "https://example.com/first", Label: "First form"},
		{URL: "https://example.com/second", Label: "Second form"},
	}
	var out bytes.Buffer
	chosen, err := chooseFormLink(strings.NewReader("bogus\n"), &out, links)
	if err != nil {
		t.Fatalf("chooseFormLink returned error: %v", err)
	}
	if chosen.URL != "https://example.com/first" {
		t.Fatalf("expected first link fallback, got %#v", chosen)
	}
}

func TestRenderFormReview_ApprovalFlow(t *testing.T) {
	result := &FormFillResult{
		FormTitle: "Contact information",
		Fields: []FilledField{
			{
				Field:      FormField{Label: "Name", Type: "text", Required: true},
				Answer:     "Avery Stone",
				Confidence: ConfidenceHigh,
				Source:     "email signature",
			},
			{
				Field:      FormField{Label: "Comments", Type: "textarea", Required: false},
				Answer:     "Optional note",
				Confidence: ConfidenceLow,
				Source:     "inferred",
			},
			{
				Field:      FormField{Label: "Service Number", Type: "text", Required: true},
				Answer:     "",
				Confidence: ConfidenceUnknown,
				Source:     "",
			},
		},
	}
	var out bytes.Buffer
	in := strings.NewReader("y\ns\ne\nSN-001\n\ny\n")
	if err := RenderFormReview(&out, in, result); err != nil {
		t.Fatalf("RenderFormReview returned error: %v", err)
	}
	if !result.ReadyToSubmit {
		t.Fatalf("expected result to be ready to submit, got %#v", result)
	}
	if !result.Fields[0].Approved {
		t.Fatalf("expected first field approved, got %#v", result.Fields[0])
	}
	if !result.Fields[1].Skipped || result.Fields[1].Answer != "" {
		t.Fatalf("expected second field skipped and cleared, got %#v", result.Fields[1])
	}
	if !result.Fields[2].Approved || result.Fields[2].Answer != "SN-001" {
		t.Fatalf("expected third field edited and approved, got %#v", result.Fields[2])
	}
	if result.Fields[2].Source != "manual edit" {
		t.Fatalf("expected manual edit source, got %#v", result.Fields[2])
	}
	if !strings.Contains(out.String(), "ready to continue with manual handoff") {
		t.Fatalf("expected ready-to-submit review output, got %q", out.String())
	}
}

func TestManualInstructionSubmitter_CanSubmit(t *testing.T) {
	submitter := ManualInstructionSubmitter{}
	if submitter.CanSubmit(FormFillResult{}) {
		t.Fatal("expected empty result to be non-submittable")
	}
	if !submitter.CanSubmit(FormFillResult{Link: FormLink{URL: "https://docs.google.com/forms/d/e/example/viewform"}}) {
		t.Fatal("expected form url to be submittable")
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

func TestConfirmationRequired_ClearActions(t *testing.T) {
	tests := []string{
		"gmail.archive_thread",
		"gmail.mark_read",
		"gmail.star_thread",
	}
	for _, toolName := range tests {
		if !shouldConfirmAssistantTool(toolName) {
			t.Fatalf("expected %s to require confirmation", toolName)
		}
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

type visionTestProvider struct {
	response string
}

func (p *visionTestProvider) Name() string { return "vision-test" }
func (p *visionTestProvider) Chat(messages []Message, tools []Tool) (string, error) {
	return p.response, nil
}
func (p *visionTestProvider) IsAvailable() (bool, error) { return true, nil }
func (p *visionTestProvider) VisionChat(messages []VisionMessage, tools []Tool, format any) (string, error) {
	return p.response, nil
}

type browserPerceptionStub struct {
	snapshot   BrowserPageSnapshot
	perception BrowserPerception
}

func (b browserPerceptionStub) Open(url string) error                    { return nil }
func (b browserPerceptionStub) Snapshot() (BrowserPageSnapshot, error)   { return b.snapshot, nil }
func (b browserPerceptionStub) Click(target string) error                { return nil }
func (b browserPerceptionStub) Type(target string, value string) error   { return nil }
func (b browserPerceptionStub) Select(target string, value string) error { return nil }
func (b browserPerceptionStub) Submit() error                            { return nil }
func (b browserPerceptionStub) URL() string                              { return b.snapshot.URL }
func (b browserPerceptionStub) Close() error                             { return nil }
func (b browserPerceptionStub) Perceive() (BrowserPerception, error)     { return b.perception, nil }

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

func jpegWithComment(t *testing.T, comment string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg.Encode returned error: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		preview := data
		if len(preview) > 4 {
			preview = preview[:4]
		}
		t.Fatalf("expected valid jpeg header, got %x", preview)
	}
	commentBytes := []byte(comment)
	segmentLen := len(commentBytes) + 2
	segment := []byte{
		0xFF, 0xFE,
		byte(segmentLen >> 8),
		byte(segmentLen),
	}
	segment = append(segment, commentBytes...)
	out := make([]byte, 0, len(data)+len(segment))
	out = append(out, data[:2]...)
	out = append(out, segment...)
	out = append(out, data[2:]...)
	return out
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

func TestAssistantStatusLineForToolCall_RRSC(t *testing.T) {
	tests := []struct {
		name string
		call AssistantToolCall
		want string
	}{
		{
			name: "clear archive",
			call: AssistantToolCall{Tool: "gmail.archive_thread"},
			want: "clearing thread from inbox...",
		},
		{
			name: "clear mark read",
			call: AssistantToolCall{Tool: "gmail.mark_read"},
			want: "marking thread as read...",
		},
		{
			name: "reply draft",
			call: AssistantToolCall{Tool: "gmail.draft_reply"},
			want: "drafting reply...",
		},
		{
			name: "schedule availability",
			call: AssistantToolCall{Tool: "calendar.free_busy"},
			want: "checking calendar availability...",
		},
		{
			name: "schedule update",
			call: AssistantToolCall{Tool: "calendar.update_event"},
			want: "updating calendar event...",
		},
	}
	for _, test := range tests {
		if got := assistantStatusLineForToolCall("", test.call); got != test.want {
			t.Fatalf("%s: expected %q, got %q", test.name, test.want, got)
		}
	}
}

func TestAssistantTurnViewFromResult_GmailClearCard(t *testing.T) {
	turn := assistantTurnViewFromResult("archive this thread", &AssistantTurnResult{
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{Tool: "gmail.archive_thread"},
			Result: ToolResult{
				Success: true,
				Data: map[string]any{
					"operation": "archive_thread",
					"target": gmailLabelMutationTarget{
						ThreadID: "thread-1",
						Subject:  "Invoice March",
						From:     "Stripe",
					},
				},
				Text: "archived Invoice March from Stripe",
			},
		}},
	}, time.Now())
	if len(turn.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(turn.Cards))
	}
	card := turn.Cards[0]
	if card.Eyebrow != "Clear" {
		t.Fatalf("expected clear eyebrow, got %#v", card)
	}
	if card.Success != "archived Invoice March from Stripe" {
		t.Fatalf("expected success text, got %#v", card)
	}
}

func TestAssistantTurnViewFromResult_CalendarFindEventsCard(t *testing.T) {
	turn := assistantTurnViewFromResult("what is on my calendar?", &AssistantTurnResult{
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{Tool: "calendar.find_events"},
			Result: ToolResult{
				Success: true,
				Data: map[string]any{
					"events": []any{
						map[string]any{
							"calendarId": "primary",
							"summary":    "Standup",
							"location":   "Teams",
							"start":      map[string]any{"dateTime": "2026-04-02T15:00:00Z"},
							"end":        map[string]any{"dateTime": "2026-04-02T15:30:00Z"},
						},
					},
				},
				Text: "found 1 event",
			},
		}},
	}, time.Now())
	if len(turn.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(turn.Cards))
	}
	card := turn.Cards[0]
	if card.Eyebrow != "Schedule · 1 event" {
		t.Fatalf("unexpected calendar eyebrow: %#v", card)
	}
	if len(card.Rows) != 1 || card.Rows[0].Subject != "Standup" {
		t.Fatalf("expected standup row, got %#v", card.Rows)
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
	if strings.TrimSpace(cfg.BrowserProfilePath) == "" {
		t.Fatalf("expected default browser profile path, got %#v", cfg)
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

func TestParseAssistantInvocation_BrowserCommand(t *testing.T) {
	configRoot := t.TempDir()
	setAssistantConfigEnv(t, configRoot)

	inv, err := parseAssistantInvocation([]string{"browser", "connect"})
	if err != nil {
		t.Fatalf("parseAssistantInvocation returned error: %v", err)
	}
	if inv.Command != "browser" {
		t.Fatalf("expected browser command, got %#v", inv)
	}
	if len(inv.Args) != 1 || inv.Args[0] != "connect" {
		t.Fatalf("expected browser connect args, got %#v", inv.Args)
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

func TestAssistantNeedsOnboarding_BrowserPending(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "gmail_token.json")
	if err := os.WriteFile(tokenPath, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := AssistantConfig{
		Provider:         "ollama",
		Model:            "llama3.2",
		OllamaURL:        "http://localhost:11434",
		GmailTokenPath:   tokenPath,
		BrowserOnboarded: false,
	}
	if !assistantNeedsOnboarding(cfg) {
		t.Fatalf("expected browser onboarding to still be required, got %#v", cfg)
	}
	cfg.BrowserOnboarded = true
	cfg.BrowserEnabled = false
	if assistantNeedsOnboarding(cfg) {
		t.Fatalf("expected onboarding to be complete after browser step is explicitly finished, got %#v", cfg)
	}
}

func TestToolResultMessageContent_SummarizesEmailsForHistory(t *testing.T) {
	result := ToolResult{
		Success: true,
		Data: []NormalizedEmail{{
			ID:       "msg-1",
			ThreadID: "thread-1",
			From:     `"Alice" <alice@example.com>`,
			Subject:  "Quarterly update",
			Snippet:  "Topline summary",
			BodyText: strings.Repeat("very long body ", 500),
			Unread:   true,
		}},
	}
	content := toolResultMessageContent(result)
	if strings.Contains(content, "very long body") {
		t.Fatalf("expected email body to be omitted from history summary, got %q", content)
	}
	if !strings.Contains(content, "Quarterly update") || !strings.Contains(content, "Topline summary") {
		t.Fatalf("expected compact email summary, got %q", content)
	}
}

func TestAssistantSession_CloneHistory_TrimsAndSummarizes(t *testing.T) {
	session := NewAssistantSession(&sequentialTestProvider{}, nil, AssistantConfig{DefaultFormat: "text"})
	for i := 0; i < assistantHistoryMaxMessages+4; i++ {
		session.appendHistory(Message{
			Role:    "assistant",
			Content: fmt.Sprintf("message-%02d %s", i, strings.Repeat("x", assistantHistoryMessageMaxChars)),
		})
	}
	history := session.CloneHistory()
	if len(history) != assistantHistoryMaxMessages {
		t.Fatalf("expected history to be trimmed to %d, got %d", assistantHistoryMaxMessages, len(history))
	}
	if strings.Contains(history[0].Content, "message-00") {
		t.Fatalf("expected oldest messages to be trimmed, got first message %q", history[0].Content)
	}
	for _, message := range history {
		if len(message.Content) > assistantHistoryMessageMaxChars+3 {
			t.Fatalf("expected message content to be truncated, got length %d", len(message.Content))
		}
	}
}

func TestAssistantBrowserLooksSignedIn(t *testing.T) {
	tests := []struct {
		name     string
		snapshot BrowserPageSnapshot
		want     bool
	}{
		{
			name: "signed in",
			snapshot: BrowserPageSnapshot{
				URL:   "https://myaccount.google.com/",
				Title: "Google Account",
				Text:  "Google Account Personal info Privacy Security",
			},
			want: true,
		},
		{
			name: "sign in gate",
			snapshot: BrowserPageSnapshot{
				URL:   "https://accounts.google.com/",
				Title: "Sign in",
				Text:  "Sign in to continue",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := assistantBrowserLooksSignedIn(tt.snapshot); got != tt.want {
				t.Fatalf("assistantBrowserLooksSignedIn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchForAnswer_UsesDirectInstructionOptionMatch(t *testing.T) {
	answer, source, confidence, reasoning := SearchForAnswer(
		SemanticUnknown,
		FormField{
			Label:   "Colour preference",
			Type:    "radio",
			Options: []string{"black", "white", "pink"},
		},
		"T-Shirt Sign Up",
		nil,
		nil,
		[]string{"finish filling the form, colour preference is pink, shirt size small and ask them if i can get a jumper too."},
		nil,
	)
	if answer != "pink" {
		t.Fatalf("expected pink from direct instruction, got %q", answer)
	}
	if confidence != ConfidenceHigh || !strings.Contains(source, "instruction") || reasoning == "" {
		t.Fatalf("expected high-confidence instruction source, got source=%q confidence=%q reasoning=%q", source, confidence, reasoning)
	}
}

func TestSearchForAnswer_UsesDirectInstructionFreeText(t *testing.T) {
	answer, source, confidence, _ := SearchForAnswer(
		SemanticFreeText,
		FormField{Label: "Questions", Type: "textarea"},
		"T-Shirt Sign Up",
		nil,
		nil,
		[]string{"finish filling the form, colour preference is pink, shirt size small and ask them if i can get a jumper too."},
		nil,
	)
	if !strings.Contains(strings.ToLower(answer), "jumper") {
		t.Fatalf("expected free-text question from direct instruction, got %q", answer)
	}
	if confidence != ConfidenceHigh || !strings.Contains(source, "instruction") {
		t.Fatalf("expected high-confidence instruction source, got source=%q confidence=%q", source, confidence)
	}
}

func TestSearchForAnswer_UsesExplicitOptionFromEmailContext(t *testing.T) {
	answer, source, confidence, reasoning := SearchForAnswer(
		SemanticUnknown,
		FormField{
			Label:   "What will you be bringing?",
			Type:    "checkbox",
			Options: []string{"Mains", "Salad", "Dessert", "Drinks", "Sides/Appetizers", "Other"},
		},
		"Party Invite",
		[]NormalizedEmail{{
			Subject:  "party invite",
			From:     `"Palma" <palma@example.com>`,
			BodyText: "Fill in for you and your gf Fiona. am looking forward to trying your salad",
		}},
		nil,
		nil,
		nil,
	)
	if answer != "Salad" {
		t.Fatalf("expected Salad from explicit email context, got %q", answer)
	}
	if confidence != ConfidenceHigh || source == "" || reasoning == "" {
		t.Fatalf("expected confident grounded answer, got source=%q confidence=%q reasoning=%q", source, confidence, reasoning)
	}
}

func TestSearchForAnswer_UsesDirectInstructionShortOptionAndTypo(t *testing.T) {
	answer, source, confidence, _ := SearchForAnswer(
		SemanticUnknown,
		FormField{
			Label:   "Shirt size",
			Type:    "radio",
			Options: []string{"XS", "S", "M", "L", "XL"},
		},
		"T-Shirt Sign Up",
		nil,
		nil,
		[]string{"finish filling the form, colour preference is wthite, shirt size small and ask them if i can get a jumper too."},
		nil,
	)
	if answer != "S" {
		t.Fatalf("expected S from small instruction, got %q", answer)
	}
	if confidence != ConfidenceHigh || !strings.Contains(source, "instruction") {
		t.Fatalf("expected high-confidence instruction source, got source=%q confidence=%q", source, confidence)
	}
}

func TestAssistantPendingFormFillFromTurn(t *testing.T) {
	turn := &AssistantTurnResult{
		Executions: []AssistantToolExecution{{
			Call: AssistantToolCall{
				Tool:   "gmail.fill_form",
				Params: map[string]any{"message_id": "msg-1", "thread_id": "thr-1"},
			},
			Result: ToolResult{
				Success: true,
				Data: FormFillResult{
					Link:      FormLink{URL: "https://docs.google.com/forms/d/e/example/viewform", MessageID: "msg-1"},
					FormTitle: "T-Shirt Sign Up",
					Fields: []FilledField{
						{Field: FormField{Label: "Name"}, Answer: "mamba"},
						{Field: FormField{Label: "Colour preference"}, Answer: ""},
					},
					Notes: []string{"2 field(s) still need your review or manual input in the browser"},
				},
			},
		}},
	}
	pending := assistantPendingFormFillFromTurn(turn)
	if pending == nil {
		t.Fatal("expected pending form fill")
	}
	if pending.FormURL == "" || pending.MessageID != "msg-1" || pending.ThreadID != "thr-1" {
		t.Fatalf("unexpected pending form fill: %#v", pending)
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
