package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type AssistantConsoleRenderer struct {
	w       io.Writer
	ui      termUI
	Format  string
	Verbose bool
}

func NewAssistantConsoleRenderer(w io.Writer, format string, verbose bool) AssistantConsoleRenderer {
	return AssistantConsoleRenderer{
		w:       w,
		ui:      newTermUI(w),
		Format:  strings.ToLower(strings.TrimSpace(format)),
		Verbose: verbose,
	}
}

type AssistantAttachmentView struct {
	Name         string
	MimeType     string
	SizeBytes    int64
	DownloadURL  string
	AttachmentID string
}

type AssistantMessageView struct {
	Role        string
	Author      string
	Tool        string
	At          time.Time
	Content     string
	Attachments []AssistantAttachmentView
}

type AssistantThreadView struct {
	ID          string
	Sender      string
	Subject     string
	Snippet     string
	Timestamp   time.Time
	Unread      bool
	Expanded    bool
	Messages    []AssistantMessageView
	Attachments []AssistantAttachmentView
	Actions     []AssistantActionView
}

type AssistantActionView struct {
	ID           string
	Kind         string
	Title        string
	Detail       string
	Status       string
	ConfirmLabel string
	ConfirmURL   string
	Completed    bool
	Pending      bool
}

type AssistantQuickPrompt struct {
	Label  string
	Prompt string
}

type AssistantInlineButtonView struct {
	ID    string
	Label string
	Tone  string
}

type AssistantTurnView struct {
	Prompt      string
	StatusLines []string
	Cards       []AssistantCardView
}

type AssistantCardView struct {
	ID      string
	Kind    string
	Eyebrow string
	Title   string
	Body    string
	Rows    []AssistantCardRowView
	Draft   *AssistantDraftView
	Event   *AssistantEventView
	Buttons []AssistantInlineButtonView
	Success string
	Note    string
}

type AssistantCardRowView struct {
	Index   int
	Sender  string
	Subject string
	Detail  string
	Meta    string
}

type AssistantDraftView struct {
	To      string
	Subject string
	Body    string
}

type AssistantEventView struct {
	Title    string
	When     string
	Calendar string
	Context  string
}

type AssistantChatEntry struct {
	Role    string
	Text    string
	At      time.Time
	Tool    string
	Success bool
}

type AssistantToolEvent struct {
	Tool      string
	Params    any
	Result    any
	Error     string
	At        time.Time
	Important bool
}

type AssistantPageData struct {
	Title           string
	Subtitle        string
	Provider        string
	Model           string
	Format          string
	Intro           string
	UnreadCount     int
	Turns           []AssistantTurnView
	Threads         []AssistantThreadView
	Actions         []AssistantActionView
	Chat            []AssistantChatEntry
	ToolEvents      []AssistantToolEvent
	QuickPrompts    []AssistantQuickPrompt
	Status          []string
	ChatPlaceholder string
	SubmitURL       string
	ActionURL       string
	RefreshURL      string
	GeneratedAt     time.Time
}

type AssistantViewerBackend interface {
	Snapshot() AssistantPageData
	SubmitChat(message string) (AssistantPageData, error)
	ConfirmAction(actionID string) (AssistantPageData, error)
}

func (r AssistantConsoleRenderer) Banner(title, subtitle string) error {
	if r.Format == "json" {
		return nil
	}
	if _, err := fmt.Fprint(r.w, r.ui.header(title)); err != nil {
		return err
	}
	if subtitle != "" {
		if _, err := fmt.Fprintln(r.w, "  "+r.ui.tdim(subtitle)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w, ""); err != nil {
			return err
		}
	}
	return nil
}

func (r AssistantConsoleRenderer) Status(provider, model, capabilitySummary string) error {
	if r.Format == "json" {
		return r.WriteJSON(map[string]any{
			"provider":     provider,
			"model":        model,
			"capabilities": capabilitySummary,
		})
	}
	if err := r.Banner("Assistant Status", ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.w, "  "+r.ui.tbold("provider: ")+provider); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.w, "  "+r.ui.tdim("model: "+model)); err != nil {
		return err
	}
	if capabilitySummary != "" {
		if _, err := fmt.Fprintln(r.w, "  "+r.ui.tdim("capabilities: "+capabilitySummary)); err != nil {
			return err
		}
	}
	return nil
}

func (r AssistantConsoleRenderer) RenderThreads(title string, threads []AssistantThreadView) error {
	if r.Format == "json" {
		return r.WriteJSON(map[string]any{"title": title, "threads": threads})
	}
	if err := r.Banner(title, ""); err != nil {
		return err
	}
	if len(threads) == 0 {
		_, err := fmt.Fprintln(r.w, "  "+r.ui.tdim("no messages matched the current filter"))
		return err
	}
	for i, thread := range threads {
		name := thread.Sender
		if thread.Unread {
			name = "● " + name
		}
		subject := thread.Subject
		if thread.Snippet != "" {
			subject += " — " + thread.Snippet
		}
		if _, err := fmt.Fprintln(r.w, r.ui.listItem(i+1, name, subject, assistantFormatClock(thread.Timestamp))); err != nil {
			return err
		}
	}
	return nil
}

func (r AssistantConsoleRenderer) RenderActions(title string, actions []AssistantActionView) error {
	if r.Format == "json" {
		return r.WriteJSON(map[string]any{
			"title":   title,
			"actions": actions,
		})
	}

	if len(actions) == 0 {
		return nil
	}

	dim := "\x1b[90m"
	reset := "\x1b[0m"
	text := "\x1b[38;2;216;210;200m"

	if _, err := fmt.Fprintln(r.w); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(r.w, "  %s%s%s\n", dim, strings.ToLower(title), reset); err != nil {
		return err
	}

	for _, action := range actions {
		if _, err := fmt.Fprintf(
			r.w,
			"  %s→%s  %s%s%s — %s\n",
			dim, reset,
			text, action.Title, reset,
			action.Detail,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r AssistantConsoleRenderer) ToolCall(tool string, params any) error {
	if !r.Verbose || r.Format == "json" {
		return nil
	}
	line, err := assistantJSONLine(params)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.w, "  [tool] %s %s\n", tool, line)
	return err
}

func (r AssistantConsoleRenderer) ToolResult(summary string) error {
	if !r.Verbose || r.Format == "json" {
		return nil
	}
	_, err := fmt.Fprintf(r.w, "  [done] %s\n", summary)
	return err
}

func (r AssistantConsoleRenderer) ConfirmationPrompt(description string) error {
	if r.Format == "json" {
		return r.WriteJSON(map[string]any{
			"confirmation_required": true,
			"description":           description,
		})
	}
	_, err := fmt.Fprint(r.w, renderConfirmationPrompt(ConfirmationRequest{Description: description}))
	return err
}

func (r AssistantConsoleRenderer) WriteJSON(payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.w, string(data))
	return err
}

func RenderAssistantTurn(prompt string, result *AssistantTurnResult, provider ModelProvider, format string, now time.Time) (string, error) {
	if normalizeAssistantFormat(format) == "json" {
		return FormatAssistantTurnResult(result, format)
	}
	if now.IsZero() {
		now = time.Now()
	}
	turn := assistantTurnViewFromResult(prompt, result, now)
	return renderAssistantTerminalTurn(turn), nil
}

func renderAssistantStatusLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if strings.HasPrefix(line, "✓ ") {
		return "  \x1b[38;2;90;175;89m" + line + "\x1b[0m"
	}
	return "  \x1b[90m\x1b[3m" + line + "\x1b[0m"
}

func (r AssistantConsoleRenderer) RenderFinal(summary string, actions []AssistantActionView, payload any) error {
	if r.Format == "json" {
		if payload == nil {
			payload = map[string]any{"summary": summary, "actions": actions}
		}
		return r.WriteJSON(payload)
	}
	if summary != "" {
		if _, err := fmt.Fprintln(r.w, summary); err != nil {
			return err
		}
	}
	return r.RenderActions("action needed:", actions)
}

func renderAssistantTerminalTurn(turn AssistantTurnView) string {
	var b strings.Builder
	ui := termUI{}

	// Status lines (e.g. "reading Gmail — searching unread, today...")
	for _, line := range turn.StatusLines {
		line = strings.TrimRight(line, "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(renderAssistantStatusLine(line) + "\n")
	}

	// One blank line between status lines and first card
	if len(turn.StatusLines) > 0 && len(turn.Cards) > 0 {
		b.WriteString("\n")
	}

	for i, card := range turn.Cards {
		// Blank line between cards
		if i > 0 {
			b.WriteString("\n")
		}
		renderAssistantTerminalCard(&b, ui, card)
	}

	return strings.TrimSpace(b.String())
}

func renderAssistantTerminalCard(b *strings.Builder, _ termUI, card AssistantCardView) {
	if renderAssistantTerminalCardMarkdownAware(b, card) {
		return
	}
	accent := "\x1b[38;2;196;168;130m"
	dim := "\x1b[90m"
	green := "\x1b[38;2;90;175;89m"
	reset := "\x1b[0m"
	text := "\x1b[38;2;216;210;200m"

	// writeLine writes a line followed by a newline.
	// Pass an empty string to emit a blank line for spacing.
	writeLine := func(line string) {
		line = strings.TrimRight(line, "\n")
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// writeKV writes a dim label and a bright value on one line, e.g.:
	//   to   alice.chen@company.com
	writeKV := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		writeLine(fmt.Sprintf("  %s%-7s%s %s%s%s", dim, label, reset, text, value, reset))
	}

	switch {

	// ── Email / attachment rows ──────────────────────────────────────────────
	//
	//   GMAIL · 4 UNREAD
	//
	//   1  Stripe        Your invoice for March is ready — $240.00   9:14am
	//   2  Alice Chen    Re: auth refactor — can we push to Friday?  10:32am
	//
	case len(card.Rows) > 0:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		for _, row := range card.Rows {
			subject := strings.TrimSpace(row.Subject)
			detail := strings.TrimSpace(row.Detail)
			if subject != "" && detail != "" {
				subject += " — " + detail
			} else if detail != "" {
				subject = detail
			}
			// Truncate long subject so it doesn't wrap
			if len([]rune(subject)) > 52 {
				subject = string([]rune(subject)[:51]) + "…"
			}
			sender := strings.TrimSpace(row.Sender)
			if len([]rune(sender)) > 14 {
				sender = string([]rune(sender)[:13]) + "…"
			}
			meta := strings.TrimSpace(row.Meta)
			// Plain columns: index  sender  subject  meta
			// No borders — just spacing and color do the work
			line := fmt.Sprintf("  %s%-2d%s  %s%-14s%s  %s%-52s%s",
				dim, row.Index, reset,
				accent, sender, reset,
				text, subject, reset,
			)
			if meta != "" {
				line += "  " + dim + meta + reset
			}
			writeLine(line)
		}

	// ── Draft reply ──────────────────────────────────────────────────────────
	//
	//   DRAFT · READY FOR REVIEW
	//
	//   to      alice.chen@company.com
	//   re      Re: auth refactor — can we push to Friday?
	//   ────────────────────────────────
	//   Hi Alice, Friday works — take the time you need...
	//
	case card.Draft != nil:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		writeKV("to", card.Draft.To)
		writeKV("re", card.Draft.Subject)
		if strings.TrimSpace(card.Draft.Body) != "" {
			writeLine("")
			writeLine("  " + dim + strings.Repeat("─", 32) + reset)
			writeLine("")
			for _, bodyLine := range renderAssistantTerminalMarkdownLines(card.Draft.Body, "  ") {
				writeLine(bodyLine)
			}
		}

	// ── Calendar event ───────────────────────────────────────────────────────
	//
	//   CALENDAR · EVENT READY
	//
	//   from    Ben K. — standup moved to Thursday 3pm
	//   title   standup — team
	//   when    Thursday Mar 27 · 3:00–3:30pm
	//   cal     primary
	//
	case card.Event != nil:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		writeKV("from", card.Event.Context)
		writeKV("title", card.Event.Title)
		writeKV("when", card.Event.When)
		writeKV("cal", card.Event.Calendar)

	// ── Default: text body / thread / summary ────────────────────────────────
	default:
		if card.Kind == "semantic-summary" && strings.TrimSpace(card.Note) != "" {
			writeLine("  " + dim + "action needed:" + reset + " " + strings.TrimSpace(card.Note))
		} else {
			if strings.TrimSpace(card.Eyebrow) != "" {
				writeLine("  " + accent + card.Eyebrow + reset)
				writeLine("")
			}
			if strings.TrimSpace(card.Title) != "" {
				writeLine("  " + dim + "subject: " + reset + text + card.Title + reset)
				writeLine("")
			}
			// Body uses a single left-margin bar — clean and terminal-native
			if card.Kind != "attachment-save" && strings.TrimSpace(card.Body) != "" {
				for _, line := range strings.Split(card.Body, "\n") {
					writeLine(fmt.Sprintf("  %s│%s %s%s%s", accent, reset, text, line, reset))
				}
			}
			// Note (only here — not duplicated in the success block below)
			if strings.TrimSpace(card.Note) != "" && strings.TrimSpace(card.Success) == "" {
				writeLine("")
				writeLine("  " + dim + strings.TrimSpace(card.Note) + reset)
			}
		}
	}

	// ── Action prompt ────────────────────────────────────────────────────────
	// Blank line before prompt, then the [y]/[e]/[n] line.
	// The web-UI button labels (card.Buttons[].Label) are intentionally
	// not printed here — assistantTerminalButtonPrompt handles the CLI line.
	if len(card.Buttons) > 0 {
		if prompt := assistantTerminalButtonPrompt(card); prompt != "" {
			writeLine("")
			writeLine(prompt)
		}
	}

	// ── Success confirmation ─────────────────────────────────────────────────
	//   ✓ 3 files saved to ~/invoices/2026-03/
	//   next time: jot assistant "download invoice attachments"
	if strings.TrimSpace(card.Success) != "" {
		writeLine("")
		writeLine("  " + green + "✓ " + card.Success + reset)
		if strings.TrimSpace(card.Note) != "" {
			writeLine("  " + dim + strings.TrimSpace(card.Note) + reset)
		}
	}
}

func renderAssistantTerminalCardMarkdownAware(b *strings.Builder, card AssistantCardView) bool {
	accent := "\x1b[38;2;196;168;130m"
	dim := "\x1b[90m"
	green := "\x1b[38;2;90;175;89m"
	text := "\x1b[38;2;216;210;200m"
	reset := "\x1b[0m"

	writeLine := func(line string) {
		line = strings.TrimRight(line, "\n")
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	writeKV := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		writeLine(fmt.Sprintf("  %s%-7s%s %s%s%s", dim, label, reset, text, value, reset))
	}

	switch {
	case len(card.Rows) > 0:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		for _, row := range card.Rows {
			subject := strings.TrimSpace(row.Subject)
			if detail := strings.TrimSpace(row.Detail); detail != "" {
				if subject != "" {
					subject += " - " + detail
				} else {
					subject = detail
				}
			}
			if len([]rune(subject)) > 52 {
				subject = string([]rune(subject)[:49]) + "..."
			}
			sender := strings.TrimSpace(row.Sender)
			if len([]rune(sender)) > 14 {
				sender = string([]rune(sender)[:11]) + "..."
			}
			line := fmt.Sprintf("  %s%-2d%s  %s%-14s%s  %s%-52s%s",
				dim, row.Index, reset,
				accent, sender, reset,
				text, subject, reset,
			)
			if meta := strings.TrimSpace(row.Meta); meta != "" {
				line += "  " + dim + meta + reset
			}
			writeLine(line)
		}
	case card.Draft != nil:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		writeKV("to", card.Draft.To)
		writeKV("re", card.Draft.Subject)
		if strings.TrimSpace(card.Draft.Body) != "" {
			writeLine("")
			writeLine("  " + dim + strings.Repeat("-", 32) + reset)
			writeLine("")
			for _, line := range renderAssistantTerminalMarkdownLines(card.Draft.Body, "  ") {
				writeLine(line)
			}
		}
	case card.Event != nil:
		if strings.TrimSpace(card.Eyebrow) != "" {
			writeLine("  " + accent + card.Eyebrow + reset)
			writeLine("")
		}
		writeKV("from", card.Event.Context)
		writeKV("title", card.Event.Title)
		writeKV("when", card.Event.When)
		writeKV("cal", card.Event.Calendar)
	default:
		if card.Kind == "semantic-summary" && strings.TrimSpace(card.Note) != "" {
			writeLine("  " + dim + "action needed:" + reset + " " + strings.TrimSpace(card.Note))
		} else {
			if strings.TrimSpace(card.Eyebrow) != "" {
				writeLine("  " + accent + card.Eyebrow + reset)
				writeLine("")
			}
			if strings.TrimSpace(card.Title) != "" {
				writeLine("  " + dim + "subject: " + reset + text + card.Title + reset)
				writeLine("")
			}
			if card.Kind != "attachment-save" && strings.TrimSpace(card.Body) != "" {
				for _, line := range renderAssistantTerminalMarkdownLines(card.Body, "  ") {
					writeLine(line)
				}
			}
			if strings.TrimSpace(card.Note) != "" && strings.TrimSpace(card.Success) == "" {
				writeLine("")
				writeLine("  " + dim + strings.TrimSpace(card.Note) + reset)
			}
		}
	}

	if len(card.Buttons) > 0 {
		if prompt := assistantTerminalButtonPrompt(card); prompt != "" {
			writeLine("")
			writeLine(prompt)
		}
	}
	if strings.TrimSpace(card.Success) != "" {
		writeLine("")
		writeLine("  " + green + "OK " + card.Success + reset)
		if strings.TrimSpace(card.Note) != "" {
			writeLine("  " + dim + strings.TrimSpace(card.Note) + reset)
		}
	}
	return true
}

func renderAssistantTerminalMarkdownLines(body, indent string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := assistantTrimMarkdownPrefix(raw)
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out = append(out, "")
		case assistantMarkdownIsRule(trimmed):
			out = append(out, indent+assistantTerminalDim(strings.Repeat("-", 36))+"\x1b[0m")
		case assistantMarkdownIsTableDivider(trimmed):
			continue
		case strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2:
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, indent+assistantRenderMarkdownTableRow(trimmed))
		case assistantMarkdownHeadingLevel(trimmed) > 0:
			level := assistantMarkdownHeadingLevel(trimmed)
			title := strings.TrimSpace(trimmed[level:])
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, indent+assistantStyleMarkdownHeading(level, title))
		default:
			if number, content := assistantMarkdownOrderedContent(trimmed); content != "" {
				out = append(out, fmt.Sprintf("%s%s%s%s %s\x1b[0m", indent, assistantTerminalDim(number+"."), "\x1b[0m", "", assistantRenderInlineMarkdown(content)))
				continue
			}
			if content := assistantMarkdownBulletContent(trimmed); content != "" {
				out = append(out, indent+assistantTerminalAccent("- ")+"\x1b[0m"+assistantRenderInlineMarkdown(content)+"\x1b[0m")
				continue
			}
			out = append(out, indent+assistantRenderInlineMarkdown(trimmed)+"\x1b[0m")
		}
	}
	return assistantCompactRenderedLines(out)
}

func assistantTrimMarkdownPrefix(line string) string {
	line = strings.TrimRight(line, " \t")
	trimmed := strings.TrimLeft(line, " \t")
	for {
		switch {
		case strings.HasPrefix(trimmed, "│"):
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "│"))
		case strings.HasPrefix(trimmed, ">"):
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		default:
			return trimmed
		}
	}
}

func assistantMarkdownIsRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, r := range line {
		if r != '-' && r != '*' && r != '_' && r != ' ' {
			return false
		}
	}
	return true
}

func assistantMarkdownHeadingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0
	}
	if level < len(line) && line[level] == ' ' {
		return level
	}
	return 0
}

func assistantMarkdownBulletContent(line string) string {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return strings.TrimSpace(line[2:])
	}
	return ""
}

func assistantMarkdownOrderedContent(line string) (string, string) {
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits+1 >= len(line) || line[digits] != '.' || line[digits+1] != ' ' {
		return "", ""
	}
	return line[:digits], strings.TrimSpace(line[digits+2:])
}

func assistantMarkdownIsTableDivider(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	for _, r := range line {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return true
}

func assistantRenderMarkdownTableRow(line string) string {
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cells = append(cells, assistantRenderInlineMarkdown(part))
	}
	if len(cells) > 3 {
		cells = append(cells[:3], assistantTerminalDim(fmt.Sprintf("+%d more", len(cells)-3))+"\x1b[0m")
	}
	if len(cells) > 0 {
		cells[0] = "\x1b[1m" + cells[0] + "\x1b[0m"
	}
	return strings.Join(cells, "  "+assistantTerminalDim("|")+"\x1b[0m"+"  ") + "\x1b[0m"
}

func assistantStyleMarkdownHeading(level int, title string) string {
	title = assistantRenderInlineMarkdown(title)
	switch level {
	case 1:
		return assistantTerminalAccent("\x1b[1m"+title+"\x1b[0m") + "\x1b[0m"
	case 2:
		return assistantTerminalAccent(title) + "\x1b[0m"
	default:
		return "\x1b[1m" + assistantTerminalDim(title) + "\x1b[0m"
	}
}

func assistantRenderInlineMarkdown(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "**"):
			if end := strings.Index(text[i+2:], "**"); end >= 0 {
				b.WriteString("\x1b[1m")
				b.WriteString(assistantRenderInlineMarkdown(text[i+2 : i+2+end]))
				b.WriteString("\x1b[0m")
				i += 4 + end
				continue
			}
		case strings.HasPrefix(text[i:], "__"):
			if end := strings.Index(text[i+2:], "__"); end >= 0 {
				b.WriteString("\x1b[1m")
				b.WriteString(assistantRenderInlineMarkdown(text[i+2 : i+2+end]))
				b.WriteString("\x1b[0m")
				i += 4 + end
				continue
			}
		case text[i] == '`':
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				b.WriteString("\x1b[38;2;90;175;89m")
				b.WriteString(text[i+1 : i+1+end])
				b.WriteString("\x1b[0m")
				i += 2 + end
				continue
			}
		case text[i] == '*' && i+1 < len(text):
			if end := strings.IndexByte(text[i+1:], '*'); end >= 0 {
				b.WriteString("\x1b[3m")
				b.WriteString(assistantRenderInlineMarkdown(text[i+1 : i+1+end]))
				b.WriteString("\x1b[0m")
				i += 2 + end
				continue
			}
		case text[i] == '_' && i+1 < len(text):
			if end := strings.IndexByte(text[i+1:], '_'); end >= 0 {
				b.WriteString("\x1b[3m")
				b.WriteString(assistantRenderInlineMarkdown(text[i+1 : i+1+end]))
				b.WriteString("\x1b[0m")
				i += 2 + end
				continue
			}
		case text[i] == '[':
			if mid := strings.IndexByte(text[i+1:], ']'); mid >= 0 {
				after := i + mid + 2
				if after < len(text) && text[after] == '(' {
					if end := strings.IndexByte(text[after+1:], ')'); end >= 0 {
						label := text[i+1 : i+1+mid]
						url := text[after+1 : after+1+end]
						b.WriteString(assistantRenderInlineMarkdown(label))
						if strings.TrimSpace(url) != "" {
							b.WriteString(" ")
							b.WriteString(assistantTerminalDim("(" + url + ")"))
							b.WriteString("\x1b[0m")
						}
						i = after + 2 + end
						continue
					}
				}
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

func assistantTerminalAccent(text string) string {
	return "\x1b[38;2;196;168;130m" + text
}

func assistantTerminalDim(text string) string {
	return "\x1b[90m" + text
}

func assistantCompactRenderedLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return out
}

func renderAssistantStreamingMarkdownChunk(text string, final bool) (string, int) {
	if text == "" {
		return "", 0
	}

	consume := 0
	if final {
		consume = len(text)
	} else if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		consume = idx + 1
	} else if idx := assistantStreamingFlushBoundary(text); idx > 0 {
		consume = idx
	}
	if consume <= 0 {
		return "", 0
	}

	segment := text[:consume]
	if strings.Contains(segment, "\n") {
		trailingNewline := strings.HasSuffix(segment, "\n")
		trimmed := strings.TrimSuffix(segment, "\n")
		lines := renderAssistantTerminalMarkdownLines(trimmed, "")
		rendered := strings.Join(lines, "\n")
		if trailingNewline {
			rendered += "\n"
		}
		return rendered, consume
	}

	return assistantRenderInlineMarkdown(segment) + "\x1b[0m", consume
}

func assistantStreamingFlushBoundary(text string) int {
	if len(text) < 48 {
		return 0
	}

	if strings.ContainsAny(text, "#*`_[|") && len(text) < 240 {
		return 0
	}

	best := -1
	for i := 0; i < len(text)-1; i++ {
		switch text[i] {
		case '.', '!', '?', ':':
			if text[i+1] == ' ' || text[i+1] == '\t' {
				best = i + 1
			}
		}
	}
	if best > 0 {
		return best
	}

	if len(text) >= 160 {
		if idx := strings.LastIndex(text, " "); idx >= 120 {
			return idx + 1
		}
	}
	return 0
}

func assistantTerminalButtonPrompt(card AssistantCardView) string {
	accent := "\x1b[38;2;196;168;130m"
	dim := "\x1b[90m"
	green := "\x1b[38;2;90;175;89m"
	reset := "\x1b[0m"

	yBtn := green + "[y]" + reset
	nBtn := dim + "[n]" + reset

	switch card.Kind {
	case "draft":
		eBtn := accent + "[e]" + reset
		return fmt.Sprintf("  %ssend this?%s  %s send   %s edit   %s discard",
			dim, reset, yBtn, eBtn, nBtn)
	case "event":
		return fmt.Sprintf("  %screate this event?%s  %s create   %s skip",
			dim, reset, yBtn, nBtn)
	case "attachment-save":
		label := "save attachments?"
		if strings.TrimSpace(card.Body) != "" {
			label = card.Body
		}
		return fmt.Sprintf("  %s%s%s  %s save   %s skip",
			dim, label, reset, yBtn, nBtn)
	default:
		return ""
	}
}

func serveAssistantViewer(w io.Writer, backend AssistantViewerBackend, idleTimeout time.Duration, now func() time.Time, selfOpen bool) error {
	if backend == nil {
		return errors.New("assistant viewer backend is nil")
	}
	if now == nil {
		now = time.Now
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	session := &assistantViewerSession{
		backend: backend,
		page:    backend.Snapshot(),
		now:     now,
		seenAt:  now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", session.handleIndex)
	mux.HandleFunc("/api/state", session.handleState)
	mux.HandleFunc("/api/chat", session.handleChat)
	mux.HandleFunc("/api/action", session.handleAction)
	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(viewerLogoPNG)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Handler: mux}
	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	viewerURL := fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)
	if _, err := fmt.Fprintln(w, viewerURL); err != nil {
		_ = server.Close()
		<-serverErr
		return err
	}
	if file, ok := w.(*os.File); ok {
		_ = file.Sync()
	}
	if selfOpen {
		_ = openURLInViewerWindow(viewerURL)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-serverErr:
			return err
		case <-ticker.C:
			if idleTimeout > 0 && session.idleDuration() >= idleTimeout {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := server.Shutdown(ctx)
				cancel()
				serveErr := <-serverErr
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				return serveErr
			}
		}
	}
}

type assistantViewerSession struct {
	mu      sync.RWMutex
	backend AssistantViewerBackend
	page    AssistantPageData
	now     func() time.Time
	seenAt  time.Time
}

func (s *assistantViewerSession) touch() {
	s.mu.Lock()
	s.seenAt = s.now()
	s.mu.Unlock()
}

func (s *assistantViewerSession) idleDuration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.now().Sub(s.seenAt)
}

func (s *assistantViewerSession) snapshot() AssistantPageData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	page := s.page
	applyAssistantPageDefaults(&page)
	return page
}

func (s *assistantViewerSession) update(page AssistantPageData) {
	applyAssistantPageDefaults(&page)
	s.mu.Lock()
	s.page = page
	s.seenAt = s.now()
	s.mu.Unlock()
}

func (s *assistantViewerSession) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderAssistantPage(s.snapshot()))
}

func (s *assistantViewerSession) handleState(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, s.snapshot())
}

func (s *assistantViewerSession) handleChat(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	message, err := readAssistantPayloadText(r, "message")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := s.backend.SubmitChat(message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.update(page)
	writeJSONResponse(w, page)
}

func (s *assistantViewerSession) handleAction(w http.ResponseWriter, r *http.Request) {
	s.touch()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	actionID, err := readAssistantPayloadText(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := s.backend.ConfirmAction(actionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.update(page)
	writeJSONResponse(w, page)
}

func renderAssistantPage(page AssistantPageData) string {
	var b strings.Builder
	_ = writeAssistantPageTerminalHTML(&b, page)
	return b.String()
}

func writeAssistantPageHTML(w io.Writer, page AssistantPageData) error {
	applyAssistantPageDefaults(&page)

	title := template.HTMLEscapeString(page.Title)
	subtitle := template.HTMLEscapeString(page.Subtitle)
	provider := template.HTMLEscapeString(providerOrFallback(page.Provider))
	model := template.HTMLEscapeString(modelOrFallback(page.Model))
	threads := renderAssistantThreadRows(page.Threads)
	actions := renderAssistantActionCards(page.Actions)
	chat := renderAssistantChatEntries(page.Chat)
	chatPlaceholder := template.HTMLEscapeString(page.ChatPlaceholder)

	_, err := fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>jot · %s</title>
  <style>%s</style>
</head>
<body>
  <header>
    <div class="brand">
      <img class="brand-mark" src="/logo.png" alt="jot">
      <div class="brand-stack">
        <div class="brand-name">jot assistant</div>
        <div class="brand-title">%s</div>
        <div class="brand-subtitle">%s</div>
      </div>
    </div>
    <div class="status-row">
      <span class="pill">%s</span>
      <span class="pill soft">%s</span>
      <span class="pill soft">%d unread</span>
    </div>
  </header>
  <main>
    <section class="panel">
      <div class="panel-head">
        <div>
          <div class="panel-title">Email list</div>
          <div class="panel-subtitle">Inline thread expansion, unread emphasis, and extracted context</div>
        </div>
      </div>
      %s
    </section>
    <aside class="panel">
      <div class="panel-head">
        <div>
          <div class="panel-title">Action panel</div>
          <div class="panel-subtitle">Deadlines, meeting requests, and reply drafts</div>
        </div>
        <span class="pill soft">%d actions</span>
      </div>
      %s
    </aside>
  </main>
  <div class="chat-strip">
    <div class="chat-log" id="chat-log">%s</div>
    <form class="chat-form" id="chat-form">
      <input class="chat-input" id="chat-input" name="message" type="text" placeholder="%s" autocomplete="off">
      <button class="chat-send" type="submit">Send</button>
    </form>
  </div>
  <script>
%s
const submitURL = %q;
const actionURL = %q;
document.getElementById('chat-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const input = document.getElementById('chat-input');
  const value = input.value.trim();
  if (!value || !submitURL) return;
  input.value = '';
  try {
    const res = await fetch(submitURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: value })
    });
    if (!res.ok) throw new Error('request failed');
    window.location.reload();
  } catch (err) {
    appendChatMessage('error', String(err));
  }
});
document.querySelectorAll('[data-action-id]').forEach((button) => {
  button.addEventListener('click', async () => {
    const id = button.getAttribute('data-action-id');
    if (!id || !actionURL) return;
    button.disabled = true;
    try {
      const res = await fetch(actionURL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      if (!res.ok) throw new Error('request failed');
      window.location.reload();
    } catch (err) {
      button.disabled = false;
      appendChatMessage('error', String(err));
    }
  });
});
  </script>
</body>
</html>`,
		title,
		assistantViewerCSS,
		title,
		subtitle,
		provider,
		model,
		page.UnreadCount,
		threads,
		len(page.Actions),
		actions,
		chat,
		chatPlaceholder,
		assistantViewerJS,
		page.SubmitURL,
		page.ActionURL,
	)
	return err
}

const assistantViewerCSS = `
    *, *::before, *::after { box-sizing: border-box; }
    :root {
      background: #f7f6f3;
      color: #1a1a18;
      font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", sans-serif;
      -webkit-font-smoothing: antialiased;
      font-size: 14px;
    }
    body {
      margin: 0;
      min-height: 100vh;
      background:
        radial-gradient(circle at top left, rgba(196, 168, 130, 0.12), transparent 28%),
        linear-gradient(180deg, #f7f6f3 0%, #f4f1ea 100%);
    }
    header {
      position: sticky;
      top: 0;
      z-index: 10;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      min-height: 48px;
      padding: 0 14px;
      background: rgba(252, 251, 249, 0.92);
      border-bottom: 0.5px solid rgba(0, 0, 0, 0.08);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
    }
    .brand { display: flex; align-items: center; gap: 10px; min-width: 0; }
    .brand-mark {
      width: 26px;
      height: 26px;
      border-radius: 7px;
      border: 0.5px solid rgba(0, 0, 0, 0.08);
      background: rgba(255, 255, 255, 0.5);
    }
    .brand-stack { display: grid; gap: 2px; min-width: 0; }
    .brand-name { font-size: 11px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: rgba(26, 26, 24, 0.42); }
    .brand-title { font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .brand-subtitle { font-size: 13px; color: rgba(26, 26, 24, 0.62); }
    .status-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 4px 9px;
      border-radius: 999px;
      background: rgba(196, 168, 130, 0.12);
      color: #8f6f43;
      font-size: 13px;
      font-weight: 600;
      border: 0.5px solid rgba(196, 168, 130, 0.28);
    }
    .pill.soft {
      background: rgba(26, 26, 24, 0.05);
      color: rgba(26, 26, 24, 0.64);
      border-color: rgba(0, 0, 0, 0.08);
    }
    main {
      display: grid;
      grid-template-columns: minmax(0, 1.55fr) minmax(330px, 0.95fr);
      gap: 16px;
      padding: 16px;
      padding-bottom: 188px;
    }
    .panel {
      background: rgba(252, 251, 249, 0.96);
      border: 0.5px solid rgba(0, 0, 0, 0.08);
      border-radius: 8px;
      overflow: hidden;
    }
    .panel-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      padding: 14px 16px;
      border-bottom: 0.5px solid rgba(0, 0, 0, 0.08);
    }
    .panel-title { font-size: 13px; font-weight: 700; letter-spacing: 0.02em; text-transform: uppercase; }
    .panel-subtitle { font-size: 13px; color: rgba(26, 26, 24, 0.56); }
    .chat-strip {
      position: fixed;
      left: 16px;
      right: 16px;
      bottom: 16px;
      background: rgba(252, 251, 249, 0.96);
      border: 0.5px solid rgba(0, 0, 0, 0.08);
      border-radius: 8px;
      overflow: hidden;
      box-shadow: 0 16px 40px rgba(26, 26, 24, 0.08);
    }
    .chat-log {
      max-height: 180px;
      overflow: auto;
      padding: 12px 14px;
      border-bottom: 0.5px solid rgba(0, 0, 0, 0.08);
      display: grid;
      gap: 8px;
    }
    .chat-entry { display: flex; gap: 10px; align-items: flex-start; line-height: 1.6; font-size: 13px; color: rgba(26, 26, 24, 0.74); }
    .chat-role { width: 62px; flex: none; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; color: rgba(26, 26, 24, 0.42); }
    .chat-text { min-width: 0; white-space: pre-wrap; }
    .chat-form { display: flex; gap: 10px; padding: 12px 14px; background: rgba(252, 251, 249, 0.98); }
    .chat-input {
      flex: 1;
      min-width: 0;
      border: 0.5px solid rgba(0, 0, 0, 0.08);
      background: #fff;
      border-radius: 8px;
      padding: 10px 12px;
      font: inherit;
      color: #1a1a18;
    }
    .chat-send {
      border: 0.5px solid rgba(196, 168, 130, 0.32);
      background: rgba(196, 168, 130, 0.12);
      color: #8f6f43;
      border-radius: 8px;
      padding: 10px 14px;
      font: inherit;
      font-weight: 700;
      cursor: pointer;
    }
    .empty { padding: 18px 16px; color: rgba(26, 26, 24, 0.56); line-height: 1.7; }
    @media (max-width: 960px) {
      main { grid-template-columns: 1fr; }
      .chat-strip { left: 12px; right: 12px; bottom: 12px; }
    }
`

const assistantViewerJS = `
function appendChatMessage(role, text) {
  const log = document.getElementById('chat-log');
  const row = document.createElement('div');
  row.className = 'chat-entry';
  const label = document.createElement('div');
  label.className = 'chat-role';
  label.textContent = role;
  const body = document.createElement('div');
  body.className = 'chat-text';
  body.textContent = text;
  row.appendChild(label);
  row.appendChild(body);
  log.appendChild(row);
  log.scrollTop = log.scrollHeight;
}
`

func writeAssistantPageTerminalHTML(w io.Writer, page AssistantPageData) error {
	applyAssistantPageDefaults(&page)

	title := template.HTMLEscapeString(page.Title)
	subtitle := template.HTMLEscapeString(page.Subtitle)
	intro := template.HTMLEscapeString(page.Intro)
	provider := template.HTMLEscapeString(providerOrFallback(page.Provider))
	model := template.HTMLEscapeString(modelOrFallback(page.Model))
	turns := renderAssistantTurns(page.Turns)
	quickPrompts := renderAssistantQuickPrompts(page.QuickPrompts)
	chatPlaceholder := template.HTMLEscapeString(page.ChatPlaceholder)

	_, err := fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>jot · %s</title>
  <style>%s</style>
</head>
<body>
  <div class="assistant-shell">
    <header class="assistant-window-bar">
      <div class="assistant-window-dots"><span></span><span></span><span></span></div>
      <div class="assistant-window-title">jot assistant</div>
      <div class="assistant-window-meta">%s · %s</div>
    </header>
    <main class="assistant-stage">
      <section class="assistant-intro">
        <div class="assistant-intro-title">jot assistant - %s</div>
        <div class="assistant-intro-subtitle">%s</div>
      </section>
      <section class="assistant-feed" id="assistant-feed">%s</section>
      <section class="assistant-composer">
        <div class="assistant-quick-prompts">%s</div>
        <form class="assistant-form" id="chat-form">
          <span class="assistant-prompt-mark">›</span>
          <input class="assistant-input" id="chat-input" name="message" type="text" placeholder="%s" autocomplete="off">
        </form>
      </section>
    </main>
  </div>
  <script>
%s
const submitURL = %q;
const actionURL = %q;
document.getElementById('chat-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const input = document.getElementById('chat-input');
  const value = input.value.trim();
  if (!value || !submitURL) return;
  input.value = '';
  try {
    const res = await fetch(submitURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: value })
    });
    if (!res.ok) throw new Error('request failed');
    window.location.reload();
  } catch (err) {
    appendTerminalNote(String(err));
  }
});
document.querySelectorAll('[data-prompt]').forEach((button) => {
  button.addEventListener('click', async () => {
    const value = button.getAttribute('data-prompt');
    if (!value || !submitURL) return;
    try {
      const res = await fetch(submitURL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: value })
      });
      if (!res.ok) throw new Error('request failed');
      window.location.reload();
    } catch (err) {
      appendTerminalNote(String(err));
    }
  });
});
document.querySelectorAll('[data-action-id]').forEach((button) => {
  button.addEventListener('click', async () => {
    const id = button.getAttribute('data-action-id');
    if (!id || !actionURL) return;
    button.disabled = true;
    try {
      const res = await fetch(actionURL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      if (!res.ok) throw new Error('request failed');
      window.location.reload();
    } catch (err) {
      button.disabled = false;
      appendTerminalNote(String(err));
    }
  });
});
  </script>
</body>
</html>`,
		title,
		assistantTerminalViewerCSS,
		provider,
		model,
		subtitle,
		intro,
		turns,
		quickPrompts,
		chatPlaceholder,
		assistantTerminalViewerJS,
		page.SubmitURL,
		page.ActionURL,
	)
	return err
}

const assistantTerminalViewerCSS = `
    *, *::before, *::after { box-sizing: border-box; }
    :root {
      color-scheme: dark;
      --bg: #0d0e0c;
      --bg-elevated: #141512;
      --bg-card: #171815;
      --bg-card-soft: #1c1d19;
      --text: #d3c6b1;
      --text-strong: #efe4ce;
      --text-dim: #8d7a5f;
      --accent: #c4a882;
      --accent-soft: rgba(196, 168, 130, 0.18);
      --green: #4fb26d;
      --border: rgba(196, 168, 130, 0.14);
      --border-strong: rgba(196, 168, 130, 0.22);
      --shadow: 0 20px 60px rgba(0, 0, 0, 0.36);
      font-family: ui-monospace, "SFMono-Regular", "SF Mono", Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 14px;
      background: var(--bg);
      color: var(--text);
      -webkit-font-smoothing: antialiased;
    }
    html, body { margin: 0; min-height: 100%; background: radial-gradient(circle at top, rgba(196, 168, 130, 0.06), transparent 35%), var(--bg); }
    body { padding: 18px; }
    .assistant-shell {
      min-height: calc(100vh - 36px);
      background: linear-gradient(180deg, rgba(20, 21, 18, 0.98), rgba(13, 14, 12, 0.98));
      border: 1px solid rgba(255, 255, 255, 0.03);
      border-radius: 16px;
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .assistant-window-bar {
      height: 34px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.04);
      display: grid;
      grid-template-columns: 120px 1fr 200px;
      align-items: center;
      padding: 0 14px;
      color: rgba(211, 198, 177, 0.52);
      font-size: 12px;
    }
    .assistant-window-dots { display: flex; gap: 7px; align-items: center; }
    .assistant-window-dots span {
      width: 10px;
      height: 10px;
      border-radius: 999px;
      background: rgba(255, 255, 255, 0.12);
      border: 1px solid rgba(255, 255, 255, 0.02);
    }
    .assistant-window-title { text-align: center; letter-spacing: 0.04em; color: rgba(211, 198, 177, 0.74); }
    .assistant-window-meta { text-align: right; color: rgba(141, 122, 95, 0.84); }
    .assistant-stage {
      display: flex;
      flex-direction: column;
      min-height: calc(100vh - 70px);
      padding: 18px 0 0;
    }
    .assistant-intro, .assistant-feed, .assistant-composer {
      width: min(900px, calc(100% - 40px));
      margin: 0 auto;
    }
    .assistant-intro { padding-bottom: 16px; }
    .assistant-intro-title { color: var(--accent); font-weight: 600; margin-bottom: 6px; }
    .assistant-intro-subtitle { color: var(--text-dim); }
    .assistant-feed {
      flex: 1;
      display: grid;
      gap: 28px;
      padding-bottom: 32px;
    }
    .assistant-turn { display: grid; gap: 10px; }
    .assistant-turn-prompt {
      color: var(--text-strong);
      font-weight: 700;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .assistant-prompt-mark { color: var(--accent); font-weight: 700; flex: none; }
    .assistant-turn-status {
      display: grid;
      gap: 4px;
      padding-left: 18px;
    }
    .assistant-status-line { color: var(--text-dim); font-style: italic; }
    .assistant-turn-cards {
      display: grid;
      gap: 14px;
      padding-left: 18px;
    }
    .assistant-card { display: grid; gap: 10px; }
    .assistant-card-eyebrow {
      color: var(--accent);
      text-transform: uppercase;
      letter-spacing: 0.06em;
      font-size: 12px;
    }
    .assistant-card-title { color: var(--text); font-weight: 600; line-height: 1.55; }
    .assistant-card-frame {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 10px;
      overflow: hidden;
    }
    .assistant-list-row, .assistant-kv-row {
      display: grid;
      gap: 12px;
      padding: 11px 14px;
      border-top: 1px solid rgba(255, 255, 255, 0.03);
    }
    .assistant-list-row:first-child, .assistant-kv-row:first-child { border-top: 0; }
    .assistant-list-row {
      grid-template-columns: 26px minmax(110px, 150px) minmax(0, 1fr) auto;
      align-items: baseline;
    }
    .assistant-row-index { color: var(--text-dim); }
    .assistant-row-sender { color: var(--accent); font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .assistant-row-subject { color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .assistant-row-detail { color: var(--text-dim); margin-top: 3px; white-space: pre-wrap; line-height: 1.6; }
    .assistant-row-meta, .assistant-kv-key { color: var(--text-dim); }
    .assistant-card-note, .assistant-card-success { line-height: 1.6; color: var(--text); }
    .assistant-card-success { color: var(--green); }
    .assistant-thread-body, .assistant-draft-body {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 14px;
      line-height: 1.65;
      color: var(--text);
      white-space: pre-wrap;
    }
    .assistant-draft-body { padding-top: 0; }
    .assistant-draft-head, .assistant-event-head {
      display: grid;
      gap: 0;
      padding: 14px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    }
    .assistant-draft-label, .assistant-event-label {
      color: var(--text-dim);
      text-transform: lowercase;
      margin-right: 10px;
      display: inline-block;
      width: 42px;
    }
    .assistant-button-row, .assistant-quick-prompts {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }
    .assistant-inline-btn, .assistant-quick-btn {
      border: 1px solid var(--border-strong);
      background: rgba(255, 255, 255, 0.02);
      color: var(--text-dim);
      border-radius: 7px;
      padding: 6px 12px;
      font: inherit;
      cursor: pointer;
    }
    .assistant-inline-btn:hover, .assistant-quick-btn:hover {
      border-color: rgba(196, 168, 130, 0.32);
      color: var(--text);
    }
    .assistant-inline-btn[data-tone="confirm"] {
      background: rgba(79, 178, 109, 0.12);
      border-color: rgba(79, 178, 109, 0.28);
      color: #83d39b;
    }
    .assistant-inline-btn[data-tone="warn"] {
      background: var(--accent-soft);
      color: var(--accent);
    }
    .assistant-composer {
      position: sticky;
      bottom: 0;
      padding: 12px 0 18px;
      background: linear-gradient(180deg, rgba(13, 14, 12, 0), rgba(13, 14, 12, 0.82) 18%, rgba(13, 14, 12, 0.98));
    }
    .assistant-form {
      display: flex;
      align-items: center;
      gap: 8px;
      padding-top: 12px;
      border-top: 1px solid rgba(255, 255, 255, 0.04);
    }
    .assistant-input {
      width: 100%;
      border: 0;
      outline: none;
      background: transparent;
      color: var(--text-dim);
      font: inherit;
      padding: 0;
    }
    .assistant-empty-state { color: var(--text-dim); line-height: 1.7; padding-left: 18px; }
    .assistant-note {
      margin-left: 18px;
      background: var(--bg-card-soft);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px 14px;
      white-space: pre-wrap;
      line-height: 1.6;
    }
    @media (max-width: 840px) {
      body { padding: 0; }
      .assistant-shell { min-height: 100vh; border-radius: 0; border: 0; }
      .assistant-window-bar { grid-template-columns: 80px 1fr 110px; }
      .assistant-window-meta {
        font-size: 10px;
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
      }
      .assistant-intro, .assistant-feed, .assistant-composer { width: calc(100% - 28px); }
      .assistant-list-row { grid-template-columns: 22px 90px minmax(0, 1fr); }
      .assistant-row-meta { grid-column: 2 / -1; }
    }
`

const assistantTerminalViewerJS = `
function appendTerminalNote(text) {
  const feed = document.getElementById('assistant-feed');
  const node = document.createElement('section');
  node.className = 'assistant-note';
  node.textContent = text;
  feed.appendChild(node);
  feed.scrollTop = feed.scrollHeight;
}
`

func renderAssistantTurns(turns []AssistantTurnView) string {
	if len(turns) == 0 {
		return `<div class="assistant-empty-state">Ask the assistant to summarize unread mail, find attachments, or draft a reply.</div>`
	}
	var b strings.Builder
	for _, turn := range turns {
		b.WriteString(`<article class="assistant-turn">`)
		b.WriteString(`<div class="assistant-turn-prompt"><span class="assistant-prompt-mark">›</span><span>`)
		b.WriteString(template.HTMLEscapeString(turn.Prompt))
		b.WriteString(`</span></div>`)
		if len(turn.StatusLines) > 0 {
			b.WriteString(`<div class="assistant-turn-status">`)
			for _, line := range turn.StatusLines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				b.WriteString(`<div class="assistant-status-line">`)
				b.WriteString(template.HTMLEscapeString(line))
				b.WriteString(`</div>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`<div class="assistant-turn-cards">`)
		for _, card := range turn.Cards {
			b.WriteString(renderAssistantCard(card))
		}
		b.WriteString(`</div></article>`)
	}
	return b.String()
}

func renderAssistantCard(card AssistantCardView) string {
	var b strings.Builder
	b.WriteString(`<section class="assistant-card">`)
	if strings.TrimSpace(card.Eyebrow) != "" {
		b.WriteString(`<div class="assistant-card-eyebrow">`)
		b.WriteString(template.HTMLEscapeString(card.Eyebrow))
		b.WriteString(`</div>`)
	}
	if strings.TrimSpace(card.Title) != "" {
		b.WriteString(`<div class="assistant-card-title">`)
		b.WriteString(template.HTMLEscapeString(card.Title))
		b.WriteString(`</div>`)
	}
	switch {
	case len(card.Rows) > 0:
		b.WriteString(`<div class="assistant-card-frame">`)
		for _, row := range card.Rows {
			fmt.Fprintf(&b, `<div class="assistant-list-row"><div class="assistant-row-index">%d</div><div class="assistant-row-sender">%s</div><div><div class="assistant-row-subject">%s</div>%s</div><div class="assistant-row-meta">%s</div></div>`,
				max(1, row.Index),
				template.HTMLEscapeString(row.Sender),
				template.HTMLEscapeString(row.Subject),
				renderAssistantRowDetail(row.Detail),
				template.HTMLEscapeString(row.Meta),
			)
		}
		b.WriteString(`</div>`)
	case card.Draft != nil:
		fmt.Fprintf(&b, `<div class="assistant-card-frame"><div class="assistant-draft-head"><div><span class="assistant-draft-label">to</span>%s</div><div><span class="assistant-draft-label">re</span>%s</div></div><div class="assistant-draft-body">%s</div></div>`,
			template.HTMLEscapeString(card.Draft.To),
			template.HTMLEscapeString(card.Draft.Subject),
			template.HTMLEscapeString(card.Draft.Body),
		)
	case card.Event != nil:
		fmt.Fprintf(&b, `<div class="assistant-card-frame"><div class="assistant-event-head"><div><span class="assistant-event-label">title</span>%s</div><div><span class="assistant-event-label">when</span>%s</div><div><span class="assistant-event-label">calendar</span>%s</div></div></div>`,
			template.HTMLEscapeString(card.Event.Title),
			template.HTMLEscapeString(card.Event.When),
			template.HTMLEscapeString(card.Event.Calendar),
		)
		if strings.TrimSpace(card.Event.Context) != "" {
			b.WriteString(`<div class="assistant-card-note">`)
			b.WriteString(template.HTMLEscapeString(card.Event.Context))
			b.WriteString(`</div>`)
		}
	case strings.TrimSpace(card.Body) != "":
		b.WriteString(`<div class="assistant-thread-body">`)
		b.WriteString(template.HTMLEscapeString(card.Body))
		b.WriteString(`</div>`)
	}
	if strings.TrimSpace(card.Note) != "" {
		b.WriteString(`<div class="assistant-card-note">`)
		b.WriteString(template.HTMLEscapeString(card.Note))
		b.WriteString(`</div>`)
	}
	if len(card.Buttons) > 0 {
		b.WriteString(`<div class="assistant-button-row">`)
		for _, button := range card.Buttons {
			fmt.Fprintf(&b, `<button class="assistant-inline-btn" data-action-id="%s" data-tone="%s">%s</button>`,
				template.HTMLEscapeString(button.ID),
				template.HTMLEscapeString(button.Tone),
				template.HTMLEscapeString(button.Label),
			)
		}
		b.WriteString(`</div>`)
	}
	if strings.TrimSpace(card.Success) != "" {
		b.WriteString(`<div class="assistant-card-success">`)
		b.WriteString(template.HTMLEscapeString(card.Success))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</section>`)
	return b.String()
}

func renderAssistantQuickPrompts(prompts []AssistantQuickPrompt) string {
	if len(prompts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, prompt := range prompts {
		fmt.Fprintf(&b, `<button class="assistant-quick-btn" data-prompt="%s">%s</button>`,
			template.HTMLEscapeString(prompt.Prompt),
			template.HTMLEscapeString(prompt.Label),
		)
	}
	return b.String()
}

func renderAssistantRowDetail(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return ""
	}
	return `<div class="assistant-row-detail">` + template.HTMLEscapeString(detail) + `</div>`
}

func applyAssistantPageDefaults(page *AssistantPageData) {
	if page == nil {
		return
	}
	if strings.TrimSpace(page.Title) == "" {
		page.Title = "jot assistant"
	}
	if strings.TrimSpace(page.Subtitle) == "" {
		page.Subtitle = "connected to Gmail"
	}
	if strings.TrimSpace(page.Intro) == "" {
		page.Intro = "type a request or pick one below"
	}
	if strings.TrimSpace(page.Provider) == "" {
		page.Provider = "provider unavailable"
	}
	if strings.TrimSpace(page.Model) == "" {
		page.Model = "model unavailable"
	}
	if strings.TrimSpace(page.Format) == "" {
		page.Format = "text"
	}
	if page.SubmitURL == "" {
		page.SubmitURL = "/api/chat"
	}
	if page.ActionURL == "" {
		page.ActionURL = "/api/action"
	}
	if page.RefreshURL == "" {
		page.RefreshURL = "/api/state"
	}
	if page.ChatPlaceholder == "" {
		page.ChatPlaceholder = "ask anything about your email..."
	}
	if len(page.QuickPrompts) == 0 {
		page.QuickPrompts = defaultAssistantQuickPrompts()
	}
	if page.GeneratedAt.IsZero() {
		page.GeneratedAt = time.Now()
	}
}

func assistantFormatClock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("3:04pm")
}

func assistantJSONLine(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeJSONResponse(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func readAssistantPayloadText(r *http.Request, key string) (string, error) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return "", err
		}
		if value, ok := payload[key].(string); ok {
			return strings.TrimSpace(value), nil
		}
		return "", fmt.Errorf("missing %s", key)
	}
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func providerOrFallback(provider string) string {
	if strings.TrimSpace(provider) == "" {
		return "provider unavailable"
	}
	return provider
}

func modelOrFallback(model string) string {
	if strings.TrimSpace(model) == "" {
		return "model unavailable"
	}
	return model
}

func renderAssistantThreadRows(threads []AssistantThreadView) string {
	if len(threads) == 0 {
		return `<div class="empty">No threads yet. Ask the assistant to search Gmail or summarize unread mail.</div>`
	}
	var b strings.Builder
	for _, thread := range threads {
		openAttr := ""
		if thread.Expanded {
			openAttr = " open"
		}
		fmt.Fprintf(&b, `<details class="thread"%s><summary><div class="thread-main"><div class="thread-title"><span class="sender">%s</span><span class="subject">%s</span></div><div class="snippet">%s</div></div><div class="meta">%s</div></summary><div class="thread-detail">`,
			openAttr,
			template.HTMLEscapeString(thread.Sender),
			template.HTMLEscapeString(thread.Subject),
			template.HTMLEscapeString(thread.Snippet),
			template.HTMLEscapeString(assistantFormatClock(thread.Timestamp)),
		)
		if len(thread.Actions) > 0 {
			b.WriteString(`<div class="thread-note">`)
			for i, action := range thread.Actions {
				if i > 0 {
					b.WriteString("<br>")
				}
				text := action.Detail
				if text == "" {
					text = action.Title
				}
				b.WriteString(`→ `)
				b.WriteString(template.HTMLEscapeString(text))
			}
			b.WriteString(`</div>`)
		}
		for _, msg := range thread.Messages {
			fmt.Fprintf(&b, `<div class="message"><div class="avatar">%s</div><div><div class="message-head"><div class="message-author">%s</div><div class="meta">%s</div></div><div class="message-body">%s</div>`,
				template.HTMLEscapeString(assistantInitials(msg.Author)),
				template.HTMLEscapeString(msg.Author),
				template.HTMLEscapeString(assistantFormatClock(msg.At)),
				template.HTMLEscapeString(msg.Content),
			)
			if len(msg.Attachments) > 0 {
				b.WriteString(`<div class="chips">`)
				for _, attachment := range msg.Attachments {
					label := attachment.Name
					if label == "" {
						label = attachment.AttachmentID
					}
					if attachment.DownloadURL != "" {
						fmt.Fprintf(&b, `<a class="chip" href="%s" download>%s</a>`, template.HTMLEscapeString(attachment.DownloadURL), template.HTMLEscapeString(label))
					} else {
						fmt.Fprintf(&b, `<span class="chip">%s</span>`, template.HTMLEscapeString(label))
					}
				}
				b.WriteString(`</div>`)
			}
			b.WriteString(`</div></div>`)
		}
		b.WriteString(`</div></details>`)
	}
	return b.String()
}

func renderAssistantActionCards(actions []AssistantActionView) string {
	if len(actions) == 0 {
		return `<div class="empty">No pending actions detected.</div>`
	}
	var b strings.Builder
	for _, action := range actions {
		stateClass := "pending"
		if action.Completed {
			stateClass = "done"
		}
		fmt.Fprintf(&b, `<div class="action-card %s"><div class="action-title">%s</div><div class="action-detail">%s</div><div class="action-meta"><span class="pill soft">%s</span>`,
			stateClass,
			template.HTMLEscapeString(action.Title),
			template.HTMLEscapeString(action.Detail),
			template.HTMLEscapeString(action.Status),
		)
		if action.ID != "" {
			label := action.ConfirmLabel
			if label == "" {
				label = "Confirm"
			}
			fmt.Fprintf(&b, `<button class="action-btn" data-action-id="%s">%s</button>`, template.HTMLEscapeString(action.ID), template.HTMLEscapeString(label))
		}
		b.WriteString(`</div></div>`)
	}
	return b.String()
}

func renderAssistantChatEntries(entries []AssistantChatEntry) string {
	if len(entries) == 0 {
		return `<div class="chat-entry"><div class="chat-role">system</div><div class="chat-text">Responses from this session will appear here.</div></div>`
	}
	var b strings.Builder
	for _, entry := range entries {
		role := entry.Role
		if role == "" {
			role = "assistant"
		}
		fmt.Fprintf(&b, `<div class="chat-entry"><div class="chat-role">%s</div><div class="chat-text">%s</div></div>`,
			template.HTMLEscapeString(role),
			template.HTMLEscapeString(entry.Text),
		)
	}
	return b.String()
}

func assistantInitials(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	switch len(fields) {
	case 0:
		return "?"
	case 1:
		runes := []rune(fields[0])
		if len(runes) == 0 {
			return "?"
		}
		return strings.ToUpper(string(runes[0]))
	default:
		first := []rune(fields[0])
		last := []rune(fields[len(fields)-1])
		if len(first) == 0 || len(last) == 0 {
			return "?"
		}
		return strings.ToUpper(string([]rune{first[0], last[0]}))
	}
}
