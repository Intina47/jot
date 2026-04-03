package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type AttachmentReader interface {
	CanRead(meta AttachmentMeta) bool
	Read(data []byte, meta AttachmentMeta) (AttachmentContent, error)
}

type AttachmentMeta struct {
	Filename     string
	MimeType     string
	SizeBytes    int64
	AttachmentID string
	MessageID    string
}

type AttachmentContent struct {
	Text     string
	Tables   [][]string
	Metadata map[string]string
	Warnings []string
}

type ExtractedActions struct {
	Summary     string
	ActionItems []string
	Deadlines   []Deadline
	MeetingReqs []MeetingRequest
	Entities    []Entity
}

type Deadline struct {
	Task string
	Due  time.Time
	Raw  string
}

type MeetingRequest struct {
	Subject       string
	ProposedTimes []TimeSlot
	Participants  []string
	Location      string
}

type TimeSlot struct {
	Start    time.Time
	End      time.Time
	Raw      string
	Timezone string
}

type Entity struct {
	Type  string
	Value string
}

type documentSignals struct {
	ActionItems []string
	Deadlines   []Deadline
	Entities    []Entity
	Highlights  []string
}

func DefaultAttachmentReaders() []AttachmentReader {
	return []AttachmentReader{
		plainAttachmentReader{},
		markdownAttachmentReader{},
		jsonAttachmentReader{},
		xmlAttachmentReader{},
		csvAttachmentReader{},
		xlsxAttachmentReader{},
		legacyExcelAttachmentReader{},
		pdfAttachmentReader{},
		docxAttachmentReader{},
	}
}

func ReadAttachmentContent(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	for _, reader := range DefaultAttachmentReaders() {
		if reader.CanRead(meta) {
			return reader.Read(data, meta)
		}
	}
	return AttachmentContent{}, fmt.Errorf("no attachment reader available for %q (%s)", meta.Filename, meta.MimeType)
}

// ExtractActions is a deterministic fallback extractor. The main assistant path
// should prefer model-based reasoning and only use this when a provider is
// unavailable or a semantic extraction response cannot be parsed safely.
func ExtractActions(text string) ExtractedActions {
	return ExtractActionsAt(text, time.Now())
}

// ExtractActionsAt is kept as a low-dependency fallback for offline/tooling
// paths. It is not intended to be the primary reasoning engine for the
// assistant experience.
func ExtractActionsAt(text string, now time.Time) ExtractedActions {
	lines := splitLines(text)
	actionItems := extractActionItems(lines)
	deadlines := extractDeadlines(lines, now)
	meetingReqs := extractMeetingRequests(lines, now)
	entities := extractEntities(text, now)
	signals := extractDocumentSignals(lines, now)
	actionItems = mergeStrings(actionItems, signals.ActionItems)
	deadlines = mergeDeadlines(deadlines, signals.Deadlines)
	entities = mergeEntities(entities, signals.Entities)
	return ExtractedActions{
		Summary:     buildActionSummaryWithHighlights(actionItems, deadlines, meetingReqs, signals.Highlights, lines),
		ActionItems: actionItems,
		Deadlines:   deadlines,
		MeetingReqs: meetingReqs,
		Entities:    entities,
	}
}

func extractDocumentSignals(lines []string, now time.Time) documentSignals {
	var signals documentSignals
	seenDeadlines := map[string]struct{}{}
	seenEntities := map[string]struct{}{}
	seenHighlights := map[string]struct{}{}
	addDeadline := func(task, raw string, due time.Time) {
		task = strings.TrimSpace(task)
		raw = strings.TrimSpace(raw)
		if task == "" && raw == "" {
			return
		}
		key := strings.ToLower(task + "|" + raw)
		if _, exists := seenDeadlines[key]; exists {
			return
		}
		seenDeadlines[key] = struct{}{}
		signals.Deadlines = append(signals.Deadlines, Deadline{Task: task, Raw: raw, Due: due})
	}
	addEntity := func(typ, value string) {
		value = strings.TrimSpace(strings.Trim(value, ",.;:"))
		if value == "" {
			return
		}
		key := typ + "|" + strings.ToLower(value)
		if _, exists := seenEntities[key]; exists {
			return
		}
		seenEntities[key] = struct{}{}
		signals.Entities = append(signals.Entities, Entity{Type: typ, Value: value})
	}
	addHighlight := func(value string) {
		value = strings.TrimSpace(strings.TrimRight(value, ".;"))
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, exists := seenHighlights[key]; exists {
			return
		}
		seenHighlights[key] = struct{}{}
		signals.Highlights = append(signals.Highlights, value)
	}

	vendor := inferVendorHint(lines)
	if vendor != "" {
		addEntity("vendor", vendor)
		addHighlight("Vendor: " + vendor)
	}

	for _, raw := range lines {
		line := strings.TrimSpace(normalizeWhitespace(raw))
		if line == "" || isLikelyBoilerplateLine(line) {
			continue
		}
		lower := strings.ToLower(line)
		if amount, ok := extractInvoiceTotal(line); ok {
			addEntity("invoice_total", amount)
			addHighlight("Invoice total: " + amount)
		}
		if ref, ok := extractInvoiceReference(line); ok {
			addEntity("invoice_reference", ref)
			addHighlight("Reference: " + ref)
		}
		if dueText, ok := extractInvoiceDueText(line, now); ok {
			if dueTime, parsed := parseDeadlineTime(dueText, now), strings.TrimSpace(dueText); !dueTime.IsZero() {
				addDeadline("Pay invoice", parsed, dueTime)
				addEntity("invoice_due_date", parsed)
				addHighlight("Invoice due: " + parsed)
			}
		}
		if task, dueText, ok := fallbackDeadlineParts(line, now); ok {
			dueTime := parseDeadlineTime(dueText, now)
			if !dueTime.IsZero() {
				addDeadline(task, dueText, dueTime)
				addHighlight(task + " due: " + dueText)
			}
		} else if strings.Contains(lower, "deadline") || strings.Contains(lower, "due date") || strings.Contains(lower, "payment due") {
			if dueText, dueTime, ok := extractDateSpan(line, now); ok {
				task := documentDeadlineTaskLabel(line)
				if task == "" {
					task = "Deadline"
				}
				addDeadline(task, dueText, dueTime)
				addEntity("date", dueText)
				addHighlight(task + ": " + dueText)
			}
		}
		if strings.Contains(lower, "invoice") && strings.Contains(lower, "total") {
			if amount, ok := extractCurrencyAmount(line); ok {
				addEntity("invoice_total", amount)
				addHighlight("Invoice total: " + amount)
			}
		}
	}

	return signals
}

func mergeStrings(base []string, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range base {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extra {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeDeadlines(base []Deadline, extra []Deadline) []Deadline {
	if len(extra) == 0 {
		return append([]Deadline(nil), base...)
	}
	seen := map[string]struct{}{}
	out := make([]Deadline, 0, len(base)+len(extra))
	appendDeadline := func(d Deadline) {
		key := strings.ToLower(strings.TrimSpace(d.Task) + "|" + strings.TrimSpace(d.Raw) + "|" + d.Due.UTC().Format(time.RFC3339))
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, d)
	}
	for _, deadline := range base {
		appendDeadline(deadline)
	}
	for _, deadline := range extra {
		appendDeadline(deadline)
	}
	return out
}

func mergeEntities(base []Entity, extra []Entity) []Entity {
	if len(extra) == 0 {
		return append([]Entity(nil), base...)
	}
	seen := map[string]struct{}{}
	out := make([]Entity, 0, len(base)+len(extra))
	appendEntity := func(e Entity) {
		key := strings.ToLower(strings.TrimSpace(e.Type) + "|" + strings.TrimSpace(e.Value))
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	for _, entity := range base {
		appendEntity(entity)
	}
	for _, entity := range extra {
		appendEntity(entity)
	}
	return out
}

type plainAttachmentReader struct{}

func (plainAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "text/plain") || strings.EqualFold(filepath.Ext(meta.Filename), ".txt")
}

func (plainAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	return AttachmentContent{Text: strings.TrimRight(string(data), "\x00"), Metadata: map[string]string{"type": "text/plain"}}, nil
}

type markdownAttachmentReader struct{}

func (markdownAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "text/markdown", "text/x-markdown") || strings.EqualFold(filepath.Ext(meta.Filename), ".md")
}

func (markdownAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	return AttachmentContent{Text: strings.TrimRight(string(data), "\x00"), Metadata: map[string]string{"type": "text/markdown"}}, nil
}

type jsonAttachmentReader struct{}

func (jsonAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/json") || strings.EqualFold(filepath.Ext(meta.Filename), ".json")
}

func (jsonAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return AttachmentContent{}, err
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return AttachmentContent{}, err
	}
	content := AttachmentContent{
		Text:     string(pretty),
		Metadata: map[string]string{"type": "application/json"},
	}
	if keys := topLevelJSONKeys(value); len(keys) > 0 {
		content.Metadata["top_keys"] = strings.Join(keys, ",")
	}
	return content, nil
}

type csvAttachmentReader struct{}

func (csvAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "text/csv", "application/csv") || strings.EqualFold(filepath.Ext(meta.Filename), ".csv")
}

func (csvAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	rows, err := reader.ReadAll()
	if err != nil {
		return AttachmentContent{}, err
	}
	content := AttachmentContent{
		Text:     renderTable(rows),
		Tables:   rows,
		Metadata: map[string]string{"type": "text/csv", "rows": strconv.Itoa(len(rows))},
	}
	if len(rows) > 0 {
		content.Metadata["columns"] = strconv.Itoa(len(rows[0]))
	}
	return content, nil
}

type xmlAttachmentReader struct{}

func (xmlAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/xml", "text/xml") || strings.EqualFold(filepath.Ext(meta.Filename), ".xml")
}

func (xmlAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	text, err := stripXMLText(data)
	if err != nil {
		return AttachmentContent{}, err
	}
	return AttachmentContent{
		Text:     strings.TrimSpace(text),
		Metadata: map[string]string{"type": "application/xml"},
	}, nil
}

type xlsxAttachmentReader struct{}

func (xlsxAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet") || strings.EqualFold(filepath.Ext(meta.Filename), ".xlsx")
}

func (xlsxAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return AttachmentContent{}, err
	}

	sharedStrings, _ := xlsxLoadSharedStrings(zr)
	sheets, err := xlsxLoadSheets(zr, sharedStrings)
	if err != nil {
		return AttachmentContent{}, err
	}
	if len(sheets) == 0 {
		return AttachmentContent{}, errors.New("no worksheets found in xlsx archive")
	}

	var textParts []string
	var tables [][]string
	for _, sheet := range sheets {
		if len(sheet.Rows) == 0 {
			continue
		}
		textParts = append(textParts, sheet.Name)
		textParts = append(textParts, renderTable(sheet.Rows))
		if len(tables) == 0 {
			tables = sheet.Rows
		}
	}
	return AttachmentContent{
		Text:     strings.TrimSpace(strings.Join(textParts, "\n\n")),
		Tables:   tables,
		Metadata: map[string]string{"type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "sheets": strconv.Itoa(len(sheets))},
	}, nil
}

type legacyExcelAttachmentReader struct{}

func (legacyExcelAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/vnd.ms-excel") || strings.EqualFold(filepath.Ext(meta.Filename), ".xls")
}

func (legacyExcelAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	runs := printableASCIIRuns(data, 4)
	content := AttachmentContent{
		Text:     strings.Join(runs, "\n"),
		Metadata: map[string]string{"type": "application/vnd.ms-excel", "strategy": "printable-ascii-runs"},
		Warnings: []string{"Legacy .xls extraction is best-effort only and may miss workbook structure or cell ordering"},
	}
	if content.Text == "" {
		content.Warnings = append(content.Warnings, "no readable text runs were recovered from the spreadsheet bytes")
	}
	return content, nil
}

type pdfAttachmentReader struct{}

func (pdfAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/pdf") || strings.EqualFold(filepath.Ext(meta.Filename), ".pdf")
}

func (pdfAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	// Best-effort fallback only. Without a PDF parser we can only recover
	// printable ASCII runs from the raw bytes, which may miss layout and text.
	runs := printableASCIIRuns(data, 6)
	content := AttachmentContent{
		Text:     strings.Join(runs, "\n"),
		Metadata: map[string]string{"type": "application/pdf", "strategy": "printable-ascii-runs"},
		Warnings: []string{"PDF text extraction is best-effort only and may miss content or ordering"},
	}
	if content.Text == "" {
		content.Warnings = append(content.Warnings, "no printable ASCII text runs were found in the PDF bytes")
	}
	return content, nil
}

type docxAttachmentReader struct{}

func (docxAttachmentReader) CanRead(meta AttachmentMeta) bool {
	return mimeMatches(meta, "application/vnd.openxmlformats-officedocument.wordprocessingml.document") || strings.EqualFold(filepath.Ext(meta.Filename), ".docx")
}

func (docxAttachmentReader) Read(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return AttachmentContent{}, err
	}

	var document []byte
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return AttachmentContent{}, err
		}
		document, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return AttachmentContent{}, err
		}
		break
	}
	if len(document) == 0 {
		return AttachmentContent{}, errors.New("word/document.xml not found in docx archive")
	}

	text, err := stripXMLText(document)
	if err != nil {
		return AttachmentContent{}, err
	}
	return AttachmentContent{
		Text:     strings.TrimSpace(text),
		Metadata: map[string]string{"type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	}, nil
}

func mimeMatches(meta AttachmentMeta, types ...string) bool {
	actual := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if actual == "" {
		return false
	}
	for _, candidate := range types {
		if actual == strings.ToLower(candidate) {
			return true
		}
	}
	return false
}

func topLevelJSONKeys(value any) []string {
	var keys []string
	switch v := value.(type) {
	case map[string]any:
		for k := range v {
			keys = append(keys, k)
		}
	case []any:
		if len(v) > 0 {
			if obj, ok := v[0].(map[string]any); ok {
				for k := range obj {
					keys = append(keys, k)
				}
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func renderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(row, " | "))
	}
	return b.String()
}

func printableASCIIRuns(data []byte, minLen int) []string {
	if minLen < 1 {
		minLen = 1
	}
	var runs []string
	var current strings.Builder
	flush := func() {
		if current.Len() >= minLen {
			runs = append(runs, strings.TrimSpace(current.String()))
		}
		current.Reset()
	}
	for _, b := range data {
		switch {
		case b == '\n' || b == '\r' || b == '\t':
			if current.Len() > 0 && !strings.HasSuffix(current.String(), " ") {
				current.WriteByte(' ')
			}
		case b >= 32 && b <= 126:
			current.WriteByte(b)
		default:
			flush()
		}
	}
	flush()
	return compactStrings(runs)
}

func stripXMLText(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var buf bytes.Buffer
	lastWasNewline := false

	writeSpace := func() {
		if buf.Len() == 0 {
			return
		}
		last := buf.Bytes()[buf.Len()-1]
		if last != ' ' && last != '\n' {
			_ = buf.WriteByte(' ')
		}
	}
	writeNewline := func() {
		for buf.Len() > 0 {
			last := buf.Bytes()[buf.Len()-1]
			if last == ' ' || last == '\t' {
				buf.Truncate(buf.Len() - 1)
				continue
			}
			break
		}
		if buf.Len() > 0 && !lastWasNewline {
			_ = buf.WriteByte('\n')
			lastWasNewline = true
		}
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "p", "tr":
				writeNewline()
			case "tab":
				writeSpace()
			case "br", "cr":
				writeNewline()
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "p", "tr":
				writeNewline()
			}
		case xml.CharData:
			text := normalizeWhitespace(string(t))
			if text == "" {
				continue
			}
			if buf.Len() > 0 {
				last := buf.Bytes()[buf.Len()-1]
				if last != '\n' && last != ' ' {
					_ = buf.WriteByte(' ')
				}
			}
			_, _ = buf.WriteString(text)
			lastWasNewline = false
		}
	}
	return normalizeLineBreaks(buf.String()), nil
}

type xlsxSheet struct {
	Name string
	Rows [][]string
}

type xlsxSharedStrings struct {
	Items []xlsxSharedStringItem `xml:"si"`
}

type xlsxSharedStringItem struct {
	Text string                `xml:"t"`
	Runs []xlsxSharedStringRun `xml:"r"`
}

type xlsxSharedStringRun struct {
	Text string `xml:"t"`
}

type xlsxWorksheet struct {
	Rows []xlsxWorksheetRow `xml:"sheetData>row"`
}

type xlsxWorksheetRow struct {
	Cells []xlsxWorksheetCell `xml:"c"`
}

type xlsxWorksheetCell struct {
	Ref        string `xml:"r,attr"`
	Type       string `xml:"t,attr"`
	Value      string `xml:"v"`
	InlineText string `xml:"is>t"`
}

func xlsxLoadSharedStrings(zr *zip.Reader) ([]string, error) {
	data, err := zipReadFile(zr, "xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	var doc xlsxSharedStrings
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.Items))
	for _, item := range doc.Items {
		text := strings.TrimSpace(item.Text)
		if text == "" && len(item.Runs) > 0 {
			var parts []string
			for _, run := range item.Runs {
				if strings.TrimSpace(run.Text) != "" {
					parts = append(parts, strings.TrimSpace(run.Text))
				}
			}
			text = strings.Join(parts, "")
		}
		out = append(out, text)
	}
	return out, nil
}

func xlsxLoadSheets(zr *zip.Reader, sharedStrings []string) ([]xlsxSheet, error) {
	var files []*zip.File
	for _, file := range zr.File {
		if strings.HasPrefix(file.Name, "xl/worksheets/sheet") && strings.HasSuffix(file.Name, ".xml") {
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	var out []xlsxSheet
	for _, file := range files {
		data, err := zipReadZipFile(file)
		if err != nil {
			return nil, err
		}
		var sheetDoc xlsxWorksheet
		if err := xml.Unmarshal(data, &sheetDoc); err != nil {
			return nil, err
		}
		rows := xlsxWorksheetRows(sheetDoc.Rows, sharedStrings)
		out = append(out, xlsxSheet{
			Name: strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name)),
			Rows: rows,
		})
	}
	return out, nil
}

func xlsxWorksheetRows(rows []xlsxWorksheetRow, sharedStrings []string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		if len(row.Cells) == 0 {
			continue
		}
		width := 0
		for _, cell := range row.Cells {
			col := xlsxColumnIndex(cell.Ref)
			if col > width {
				width = col
			}
		}
		if width == 0 {
			width = len(row.Cells)
		}
		record := make([]string, width)
		for i, cell := range row.Cells {
			col := xlsxColumnIndex(cell.Ref)
			if col <= 0 {
				col = i + 1
			}
			record[col-1] = xlsxCellValue(cell, sharedStrings)
		}
		out = append(out, trimTrailingEmptyCells(record))
	}
	return out
}

func xlsxColumnIndex(ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0
	}
	col := 0
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			col = col*26 + int(r-'A'+1)
			continue
		}
		if r >= 'a' && r <= 'z' {
			col = col*26 + int(r-'a'+1)
			continue
		}
		break
	}
	return col
}

func xlsxCellValue(cell xlsxWorksheetCell, sharedStrings []string) string {
	switch strings.TrimSpace(cell.Type) {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err == nil && idx >= 0 && idx < len(sharedStrings) {
			return sharedStrings[idx]
		}
	case "inlineStr", "str":
		if strings.TrimSpace(cell.InlineText) != "" {
			return strings.TrimSpace(cell.InlineText)
		}
	}
	return strings.TrimSpace(cell.Value)
}

func trimTrailingEmptyCells(values []string) []string {
	end := len(values)
	for end > 0 && strings.TrimSpace(values[end-1]) == "" {
		end--
	}
	return append([]string(nil), values[:end]...)
}

func zipReadFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, file := range zr.File {
		if file.Name == name {
			return zipReadZipFile(file)
		}
	}
	return nil, os.ErrNotExist
}

func zipReadZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func splitLines(text string) []string {
	text = normalizeLineBreaks(text)
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func normalizeLineBreaks(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(normalizeWhitespace(line))
	}
	return strings.TrimSpace(strings.Join(compactStrings(lines), "\n"))
}

func normalizeWhitespace(text string) string {
	var b strings.Builder
	lastWasSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !lastWasSpace {
				b.WriteByte(' ')
				lastWasSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastWasSpace = false
	}
	return strings.TrimSpace(b.String())
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == v {
			continue
		}
		out = append(out, v)
	}
	return out
}

func extractActionItems(lines []string) []string {
	var items []string
	seen := map[string]struct{}{}
	for _, raw := range lines {
		line := strings.TrimSpace(normalizeWhitespace(raw))
		if line == "" || isLikelyBoilerplateLine(line) {
			continue
		}
		if bullet, ok := stripBulletPrefix(line); ok {
			line = bullet
		}
		if !isLikelyActionCandidate(line) {
			continue
		}
		key := strings.ToLower(line)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, strings.TrimRight(line, ".;"))
	}
	return items
}

func isLikelyActionCandidate(line string) bool {
	line = strings.TrimSpace(normalizeWhitespace(line))
	if line == "" || isLikelyBoilerplateLine(line) {
		return false
	}
	lower := strings.ToLower(line)
	for _, prefix := range []string{"please ", "can you ", "could you ", "let's ", "lets "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, phrase := range []string{"need to ", "needs to ", "follow up", "todo", "action item"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	words := strings.Fields(lower)
	if len(words) == 0 {
		return false
	}
	switch strings.Trim(words[0], ",.;:!?") {
	case "send", "review", "confirm", "reply", "update", "complete", "join", "attend", "submit", "share", "schedule", "reschedule", "approve", "pay", "check", "read", "prepare", "upload", "finish", "call", "email", "book", "move":
		return true
	default:
		return false
	}
}

func isLikelyBoilerplateLine(line string) bool {
	line = strings.TrimSpace(normalizeWhitespace(line))
	if line == "" {
		return true
	}
	lower := strings.ToLower(line)
	if len(line) > 320 {
		return true
	}
	for _, phrase := range []string{
		"unsubscribe",
		"manage preferences",
		"privacy policy",
		"terms of service",
		"view in browser",
		"copyright",
		"all rights reserved",
		"do not reply",
		"no action is required",
		"you don't need to do anything",
		"you dont need to do anything",
		"this email and any attachments are confidential",
		"confidential and may be protected",
		"follow us",
		"share your referral link",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func stripBulletPrefix(line string) (string, bool) {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return strings.TrimSpace(line[2:]), true
	}
	trimmed := strings.TrimSpace(line)
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(trimmed) && (trimmed[i] == '.' || trimmed[i] == ')') && trimmed[i+1] == ' ' {
		return strings.TrimSpace(trimmed[i+2:]), true
	}
	return "", false
}

func extractDeadlines(lines []string, now time.Time) []Deadline {
	var deadlines []Deadline
	seen := map[string]struct{}{}
	for _, raw := range lines {
		line := strings.TrimSpace(normalizeWhitespace(raw))
		if line == "" || isLikelyBoilerplateLine(line) {
			continue
		}
		task, dueText, ok := fallbackDeadlineParts(line, now)
		if !ok {
			continue
		}
		key := strings.ToLower(task + "|" + dueText)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		due := parseDeadlineTime(dueText, now)
		if due.IsZero() {
			if candidate, parsed, ok := extractDateSpan(dueText, now); ok {
				dueText = candidate
				due = parsed
			}
		}
		deadlines = append(deadlines, Deadline{
			Task: task,
			Due:  due,
			Raw:  dueText,
		})
	}
	return deadlines
}

func fallbackDeadlineParts(line string, now time.Time) (string, string, bool) {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		" due date ",
		" payment due ",
		" amount due ",
		" balance due ",
		" due on ",
		" due by ",
		" due ",
		" by ",
		" no later than ",
		" before ",
		" submit by ",
		" complete by ",
		" required by ",
		" must be received by ",
		" respond by ",
		" reply by ",
		" return by ",
	} {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		task := strings.TrimSpace(strings.TrimRight(line[:idx], ":-, "))
		dueText := strings.TrimSpace(strings.TrimRight(line[idx+len(marker):], ".;, "))
		if task == "" || dueText == "" {
			continue
		}
		if candidate, _, ok := extractDateSpan(dueText, now); ok {
			dueText = candidate
		}
		if strings.EqualFold(task, "due date") || strings.EqualFold(task, "deadline") {
			task = documentDeadlineTaskLabel(line)
			if task == "" {
				task = strings.Title(strings.TrimSpace(marker))
			}
		}
		return task, dueText, true
	}
	if strings.Contains(lower, "deadline") || strings.Contains(lower, "due date") || strings.Contains(lower, "payment due") || strings.Contains(lower, "submit by") || strings.Contains(lower, "complete by") {
		if dueText, _, ok := extractDateSpan(line, now); ok {
			task := documentDeadlineTaskLabel(line)
			if task == "" {
				task = "Deadline"
			}
			return task, dueText, true
		}
	}
	return "", "", false
}

func parseDeadlineTime(raw string, now time.Time) time.Time {
	raw = strings.TrimSpace(strings.TrimRight(raw, ".;,"))
	if raw == "" {
		return time.Time{}
	}
	if base, ok := parseRelativeDate(raw, now); ok {
		if t, ok := parseClockInText(raw, base); ok {
			return t
		}
		return base
	}
	if due, ok := parseExplicitDateInText(raw, now); ok {
		if t, ok := parseClockInText(raw, due); ok {
			return t
		}
		return due
	}
	if t, ok := parseClockInText(raw, now); ok {
		return t
	}
	return time.Time{}
}

func parseRelativeDate(raw string, now time.Time) (time.Time, bool) {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "tomorrow"):
		return dateAtHour(now.AddDate(0, 0, 1), 17, 0), true
	case strings.Contains(lower, "today"):
		return dateAtHour(now, 17, 0), true
	case strings.Contains(lower, "next week"):
		return dateAtHour(now.AddDate(0, 0, daysUntilEndOfWeek(now)+7), 17, 0), true
	case strings.Contains(lower, "this week"):
		return dateAtHour(now.AddDate(0, 0, daysUntilEndOfWeek(now)), 17, 0), true
	case strings.Contains(lower, "next month"):
		return dateAtHour(now.AddDate(0, 1, 0), 17, 0), true
	case strings.Contains(lower, "this month"):
		return endOfMonth(now), true
	case strings.Contains(lower, "eod"), strings.Contains(lower, "end of day"):
		return dateAtHour(now, 17, 0), true
	default:
		if relative, ok := parseRelativeDays(lower, now); ok {
			return relative, true
		}
		return time.Time{}, false
	}
}

func parseClockInText(raw string, base time.Time) (time.Time, bool) {
	cleaned := strings.NewReplacer(",", " ", ";", " ", ".", " ", "(", " ", ")", " ").Replace(strings.ToLower(raw))
	fields := strings.Fields(cleaned)
	for i := 0; i < len(fields); i++ {
		token := fields[i]
		if hour, minute, ok := parseClockToken(token); ok {
			return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, base.Location()), true
		}
		if i+1 < len(fields) {
			combined := token + fields[i+1]
			if hour, minute, ok := parseClockToken(combined); ok {
				return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, base.Location()), true
			}
		}
	}
	return time.Time{}, false
}

func parseClockToken(token string) (int, int, bool) {
	token = strings.TrimSpace(strings.Trim(token, `"'`))
	if len(token) < 3 {
		return 0, 0, false
	}
	suffix := token[len(token)-2:]
	if suffix != "am" && suffix != "pm" {
		return 0, 0, false
	}
	body := strings.TrimSpace(token[:len(token)-2])
	parts := strings.Split(body, ":")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minute := 0
	if len(parts) == 2 {
		minute, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	if suffix == "pm" && hour < 12 {
		hour += 12
	}
	if suffix == "am" && hour == 12 {
		hour = 0
	}
	return hour, minute, true
}

func extractMeetingRequests(lines []string, now time.Time) []MeetingRequest {
	var reqs []MeetingRequest
	seen := map[string]struct{}{}
	for _, raw := range lines {
		line := strings.TrimSpace(normalizeWhitespace(raw))
		if line == "" || isLikelyBoilerplateLine(line) || !looksLikeMeetingRequest(line) {
			continue
		}
		req := MeetingRequest{
			Subject:       line,
			ProposedTimes: extractTimeSlots(line, now),
			Participants:  extractParticipants(line),
			Location:      extractLocation(line),
		}
		key := strings.ToLower(req.Subject + "|" + strings.Join(req.Participants, ",") + "|" + req.Location)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		reqs = append(reqs, req)
	}
	return reqs
}

func looksLikeMeetingRequest(line string) bool {
	lower := strings.ToLower(line)
	for _, keyword := range []string{"meet", "meeting", "schedule", "sync", "standup", "call", "catch up", "calendar invite"} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func extractTimeSlots(line string, now time.Time) []TimeSlot {
	base, ok := parseRelativeDate(line, now)
	if !ok {
		base = now
	}
	start, ok := parseClockInText(line, base)
	if !ok {
		return nil
	}
	end := start.Add(time.Hour)
	if parsedEnd, ok := parseClockRangeEnd(line, base, start); ok {
		end = parsedEnd
	}
	return []TimeSlot{{
		Start:    start,
		End:      end,
		Raw:      strings.TrimSpace(line),
		Timezone: now.Location().String(),
	}}
}

func parseClockRangeEnd(line string, base time.Time, start time.Time) (time.Time, bool) {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, " to ")
	if idx < 0 {
		return time.Time{}, false
	}
	endText := strings.TrimSpace(line[idx+4:])
	endTime, ok := parseClockInText(endText, base)
	if !ok {
		return time.Time{}, false
	}
	if endTime.Before(start) {
		return endTime.Add(12 * time.Hour), true
	}
	return endTime, true
}

func extractParticipants(line string) []string {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, " with ")
	if idx < 0 {
		return nil
	}
	rest := strings.TrimSpace(line[idx+6:])
	for _, boundary := range []string{" in ", " via ", " at ", " tomorrow", " today", " this week", " next week"} {
		if cut := strings.Index(strings.ToLower(rest), boundary); cut >= 0 {
			rest = strings.TrimSpace(rest[:cut])
			break
		}
	}
	if rest == "" {
		return nil
	}
	return []string{strings.Trim(rest, ",.; ")}
}

func extractLocation(line string) string {
	lower := strings.ToLower(line)
	for _, marker := range []string{" via ", " in ", " at "} {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(marker):])
		for _, boundary := range []string{" with ", " tomorrow", " today", " this week", " next week"} {
			if cut := strings.Index(strings.ToLower(rest), boundary); cut >= 0 {
				rest = strings.TrimSpace(rest[:cut])
				break
			}
		}
		rest = strings.Trim(rest, ",.; ")
		if rest != "" {
			return rest
		}
	}
	return ""
}

func extractEntities(text string, now time.Time) []Entity {
	var entities []Entity
	seen := map[string]struct{}{}
	add := func(typ, value string) {
		value = strings.TrimSpace(strings.Trim(value, ",.;:"))
		if value == "" {
			return
		}
		key := typ + "|" + strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		entities = append(entities, Entity{Type: typ, Value: value})
	}

	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, `"'()[]{}<>,.;:`)
		lower := strings.ToLower(clean)
		if strings.Contains(clean, "@") && strings.Contains(clean, ".") {
			add("person", clean)
			continue
		}
		if strings.HasPrefix(clean, "$") || strings.HasPrefix(clean, "£") || strings.HasPrefix(clean, "€") {
			add("amount", clean)
			continue
		}
		if strings.HasSuffix(lower, "usd") || strings.HasSuffix(lower, "eur") || strings.HasSuffix(lower, "gbp") {
			add("amount", clean)
		}
	}
	for _, phrase := range []string{"today", "tomorrow", "this week", "next week", "this month", "next month", "eod", "end of day"} {
		if strings.Contains(strings.ToLower(text), phrase) {
			add("date", phrase)
		}
	}
	for _, line := range splitLines(text) {
		line = strings.TrimSpace(normalizeWhitespace(line))
		if line == "" || isLikelyBoilerplateLine(line) {
			continue
		}
		if amount, ok := extractInvoiceTotal(line); ok {
			add("invoice_total", amount)
		}
		if ref, ok := extractInvoiceReference(line); ok {
			add("invoice_reference", ref)
		}
		if dueText, ok := extractInvoiceDueText(line, now); ok {
			add("invoice_due_date", dueText)
		}
	}
	if vendor := inferVendorHint(splitLines(text)); vendor != "" {
		add("vendor", vendor)
	}
	_ = now
	return entities
}

func buildActionSummary(actionItems []string, deadlines []Deadline, meetingReqs []MeetingRequest, lines []string) string {
	return buildActionSummaryWithHighlights(actionItems, deadlines, meetingReqs, nil, lines)
}

func buildActionSummaryWithHighlights(actionItems []string, deadlines []Deadline, meetingReqs []MeetingRequest, highlights []string, lines []string) string {
	if len(highlights) > 0 {
		limit := minInt(len(highlights), 3)
		return strings.Join(highlights[:limit], "; ")
	}
	if len(actionItems) == 0 && len(deadlines) == 0 && len(meetingReqs) == 0 {
		sentence := firstSentence(strings.Join(lines, " "))
		if sentence == "" {
			return "No obvious action items detected."
		}
		return sentence
	}
	return fmt.Sprintf("%d action items, %d deadlines, %d meeting requests", len(actionItems), len(deadlines), len(meetingReqs))
}

func extractInvoiceTotal(line string) (string, bool) {
	labels := []string{
		"amount due",
		"balance due",
		"grand total",
		"invoice total",
		"total due",
		"amount payable",
		"total payable",
		"amount",
		"total",
	}
	if labelValue, ok := extractLabeledValue(line, labels); ok {
		if amount, ok := extractCurrencyAmount(labelValue); ok {
			return amount, true
		}
		if amount, ok := extractCurrencyAmount(line); ok {
			return amount, true
		}
	}
	if amount, ok := extractCurrencyAmount(line); ok {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "invoice") || strings.Contains(lower, "total") || strings.Contains(lower, "amount due") || strings.Contains(lower, "balance due") {
			return amount, true
		}
	}
	return "", false
}

func extractInvoiceReference(line string) (string, bool) {
	labels := []string{
		"invoice number",
		"invoice no",
		"invoice #",
		"invoice ref",
		"reference number",
		"reference no",
		"reference",
		"ref",
		"payment reference",
		"document number",
		"order number",
		"po number",
		"po no",
		"purchase order",
	}
	value, ok := extractLabeledValue(line, labels)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", false
	}
	cut := len(fields)
	for i, field := range fields {
		switch strings.ToLower(strings.Trim(field, ",.;:")) {
		case "due", "total", "amount", "paid", "balance", "invoice", "pay", "date":
			cut = i
			goto done
		}
	}
done:
	value = strings.TrimSpace(strings.Join(fields[:cut], " "))
	value = strings.Trim(value, ",.;:")
	if value == "" {
		return "", false
	}
	return value, true
}

func extractInvoiceDueText(line string, now time.Time) (string, bool) {
	lower := strings.ToLower(line)
	for _, label := range []string{
		"due date",
		"payment due",
		"amount due",
		"balance due",
		"due by",
		"due on",
		"pay by",
		"payable by",
		"deadline",
		"due",
	} {
		if idx := strings.Index(lower, label); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len(label):])
			rest = strings.TrimLeft(rest, ":-—= #\t")
			if candidate, _, ok := extractDateSpan(rest, now); ok {
				return candidate, true
			}
			if candidate, _, ok := extractDateSpan(line, now); ok {
				return candidate, true
			}
		}
	}
	if strings.Contains(lower, "deadline") || strings.Contains(lower, "payment due") || strings.Contains(lower, "due date") {
		if candidate, _, ok := extractDateSpan(line, now); ok {
			return candidate, true
		}
	}
	return "", false
}

func extractCurrencyAmount(line string) (string, bool) {
	fields := strings.Fields(strings.NewReplacer(",", " ", ":", " ", ";", " ", "(", " ", ")", " ").Replace(line))
	for i, field := range fields {
		token := strings.TrimSpace(strings.Trim(field, ",.;:"))
		lower := strings.ToLower(token)
		switch {
		case strings.HasPrefix(token, "$") || strings.HasPrefix(token, "£") || strings.HasPrefix(token, "€"):
			number := strings.TrimSpace(token[1:])
			if isAmountNumber(number) {
				return token, true
			}
		case lower == "usd" || lower == "gbp" || lower == "eur":
			if i+1 < len(fields) {
				next := strings.TrimSpace(strings.Trim(fields[i+1], ",.;:"))
				if isAmountNumber(next) {
					return strings.ToUpper(token) + " " + next, true
				}
			}
		default:
			if isAmountNumber(token) {
				if i > 0 {
					prev := strings.TrimSpace(strings.Trim(fields[i-1], ",.;:"))
					switch strings.ToLower(prev) {
					case "usd", "gbp", "eur":
						return strings.ToUpper(prev) + " " + token, true
					}
				}
				if i+1 < len(fields) {
					next := strings.TrimSpace(strings.Trim(fields[i+1], ",.;:"))
					switch strings.ToLower(next) {
					case "usd", "gbp", "eur":
						return strings.ToUpper(next) + " " + token, true
					}
				}
			}
		}
	}
	return "", false
}

func isAmountNumber(token string) bool {
	token = strings.TrimSpace(strings.Trim(token, ",.;:"))
	if token == "" {
		return false
	}
	digits := 0
	dots := 0
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

func extractLabeledValue(line string, labels []string) (string, bool) {
	lower := strings.ToLower(normalizeWhitespace(line))
	for _, label := range labels {
		labelLower := strings.ToLower(label)
		idx := strings.Index(lower, labelLower)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(label):])
		rest = strings.TrimLeft(rest, ":-—= #\t")
		rest = strings.TrimSpace(rest)
		if rest == "" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		return strings.TrimSpace(strings.Join(fields, " ")), true
	}
	return "", false
}

func extractDateSpan(text string, now time.Time) (string, time.Time, bool) {
	cleaned := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "(", " ", ")", " ", "[", " ", "]", " ").Replace(strings.TrimSpace(text))
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return "", time.Time{}, false
	}
	maxWindow := 6
	if len(fields) < maxWindow {
		maxWindow = len(fields)
	}
	for size := maxWindow; size >= 1; size-- {
		for start := 0; start+size <= len(fields); start++ {
			candidate := strings.TrimSpace(strings.Join(fields[start:start+size], " "))
			if candidate == "" {
				continue
			}
			if due := parseDeadlineTime(candidate, now); !due.IsZero() {
				return candidate, due, true
			}
		}
	}
	return "", time.Time{}, false
}

func parseExplicitDateInText(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(strings.Trim(raw, ".;,"))
	if raw == "" {
		return time.Time{}, false
	}
	cleaned := strings.NewReplacer(",", " ", ".", " ", ";", " ", "(", " ", ")", " ", "[", " ", "]", " ").Replace(raw)
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return time.Time{}, false
	}
	maxWindow := 6
	if len(fields) < maxWindow {
		maxWindow = len(fields)
	}
	for size := maxWindow; size >= 1; size-- {
		for start := 0; start+size <= len(fields); start++ {
			candidate := strings.TrimSpace(strings.Join(fields[start:start+size], " "))
			if candidate == "" {
				continue
			}
			if parsed, ok := parseDateCandidate(candidate, now); ok {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

func parseDateCandidate(candidate string, now time.Time) (time.Time, bool) {
	candidate = strings.TrimSpace(strings.Trim(candidate, ".;,"))
	if candidate == "" {
		return time.Time{}, false
	}
	if rel, ok := parseRelativeDate(candidate, now); ok {
		return rel, true
	}
	layouts := []string{
		"2006-01-02",
		"02/01/2006",
		"01/02/2006",
		"02-01-2006",
		"01-02-2006",
		"2 Jan 2006",
		"2 January 2006",
		"Jan 2, 2006",
		"January 2, 2006",
		"Jan 2 2006",
		"January 2 2006",
		"02 Jan 2006",
		"02 January 2006",
		"2 Jan",
		"2 January",
		"Jan 2",
		"January 2",
	}
	loc := now.Location()
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, candidate, loc); err == nil {
			parsed = normalizeParsedDateYear(parsed, now)
			return parsed, true
		}
	}
	return time.Time{}, false
}

func normalizeParsedDateYear(parsed, now time.Time) time.Time {
	if parsed.IsZero() {
		return parsed
	}
	if parsed.Year() != 0 {
		return parsed
	}
	adjusted := time.Date(now.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), parsed.Nanosecond(), now.Location())
	if adjusted.Before(now.AddDate(0, 0, -1)) {
		adjusted = adjusted.AddDate(1, 0, 0)
	}
	return adjusted
}

func parseRelativeDays(raw string, now time.Time) (time.Time, bool) {
	fields := strings.Fields(raw)
	for i := 0; i+2 < len(fields); i++ {
		switch strings.ToLower(strings.Trim(fields[i], ",.;:")) {
		case "in", "within", "after", "by":
			numberToken := strings.Trim(fields[i+1], ",.;:")
			days, err := strconv.Atoi(numberToken)
			if err != nil || days < 0 {
				continue
			}
			unit := strings.ToLower(strings.Trim(fields[i+2], ",.;:"))
			switch {
			case strings.HasPrefix(unit, "day"):
				return dateAtHour(now.AddDate(0, 0, days), 17, 0), true
			case strings.HasPrefix(unit, "week"):
				return dateAtHour(now.AddDate(0, 0, days*7), 17, 0), true
			}
		}
	}
	return time.Time{}, false
}

func inferVendorHint(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	hasInvoiceSignal := false
	for _, raw := range lines {
		lower := strings.ToLower(strings.TrimSpace(normalizeWhitespace(raw)))
		if strings.Contains(lower, "invoice") || strings.Contains(lower, "receipt") || strings.Contains(lower, "statement") || strings.Contains(lower, "bill") {
			hasInvoiceSignal = true
			break
		}
	}
	if !hasInvoiceSignal {
		return ""
	}
	for i, raw := range lines {
		if i >= 12 {
			break
		}
		line := strings.TrimSpace(normalizeWhitespace(raw))
		if line == "" || isLikelyBoilerplateLine(line) {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "invoice") || strings.Contains(lower, "statement") || strings.Contains(lower, "receipt") || strings.Contains(lower, "bill") {
			continue
		}
		if strings.Contains(line, ":") || strings.Contains(line, "=") {
			continue
		}
		if strings.ContainsAny(line, "$£€") {
			continue
		}
		words := strings.Fields(line)
		if len(words) == 0 || len(words) > 8 {
			continue
		}
		hasLetter := false
		for _, r := range line {
			if unicode.IsLetter(r) {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			continue
		}
		if looksLikeVendorName(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func looksLikeVendorName(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	for _, suffix := range []string{" ltd", " ltd.", " limited", " inc", " inc.", " llc", " llp", " plc", " corp", " corporation", " company", " co", " co.", " gmbh", " sa", " s.a.", " bv"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	words := strings.Fields(line)
	if len(words) == 1 {
		return true
	}
	titleLike := 0
	for _, word := range words {
		w := strings.Trim(word, ",.;:()[]{}")
		if w == "" {
			continue
		}
		runes := []rune(w)
		if len(runes) == 0 {
			continue
		}
		if unicode.IsUpper(runes[0]) {
			titleLike++
		}
	}
	return titleLike >= minInt(2, len(words))
}

func documentDeadlineTaskLabel(line string) string {
	lower := strings.ToLower(line)
	for _, label := range []string{"deadline", "due date", "payment due", "amount due", "balance due", "submit by", "complete by", "required by", "must be received by", "respond by", "reply by", "return by"} {
		if idx := strings.Index(lower, label); idx >= 0 {
			return strings.Title(strings.TrimSpace(label))
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, sep := range []string{". ", "? ", "! ", "\n"} {
		if idx := strings.Index(text, sep); idx > 0 {
			return truncateRunes(text[:idx+1], 140)
		}
	}
	return truncateRunes(text, 140)
}

func truncateRunes(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max-1]) + "..."
}

func daysUntilEndOfWeek(now time.Time) int {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return 7 - weekday
}

func endOfMonth(now time.Time) time.Time {
	y, m, _ := now.Date()
	firstNextMonth := time.Date(y, m+1, 1, 0, 0, 0, 0, now.Location())
	return dateAtHour(firstNextMonth.AddDate(0, 0, -1), 17, 0)
}

func dateAtHour(t time.Time, hour, minute int) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, hour, minute, 0, 0, t.Location())
}
