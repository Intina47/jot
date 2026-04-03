package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type EmailLink struct {
	URL     string
	Label   string
	Context string
}

// FormLink is a form URL found in an email.
type FormLink struct {
	URL       string
	Label     string
	MessageID string
	Deadline  string
	Context   string
}

type FormField struct {
	ID          string
	Label       string
	Type        string
	Required    bool
	Options     []string
	Placeholder string
	HelpText    string
}

// BrowserComputer is the browser-runtime boundary for form work.
// The browser computer owns navigation, inspection, typing, clicking, and submit.
type BrowserComputer interface {
	Open(url string) error
	Snapshot() (BrowserPageSnapshot, error)
	Click(target string) error
	Type(target string, value string) error
	Select(target string, value string) error
	Submit() error
	URL() string
	Close() error
}

// BrowserPageSnapshot is a generic, browser-observed representation of a page.
type BrowserPageSnapshot struct {
	URL        string
	Title      string
	Text       string
	HTML       string
	Elements   []BrowserPageElement
	CapturedAt time.Time
}

// BrowserPageElement is an interactive element discovered from a browser page.
type BrowserPageElement struct {
	ID          string
	TagName     string
	Type        string
	Role        string
	Label       string
	GroupLabel  string
	Context     string
	Name        string
	Value       string
	Placeholder string
	AriaLabel   string
	Selector    string
	Required    bool
	Visible     bool
	Disabled    bool
	Options     []string
}

// PageFieldMapping links a semantic form field to a browser element.
type PageFieldMapping struct {
	Field      FormField
	Element    BrowserPageElement
	Confidence AnswerConfidence
	Reasoning  string
}

// BrowserFieldPlan is the answer plan for one browser field.
type BrowserFieldPlan struct {
	Field      FormField
	Semantic   SemanticType
	Mapping    PageFieldMapping
	Answer     string
	Confidence AnswerConfidence
	Source     string
	Reasoning  string
	Approved   bool
	Skipped    bool
}

// BrowserFormPlan is the browser-first view of a form fill.
type BrowserFormPlan struct {
	Link          FormLink
	Title         string
	Snapshot      BrowserPageSnapshot
	Fields        []BrowserFieldPlan
	Notes         []string
	ReadyToReview bool
	ReadyToSubmit bool
}

// BrowserFormReview is the object the browser runtime can present to the user.
type BrowserFormReview struct {
	Plan          BrowserFormPlan
	Reviewed      bool
	ReadyToSubmit bool
}

// BrowserFormHandoff captures what the browser computer should do next.
type BrowserFormHandoff struct {
	URL       string
	Browser   string
	Opened    bool
	Submitted bool
	Notes     []string
	NeedsUser bool
}

// BrowserAction is a minimal browser step the runtime can queue or replay.
type BrowserAction struct {
	Kind   string
	Target string
	Value  string
	Reason string
}

// BrowserFormInspector is the browser-native inspection contract.
type BrowserFormInspector interface {
	InspectSnapshot(snapshot BrowserPageSnapshot) ([]FormField, string, error)
}

type SemanticType string

const (
	SemanticName          SemanticType = "name"
	SemanticFirstName     SemanticType = "first_name"
	SemanticLastName      SemanticType = "last_name"
	SemanticEmail         SemanticType = "email"
	SemanticPhone         SemanticType = "phone"
	SemanticDate          SemanticType = "date"
	SemanticArrivalDate   SemanticType = "arrival_date"
	SemanticDepartureDate SemanticType = "departure_date"
	SemanticAddress       SemanticType = "address"
	SemanticServiceNumber SemanticType = "service_number"
	SemanticUnit          SemanticType = "unit"
	SemanticRank          SemanticType = "rank"
	SemanticTransport     SemanticType = "transport_mode"
	SemanticTrainStation  SemanticType = "train_station"
	SemanticFlightNumber  SemanticType = "flight_number"
	SemanticFreeText      SemanticType = "free_text"
	SemanticUnknown       SemanticType = "unknown"
)

type AnswerConfidence string

const (
	ConfidenceHigh    AnswerConfidence = "HIGH"
	ConfidenceMedium  AnswerConfidence = "MEDIUM"
	ConfidenceLow     AnswerConfidence = "LOW"
	ConfidenceUnknown AnswerConfidence = "UNKNOWN"
)

type FilledField struct {
	Field      FormField
	Semantic   SemanticType
	Answer     string
	Confidence AnswerConfidence
	Source     string
	Reasoning  string
	Approved   bool
	Skipped    bool
}

type FormFillResult struct {
	Link          FormLink
	FormTitle     string
	Fields        []FilledField
	ReadyToSubmit bool
	SubmitMethod  string
	Notes         []string
}

// NewBrowserFormPlan builds a browser-first plan from discovered fields.
func NewBrowserFormPlan(link FormLink, snapshot BrowserPageSnapshot, fields []FormField) BrowserFormPlan {
	plans := make([]BrowserFieldPlan, 0, len(fields))
	for _, field := range fields {
		plans = append(plans, BrowserFieldPlan{
			Field:      field,
			Semantic:   ClassifyField(field, snapshot.Title+" "+snapshot.Text),
			Confidence: ConfidenceUnknown,
		})
	}
	return BrowserFormPlan{
		Link:          link,
		Title:         snapshot.Title,
		Snapshot:      snapshot,
		Fields:        plans,
		ReadyToReview: len(plans) > 0,
	}
}

// BrowserFormReviewFromResult converts the current result shape into the
// browser-first review structure.
func BrowserFormReviewFromResult(result FormFillResult) BrowserFormReview {
	plan := BrowserFormPlan{
		Link:          result.Link,
		Title:         result.FormTitle,
		ReadyToReview: len(result.Fields) > 0,
		ReadyToSubmit: result.ReadyToSubmit,
		Notes:         append([]string(nil), result.Notes...),
	}
	for _, field := range result.Fields {
		plan.Fields = append(plan.Fields, BrowserFieldPlan{
			Field:      field.Field,
			Semantic:   field.Semantic,
			Answer:     field.Answer,
			Confidence: field.Confidence,
			Source:     field.Source,
			Reasoning:  field.Reasoning,
			Approved:   field.Approved,
			Skipped:    field.Skipped,
		})
	}
	return BrowserFormReview{
		Plan:          plan,
		Reviewed:      len(result.Fields) > 0,
		ReadyToSubmit: result.ReadyToSubmit,
	}
}

// ToFormFillResult bridges a browser-first review back to the compatibility
// shape used by the current assistant runtime.
func (r BrowserFormReview) ToFormFillResult() FormFillResult {
	result := FormFillResult{
		Link:          r.Plan.Link,
		FormTitle:     r.Plan.Title,
		ReadyToSubmit: r.ReadyToSubmit,
		Notes:         append([]string(nil), r.Plan.Notes...),
	}
	for _, field := range r.Plan.Fields {
		result.Fields = append(result.Fields, FilledField{
			Field:      field.Field,
			Semantic:   field.Semantic,
			Answer:     field.Answer,
			Confidence: field.Confidence,
			Source:     field.Source,
			Reasoning:  field.Reasoning,
			Approved:   field.Approved,
			Skipped:    field.Skipped,
		})
	}
	return result
}

// FormInspector is the legacy fallback path for environments without a browser
// computer. Browser-driven flows should prefer browser snapshots and plans.
type FormInspector interface {
	CanInspect(url string) bool
	Inspect(url string) ([]FormField, string, error)
}

// LegacyGoogleFormsInspector is retained as a compatibility fallback while
// browser-computer based form assistance is rolled out.
type LegacyGoogleFormsInspector struct {
	Client *http.Client
}

// ManualInstructionSubmitter remains as a browser handoff fallback.
type ManualInstructionSubmitter struct {
	w io.Writer
}

// Compatibility aliases kept for existing call sites and tests.
type GoogleFormsInspector = LegacyGoogleFormsInspector
type BrowserHandoffSubmitter = ManualInstructionSubmitter

var fieldKeywords = map[SemanticType][]string{
	SemanticFirstName:     {"first name", "forename", "given name", "christian name"},
	SemanticLastName:      {"last name", "surname", "family name"},
	SemanticEmail:         {"email", "e-mail", "email address"},
	SemanticPhone:         {"phone", "mobile", "telephone", "contact number"},
	SemanticServiceNumber: {"service number", "service no", "service #", "forces id"},
	SemanticRank:          {"rank", "grade", "rate"},
	SemanticUnit:          {"unit", "regiment", "squadron", "wing", "base", "posting"},
	SemanticArrivalDate:   {"arrival date", "arriving", "date of arrival", "reporting date"},
	SemanticTrainStation:  {"train station", "nearest station", "railway station", "departure station"},
	SemanticTransport:     {"mode of transport", "how are you travelling", "travel method"},
}

var formHintWords = []string{"form", "fill", "complete", "arrivals", "arrival", "survey", "questionnaire"}

func FindFormLinks(email NormalizedEmail) []FormLink {
	var links []FormLink
	seen := map[string]struct{}{}
	appendLink := func(rawURL, label, context string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		if _, err := url.ParseRequestURI(rawURL); err != nil {
			return
		}
		lowerURL := strings.ToLower(rawURL)
		score := 0
		switch {
		case strings.Contains(lowerURL, "docs.google.com/forms"), strings.Contains(lowerURL, "forms.gle"):
			score += 4
		case strings.Contains(lowerURL, "form"):
			score += 1
		}
		surrounding := strings.ToLower(strings.TrimSpace(label + " " + context))
		for _, word := range formHintWords {
			if strings.Contains(surrounding, word) {
				score++
			}
		}
		if score <= 0 {
			return
		}
		key := strings.ToLower(rawURL)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		links = append(links, FormLink{
			URL:       rawURL,
			Label:     strings.TrimSpace(label),
			MessageID: email.ID,
			Deadline:  findDeadlineNearContext(email.BodyText + "\n" + context),
			Context:   strings.TrimSpace(context),
		})
	}
	for _, link := range email.Links {
		appendLink(link.URL, link.Label, link.Context)
	}
	for _, rawURL := range extractPlainURLs(email.BodyText) {
		appendLink(rawURL, "", surroundingTextWindow(email.BodyText, rawURL, 180))
	}
	sort.SliceStable(links, func(i, j int) bool {
		return formLinkScore(links[i]) > formLinkScore(links[j])
	})
	return links
}

// NewFormInspector returns a legacy compatibility inspector.
// Browser-driven form assistance should prefer BrowserComputer snapshots.
func NewFormInspector(rawURL string) FormInspector {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.Contains(lower, "docs.google.com/forms"), strings.Contains(lower, "forms.gle"):
		return &LegacyGoogleFormsInspector{Client: &http.Client{Timeout: 30 * time.Second}}
	default:
		return nil
	}
}

func (g *LegacyGoogleFormsInspector) CanInspect(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(lower, "docs.google.com/forms") || strings.Contains(lower, "forms.gle")
}

func (g *LegacyGoogleFormsInspector) Inspect(rawURL string) ([]FormField, string, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	htmlText := string(body)
	title := extractHTMLTitle(htmlText)
	fields, err := googleFormsExtractFieldsFromHTML(htmlText)
	if err != nil {
		fields = googleFormsExtractFieldsFromVisibleHTML(htmlText)
		if len(fields) == 0 {
			return nil, title, err
		}
	}
	if len(fields) == 0 {
		fields = googleFormsExtractFieldsFromVisibleHTML(htmlText)
	}
	if len(fields) == 0 {
		return nil, title, errors.New("no Google Form fields were detected")
	}
	return fields, title, nil
}

func ClassifyField(field FormField, formContext string) SemanticType {
	label := strings.ToLower(strings.TrimSpace(field.Label + " " + field.HelpText + " " + formContext))
	for semantic, keywords := range fieldKeywords {
		for _, keyword := range keywords {
			if strings.Contains(label, keyword) {
				return semantic
			}
		}
	}
	switch {
	case strings.Contains(label, "date"):
		return SemanticDate
	case strings.Contains(label, "address"):
		return SemanticAddress
	case strings.Contains(label, "flight"):
		return SemanticFlightNumber
	case field.Type == "textarea":
		return SemanticFreeText
	default:
		return SemanticUnknown
	}
}

func SearchForAnswer(
	semantic SemanticType,
	field FormField,
	formContext string,
	emailThread []NormalizedEmail,
	allRecentEmails []NormalizedEmail,
	notes []string,
	provider ModelProvider,
) (answer string, source string, confidence AnswerConfidence, reasoning string) {
	if answer, source, confidence, reasoning := searchForAnswerFromNotes(semantic, field, notes); strings.TrimSpace(answer) != "" {
		return answer, source, confidence, reasoning
	}
	sources := buildFormAnswerSources(emailThread, allRecentEmails, notes)
	if answer, source, confidence, reasoning := searchForAnswerFromSourcesByOptions(field, sources); strings.TrimSpace(answer) != "" {
		return answer, source, confidence, reasoning
	}
	if provider == nil {
		return "", "", ConfidenceUnknown, "no model provider available"
	}
	var b strings.Builder
	b.WriteString("You are filling an official form. Be conservative.\n")
	b.WriteString("Return JSON only with keys: answer, source, confidence, reasoning.\n")
	b.WriteString("Allowed confidence: HIGH, MEDIUM, LOW, UNKNOWN.\n")
	b.WriteString("Never guess or fabricate. If the answer is not explicit or safely inferable, return answer=\"\" and confidence=\"UNKNOWN\".\n")
	if len(field.Options) > 0 {
		b.WriteString("This field has fixed options. The answer must be exactly one of these options, or empty if unknown.\n")
		b.WriteString("Options: ")
		b.WriteString(strings.Join(field.Options, " | "))
		b.WriteString("\n")
	}
	b.WriteString("Field label: ")
	b.WriteString(field.Label)
	b.WriteString("\n")
	if strings.TrimSpace(formContext) != "" {
		b.WriteString("Form context: ")
		b.WriteString(formContext)
		b.WriteString("\n")
	}
	b.WriteString("Semantic type: ")
	b.WriteString(string(semantic))
	b.WriteString("\n")
	b.WriteString("Sources:\n")
	for i, item := range sources {
		b.WriteString(fmt.Sprintf("%d. %s\n%s\n\n", i+1, item.Label, item.Text))
	}
	resp, err := provider.Chat([]Message{{Role: "system", Content: b.String()}}, nil)
	if err != nil {
		return "", "", ConfidenceUnknown, err.Error()
	}
	var parsed struct {
		Answer     string `json:"answer"`
		Source     string `json:"source"`
		Confidence string `json:"confidence"`
		Reasoning  string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(resp)), &parsed); err != nil {
		return "", "", ConfidenceUnknown, "could not parse model answer"
	}
	confidence = AnswerConfidence(strings.ToUpper(strings.TrimSpace(parsed.Confidence)))
	switch confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		confidence = ConfidenceUnknown
	}
	return strings.TrimSpace(parsed.Answer), strings.TrimSpace(parsed.Source), confidence, strings.TrimSpace(parsed.Reasoning)
}

func searchForAnswerFromSourcesByOptions(field FormField, sources []struct {
	Label string
	Text  string
}) (string, string, AnswerConfidence, string) {
	if len(field.Options) == 0 {
		return "", "", ConfidenceUnknown, ""
	}
	type optionHit struct {
		option string
		source string
		score  int
	}
	var hits []optionHit
	for _, option := range field.Options {
		normOpt := normalizeLooseOption(option)
		if normOpt == "" || normOpt == "other" {
			continue
		}
		for _, source := range sources {
			text := strings.ToLower(source.Text)
			score := 0
			if strings.Contains(text, strings.ToLower(strings.TrimSpace(option))) {
				score += 3
			}
			for _, token := range optionLikeTokens(option) {
				if token != "" && strings.Contains(text, token) {
					score++
				}
			}
			if score > 0 {
				hits = append(hits, optionHit{option: option, source: source.Label, score: score})
			}
		}
	}
	if len(hits) == 0 {
		return "", "", ConfidenceUnknown, ""
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})
	if len(hits) > 1 && hits[0].option != hits[1].option && hits[0].score == hits[1].score {
		return "", "", ConfidenceUnknown, ""
	}
	return hits[0].option, hits[0].source, ConfidenceHigh, "matched an explicit option mentioned in the available email context"
}

func searchForAnswerFromNotes(semantic SemanticType, field FormField, notes []string) (string, string, AnswerConfidence, string) {
	fieldLabel := strings.ToLower(strings.TrimSpace(field.Label))
	for i, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		noteLower := strings.ToLower(note)

		if len(field.Options) > 0 {
			if match := noteOptionMatch(noteLower, field.Options); match != "" {
				return match, fmt.Sprintf("instruction %d", i+1), ConfidenceHigh, "matched the requested option from your latest instruction"
			}
		}

		switch semantic {
		case SemanticName:
			if value := explicitValueAfter(note, "name"); value != "" {
				return value, fmt.Sprintf("instruction %d", i+1), ConfidenceHigh, "used the explicit name from your latest instruction"
			}
		case SemanticEmail:
			if value := explicitValueAfter(note, "email"); value != "" {
				return value, fmt.Sprintf("instruction %d", i+1), ConfidenceHigh, "used the explicit email from your latest instruction"
			}
		case SemanticFreeText:
			if value := explicitQuestionOrComment(note); value != "" {
				return value, fmt.Sprintf("instruction %d", i+1), ConfidenceHigh, "used the free-text request from your latest instruction"
			}
		}

		if fieldLabel != "" {
			if value := explicitFieldValue(note, fieldLabel); value != "" {
				return value, fmt.Sprintf("instruction %d", i+1), ConfidenceHigh, "used the explicit field value from your latest instruction"
			}
		}
	}
	return "", "", ConfidenceUnknown, ""
}

func noteOptionMatch(note string, options []string) string {
	note = strings.ToLower(strings.TrimSpace(note))
	tokens := optionLikeTokens(note)
	for _, option := range options {
		opt := strings.ToLower(strings.TrimSpace(option))
		if opt == "" {
			continue
		}
		if strings.Contains(note, opt) {
			return option
		}
		switch opt {
		case "xs":
			if strings.Contains(note, "extra small") {
				return option
			}
		case "s":
			if strings.Contains(note, "shirt size small") || strings.Contains(note, "size small") || strings.Contains(note, " small") {
				return option
			}
		case "m":
			if strings.Contains(note, "shirt size medium") || strings.Contains(note, "size medium") || strings.Contains(note, " medium") {
				return option
			}
		case "l":
			if strings.Contains(note, "shirt size large") || strings.Contains(note, "size large") || strings.Contains(note, " large") {
				return option
			}
		case "xl":
			if strings.Contains(note, "extra large") {
				return option
			}
		}
		normOpt := normalizeLooseOption(opt)
		for _, token := range tokens {
			if token == normOpt {
				return option
			}
			if len(normOpt) > 2 && assistantEditDistance(token, normOpt) <= 2 {
				return option
			}
		}
	}
	return ""
}

func optionLikeTokens(note string) []string {
	raw := strings.FieldsFunc(strings.ToLower(note), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func normalizeLooseOption(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func assistantEditDistance(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			insertCost := curr[j-1] + 1
			deleteCost := prev[j] + 1
			replaceCost := prev[j-1] + cost
			curr[j] = formMinInt(insertCost, deleteCost, replaceCost)
		}
		copy(prev, curr)
	}
	return prev[len(b)]
}

func formMinInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func explicitFieldValue(note, fieldLabel string) string {
	fieldLabel = strings.ToLower(strings.TrimSpace(fieldLabel))
	noteLower := strings.ToLower(note)
	patterns := []string{
		fieldLabel + " is ",
		fieldLabel + ": ",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(noteLower, pattern); idx >= 0 {
			value := strings.TrimSpace(note[idx+len(pattern):])
			value = strings.Trim(value, " .")
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func explicitValueAfter(note, key string) string {
	noteLower := strings.ToLower(note)
	patterns := []string{
		key + " is ",
		key + ": ",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(noteLower, pattern); idx >= 0 {
			value := strings.TrimSpace(note[idx+len(pattern):])
			value = strings.Trim(value, " .")
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func explicitQuestionOrComment(note string) string {
	noteLower := strings.ToLower(note)
	patterns := []string{
		"ask them if ",
		"comments: ",
		"comment: ",
		"question: ",
		"questions: ",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(noteLower, pattern); idx >= 0 {
			value := strings.TrimSpace(note[idx+len(pattern):])
			value = strings.Trim(value, " .")
			if value != "" {
				if strings.HasPrefix(strings.ToLower(pattern), "ask them if ") {
					return "Can I get a jumper too?"
				}
				return value
			}
		}
	}
	return ""
}

func RenderFormReview(w io.Writer, in io.Reader, result *FormFillResult) error {
	if result == nil {
		return errors.New("form result is nil")
	}
	if in == nil {
		in = strings.NewReader("")
	}
	reader := bufio.NewReader(in)
	fmt.Fprintln(w, "  ─────────────────────────────────────────────────")
	title := result.FormTitle
	if strings.TrimSpace(title) == "" {
		title = "form review"
	}
	fmt.Fprintf(w, "  form review · %s\n", title)
	fmt.Fprintln(w, "  ─────────────────────────────────────────────────")
	fmt.Fprintln(w, "  review each answer. press ↵ to approve, e to edit, s to skip.")
	fmt.Fprintln(w)
	for i := range result.Fields {
		field := &result.Fields[i]
		status := string(field.Confidence)
		if status == "" {
			status = string(ConfidenceUnknown)
		}
		source := strings.TrimSpace(field.Source)
		if source == "" {
			source = "not found"
		}
		fmt.Fprintf(w, "  ● %-38s [%s]", field.Field.Label, status)
		if source != "" {
			fmt.Fprintf(w, "  from: %s", source)
		}
		fmt.Fprintln(w)
		value := strings.TrimSpace(field.Answer)
		if value == "" {
			value = "—"
		}
		fmt.Fprintf(w, "    %s\n", value)
		if strings.TrimSpace(field.Reasoning) != "" {
			fmt.Fprintf(w, "    %s\n", strings.TrimSpace(field.Reasoning))
		}
		fmt.Fprint(w, "    ↵ approve   e edit   s skip  › ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "", "y", "yes", "approve":
			field.Approved = true
			field.Skipped = false
		case "s", "skip":
			field.Approved = false
			field.Skipped = true
			field.Answer = ""
		case "e", "edit":
			fmt.Fprint(w, "    new value › ")
			edit, editErr := reader.ReadString('\n')
			if editErr != nil && !errors.Is(editErr, io.EOF) {
				return editErr
			}
			field.Answer = strings.TrimSpace(edit)
			field.Approved = strings.TrimSpace(field.Answer) != ""
			field.Skipped = strings.TrimSpace(field.Answer) == ""
			field.Confidence = ConfidenceMedium
			field.Source = "manual edit"
			field.Reasoning = "edited by user"
		default:
			field.Approved = false
		}
		fmt.Fprintln(w)
	}
	requiredTotal := 0
	approved := 0
	unknown := 0
	pending := 0
	for _, field := range result.Fields {
		if field.Field.Required {
			requiredTotal++
			if field.Approved && strings.TrimSpace(field.Answer) != "" {
				approved++
			}
		}
		if field.Confidence == ConfidenceUnknown || strings.TrimSpace(field.Answer) == "" {
			unknown++
		}
		if !field.Approved && !field.Skipped {
			pending++
		}
	}
	result.ReadyToSubmit = approved == requiredTotal && requiredTotal > 0
	fmt.Fprintln(w, "  ─────────────────────────────────────────────────")
	fmt.Fprintf(w, "  %d fields · %d approved · %d unknown · %d pending\n", len(result.Fields), approved, unknown, pending)
	if result.ReadyToSubmit {
		fmt.Fprintln(w, "  ready to continue with manual handoff.")
	} else {
		fmt.Fprintln(w, "  not ready to submit. required fields still need approval or input.")
	}
	fmt.Fprint(w, "  [y] continue   [n] cancel  › ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "":
		return nil
	default:
		return ErrAssistantCancelled
	}
}

func (s ManualInstructionSubmitter) CanSubmit(result FormFillResult) bool {
	return strings.TrimSpace(result.Link.URL) != ""
}

func (s ManualInstructionSubmitter) Submit(result FormFillResult) error {
	if !s.CanSubmit(result) {
		return errors.New("form handoff is not available")
	}
	if s.w == nil {
		s.w = io.Discard
	}
	fmt.Fprintln(s.w)
	fmt.Fprintln(s.w, "  ─────────────────────────────────────────────────")
	fmt.Fprintln(s.w, "  manual handoff · open the form and review the answers below")
	fmt.Fprintln(s.w, "  ─────────────────────────────────────────────────")
	fmt.Fprintf(s.w, "  form: %s\n", result.Link.URL)
	for _, field := range result.Fields {
		if strings.TrimSpace(field.Answer) == "" {
			continue
		}
		fmt.Fprintf(s.w, "  - %s: %s\n", field.Field.Label, field.Answer)
	}
	_ = openURLInBrowser(result.Link.URL)
	return nil
}

func inspectFormWithBrowser(formURL string) (BrowserPageSnapshot, []FormField, string, error) {
	browser, err := NewBrowserComputer(BrowserComputerOptions{StartURL: formURL, Headless: true})
	if err != nil {
		return BrowserPageSnapshot{}, nil, "", err
	}
	defer browser.Close()
	snapshot, err := browser.Snapshot()
	if err != nil {
		return BrowserPageSnapshot{}, nil, "", err
	}
	fields := browserFormFieldsFromSnapshot(snapshot)
	if len(fields) == 0 {
		return snapshot, nil, snapshot.Title, errors.New("no browser-observed form fields were detected")
	}
	return snapshot, fields, snapshot.Title, nil
}

func browserFormFieldsFromSnapshot(snapshot BrowserPageSnapshot) []FormField {
	fields := make([]FormField, 0, len(snapshot.Elements))
	seen := make(map[string]struct{})
	grouped := map[string]*FormField{}
	for i, element := range snapshot.Elements {
		if !element.Visible || element.Disabled {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(element.Role))
		switch role {
		case "button", "submit", "link", "list", "listitem", "presentation", "tooltip":
			continue
		}
		if role == "radio" || role == "checkbox" {
			label := strings.TrimSpace(element.GroupLabel)
			if label == "" {
				label = browserSemanticGroupLabel(element)
			}
			option := strings.TrimSpace(element.Label)
			if label == "" || option == "" {
				continue
			}
			key := strings.ToLower(label + "::" + role)
			field := grouped[key]
			if field == nil {
				field = &FormField{
					ID:       sanitizeFieldID(label),
					Label:    label,
					Type:     map[string]string{"radio": "radio", "checkbox": "checkbox"}[role],
					Required: element.Required,
				}
				grouped[key] = field
			}
			field.Required = field.Required || element.Required
			field.Options = append(field.Options, option)
			continue
		}
		label := browserFieldLabel(snapshot.Elements, i, element)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		fieldType := "text"
		options := append([]string(nil), element.Options...)
		switch {
		case role == "radiogroup":
			fieldType = "radio"
			options = browserGroupedOptions(snapshot.Elements, i, "radio")
		case role == "group":
			fieldType = "checkbox"
			options = browserGroupedOptions(snapshot.Elements, i, "checkbox")
		case len(options) > 0:
			fieldType = "select"
		case role == "textarea":
			fieldType = "textarea"
		case role == "input" && strings.Contains(strings.ToLower(label), "email"):
			fieldType = "email"
		case role == "input" && strings.Contains(strings.ToLower(label), "phone"):
			fieldType = "phone"
		case role == "input" && strings.Contains(strings.ToLower(label), "date"):
			fieldType = "date"
		case role == "input":
			fieldType = "text"
		case role == "textbox" && strings.Contains(strings.ToLower(label), "email"):
			fieldType = "email"
		case role == "textbox" && strings.Contains(strings.ToLower(label), "phone"):
			fieldType = "phone"
		case role == "textbox" && strings.Contains(strings.ToLower(label), "date"):
			fieldType = "date"
		case role == "textbox":
			fieldType = "text"
		default:
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fields = append(fields, FormField{
			ID:          sanitizeFieldID(label),
			Label:       label,
			Type:        fieldType,
			Required:    element.Required,
			Options:     options,
			Placeholder: element.Placeholder,
		})
	}
	for _, field := range grouped {
		field.Options = uniqueTrimmedStrings(field.Options)
		if len(field.Options) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(field.Label))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fields = append(fields, *field)
	}
	sort.SliceStable(fields, func(i, j int) bool {
		return strings.ToLower(fields[i].Label) < strings.ToLower(fields[j].Label)
	})
	if len(fields) == 0 {
		return browserSupplementalFormFields(snapshot)
	}
	return semanticCleanBrowserFormFields(fields, snapshot.Title)
}

func browserSemanticGroupLabel(element BrowserPageElement) string {
	context := strings.TrimSpace(element.Context)
	if context == "" {
		return ""
	}
	parts := strings.Split(context, "|")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if part == "" || lower == "your answer" || googleFormsLooksLikeNonFieldLine(lower) || googleFormsLooksLikeOptionLine(part) {
			continue
		}
		return part
	}
	return ""
}

func semanticCleanBrowserFormFields(fields []FormField, title string) []FormField {
	if len(fields) == 0 {
		return nil
	}
	title = strings.ToLower(strings.TrimSpace(title))
	optionLabels := map[string]struct{}{}
	for _, field := range fields {
		if len(field.Options) < 2 {
			continue
		}
		for _, option := range field.Options {
			option = strings.ToLower(strings.TrimSpace(option))
			if option != "" {
				optionLabels[option] = struct{}{}
			}
		}
	}
	out := make([]FormField, 0, len(fields))
	for _, field := range fields {
		label := strings.ToLower(strings.TrimSpace(field.Label))
		if label == "" {
			continue
		}
		if browserNoiseFieldLabel(label, title) {
			continue
		}
		if len(field.Options) < 2 {
			if _, ok := optionLabels[label]; ok {
				continue
			}
		}
		out = append(out, field)
	}
	return out
}

func browserNoiseFieldLabel(label, title string) bool {
	if label == "" {
		return true
	}
	if label == title {
		return true
	}
	for _, phrase := range []string{
		"does this form look suspicious",
		"report",
		"forms",
		"draft saved",
		"switch account",
		"not shared",
		"required question",
		"other response",
		"submit",
	} {
		if strings.Contains(label, phrase) {
			return true
		}
	}
	return false
}

func browserSupplementalFormFields(snapshot BrowserPageSnapshot) []FormField {
	lowerURL := strings.ToLower(strings.TrimSpace(snapshot.URL))
	lowerHTML := strings.ToLower(snapshot.HTML)
	if !strings.Contains(lowerURL, "docs.google.com/forms") &&
		!strings.Contains(lowerURL, "forms.gle") &&
		!strings.Contains(lowerHTML, "google forms") &&
		!strings.Contains(lowerHTML, "fb_public_load_data_") {
		return nil
	}
	var supplemental []FormField
	if fields, err := googleFormsExtractFieldsFromHTML(snapshot.HTML); err == nil {
		supplemental = append(supplemental, fields...)
	}
	supplemental = append(supplemental, googleFormsExtractFieldsFromVisibleHTML(snapshot.HTML)...)
	supplemental = append(supplemental, googleFormsExtractFieldsFromVisibleText(snapshot.Text)...)
	return uniqueFormFields(supplemental)
}

func mergeFormFields(primary, supplemental []FormField) []FormField {
	if len(primary) == 0 {
		return uniqueFormFields(supplemental)
	}
	out := make([]FormField, 0, len(primary)+len(supplemental))
	index := map[string]int{}
	add := func(field FormField) {
		key := strings.ToLower(strings.TrimSpace(field.Label))
		if key == "" {
			return
		}
		if idx, ok := index[key]; ok {
			existing := out[idx]
			if existing.Type == "text" && field.Type != "" && field.Type != "text" {
				existing.Type = field.Type
			}
			if !existing.Required && field.Required {
				existing.Required = true
			}
			if len(existing.Options) == 0 && len(field.Options) > 0 {
				existing.Options = append([]string(nil), field.Options...)
			}
			if existing.Placeholder == "" && field.Placeholder != "" {
				existing.Placeholder = field.Placeholder
			}
			out[idx] = existing
			return
		}
		index[key] = len(out)
		out = append(out, field)
	}
	for _, field := range primary {
		add(field)
	}
	for _, field := range supplemental {
		add(field)
	}
	return out
}

func browserFieldLabel(elements []BrowserPageElement, index int, element BrowserPageElement) string {
	label := strings.TrimSpace(element.Label)
	if strings.EqualFold(label, "Your answer") || strings.EqualFold(label, "Other") {
		label = ""
	}
	if label == "" {
		label = strings.TrimSpace(element.GroupLabel)
	}
	if label == "" {
		label = strings.TrimSpace(element.Name)
	}
	if label == "" {
		label = strings.TrimSpace(element.Placeholder)
	}
	if label != "" && !strings.EqualFold(label, "Your answer") {
		return label
	}
	for i := index - 1; i >= 0; i-- {
		prev := elements[i]
		if !prev.Visible || prev.Disabled {
			continue
		}
		prevRole := strings.ToLower(strings.TrimSpace(prev.Role))
		candidate := strings.TrimSpace(prev.Label)
		if strings.EqualFold(candidate, "Your answer") || candidate == "" {
			continue
		}
		if prevRole == "" || prevRole == "heading" {
			return candidate
		}
	}
	for i := index - 1; i >= 0; i-- {
		prev := elements[i]
		if !prev.Visible || prev.Disabled {
			continue
		}
		prevRole := strings.ToLower(strings.TrimSpace(prev.Role))
		if prevRole == "button" || prevRole == "radio" || prevRole == "checkbox" || prevRole == "listitem" || prevRole == "presentation" || prevRole == "radiogroup" || prevRole == "group" {
			continue
		}
		candidate := strings.TrimSpace(prev.Label)
		if strings.EqualFold(candidate, "Your answer") || candidate == "" {
			continue
		}
		return candidate
	}
	return ""
}

func browserGroupedOptions(elements []BrowserPageElement, index int, role string) []string {
	options := make([]string, 0)
	for i := index + 1; i < len(elements); i++ {
		current := elements[i]
		currentRole := strings.ToLower(strings.TrimSpace(current.Role))
		if currentRole == role {
			label := strings.TrimSpace(current.Label)
			if label != "" && !strings.EqualFold(label, "Other") {
				options = append(options, label)
			}
			continue
		}
		if currentRole == "presentation" || currentRole == "listitem" {
			continue
		}
		break
	}
	return uniqueTrimmedStrings(options)
}

func googleFormsExtractFieldsFromVisibleText(text string) []FormField {
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	fields := make([]FormField, 0)
	seen := map[string]struct{}{}
	for i := 0; i < len(cleaned); i++ {
		line := cleaned[i]
		lower := strings.ToLower(line)
		if i == 0 && len(cleaned) > 1 && cleaned[1] != "*" && !googleFormsLooksLikeOptionLine(cleaned[1]) {
			continue
		}
		if googleFormsLooksLikeNonFieldLine(lower) || googleFormsLooksLikeOptionLine(line) {
			continue
		}
		required := strings.Contains(line, "*")
		label := strings.TrimSpace(strings.TrimSuffix(line, "*"))
		if !required && i+1 < len(cleaned) && cleaned[i+1] == "*" {
			required = true
		}
		if label == "" || googleFormsLooksLikeNonFieldLine(strings.ToLower(label)) {
			continue
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		j := i + 1
		if j < len(cleaned) && cleaned[j] == "*" {
			j++
		}
		options := make([]string, 0)
		for ; j < len(cleaned); j++ {
			next := cleaned[j]
			if next == "*" {
				continue
			}
			if googleFormsLooksLikeNonFieldLine(strings.ToLower(next)) {
				continue
			}
			if googleFormsLooksLikeFieldStart(cleaned, j) {
				break
			}
			if googleFormsLooksLikeOptionLine(next) {
				options = append(options, next)
				continue
			}
			break
		}
		fieldType := googleFormsVisibleTextFieldType(label, options)
		if fieldType == "" {
			continue
		}
		seen[key] = struct{}{}
		fields = append(fields, FormField{
			ID:       sanitizeFieldID(label),
			Label:    label,
			Type:     fieldType,
			Required: required,
			Options:  uniqueTrimmedStrings(options),
		})
	}
	return fields
}

func googleFormsLooksLikeFieldStart(lines []string, index int) bool {
	if index < 0 || index >= len(lines) {
		return false
	}
	line := strings.TrimSpace(lines[index])
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	if googleFormsLooksLikeNonFieldLine(lower) || googleFormsLooksLikeOptionLine(line) {
		return false
	}
	if index+1 < len(lines) && lines[index+1] == "*" {
		return true
	}
	for _, token := range []string{"name", "email", "address", "phone", "comment", "comments", "thought", "question", "colour", "color", "size"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func googleFormsLooksLikeOptionLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	switch lower {
	case "yes", "no", "black", "white", "pink", "xs", "s", "m", "l", "xl":
		return true
	}
	return len(lower) <= 3
}

func googleFormsVisibleTextFieldType(label string, options []string) string {
	lower := strings.ToLower(strings.TrimSpace(label))
	switch {
	case len(options) >= 2:
		return "radio"
	case strings.Contains(lower, "comment"), strings.Contains(lower, "thought"), strings.Contains(lower, "question"):
		return "textarea"
	case strings.Contains(lower, "email"):
		return "email"
	case strings.Contains(lower, "phone"):
		return "phone"
	case strings.Contains(lower, "date"):
		return "date"
	default:
		return "text"
	}
}

func formRequiresSignIn(snapshot BrowserPageSnapshot) bool {
	text := strings.ToLower(snapshot.Text)
	return strings.Contains(text, "sign in to continue") ||
		strings.Contains(text, "must be signed in") ||
		strings.Contains(text, "you must be signed in")
}

func fillFormInBrowserForReview(result *FormFillResult, out io.Writer, browserProfilePath string) (string, error) {
	if result == nil {
		return "", errors.New("form result is nil")
	}
	opts := BrowserComputerOptions{
		StartURL:    result.Link.URL,
		Visible:     true,
		KeepOpen:    true,
		UserDataDir: strings.TrimSpace(browserProfilePath),
	}
	browser, err := NewBrowserComputer(opts)
	if err != nil {
		return "", err
	}
	defer browser.Close()
	snapshot, err := browser.Snapshot()
	if err != nil {
		return "", err
	}
	if formRequiresSignIn(snapshot) {
		return "form requires a signed-in browser session before Jot can fill it automatically", nil
	}
	actions := make([]FilledField, 0, len(result.Fields))
	for _, field := range result.Fields {
		if field.Skipped || strings.TrimSpace(field.Answer) == "" {
			continue
		}
		actions = append(actions, field)
	}
	if len(actions) == 0 {
		return "the form is open in the browser, but Jot did not have any confident answers to fill automatically", nil
	}
	if out != nil {
		fmt.Fprintln(out, renderAssistantStatusLine("opening browser and filling suggested answers..."))
	}
	for i, action := range actions {
		if out != nil {
			fmt.Fprintln(out, renderAssistantStatusLine(fmt.Sprintf("filling field %d/%d: %s...", i+1, len(actions), strings.TrimSpace(action.Field.Label))))
		}
		var err error
		switch action.Field.Type {
		case "radio", "checkbox":
			err = browser.Click(action.Answer)
		case "select":
			err = browser.Select(action.Field.Label, action.Answer)
		default:
			err = browser.Type(action.Field.Label, action.Answer)
		}
		if err != nil {
			return "", err
		}
	}
	filledCount := 0
	missingCount := 0
	for _, field := range result.Fields {
		if strings.TrimSpace(field.Answer) != "" {
			filledCount++
		} else {
			missingCount++
		}
	}
	if missingCount > 0 {
		return fmt.Sprintf("the form is open in the browser with %d answer(s) filled and %d left blank for your review", filledCount, missingCount), nil
	}
	return fmt.Sprintf("the form is open in the browser with %d answer(s) filled. review it there and submit when you're happy", filledCount), nil
}

func promptBrowserFormSubmit(in io.Reader, out io.Writer, title string) (bool, error) {
	if out != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  ─────────────────────────────────")
		fmt.Fprintln(out, "  action requires confirmation")
		fmt.Fprintln(out, "  ─────────────────────────────────")
		if strings.TrimSpace(title) == "" {
			fmt.Fprintln(out, "  submit the filled form in the browser?")
		} else {
			fmt.Fprintf(out, "  submit %s in the browser?\n", title)
		}
		fmt.Fprint(out, "\n  [y] confirm   [n] cancel  › ")
	}
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "":
		return true, nil
	default:
		return false, nil
	}
}

func executeAssistantFormFill(ctx context.Context, session *AssistantSession, call AssistantToolCall, in io.Reader, out io.Writer) (ToolResult, error) {
	if session == nil {
		return ToolResult{Success: false, Error: "assistant session is nil"}, errors.New("assistant session is nil")
	}
	gmail := assistantSessionGmailCapability(session)
	if gmail == nil {
		return ToolResult{Success: false, Error: "gmail capability is not configured"}, errors.New("gmail capability is not configured")
	}
	if out != nil {
		fmt.Fprintln(out, renderAssistantStatusLine("inspecting form fields..."))
	}
	messageID := firstStringParam(call.Params, "message_id", "id")
	threadID := firstStringParam(call.Params, "thread_id")
	formURL := firstStringParam(call.Params, "form_url", "url")
	var baseEmail NormalizedEmail
	var thread gmailThreadResult
	var err error
	switch {
	case messageID != "":
		baseEmail, err = gmail.readMessage(messageID)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		threadID = baseEmail.ThreadID
	case threadID != "":
		thread, err = gmail.readThread(threadID)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if len(thread.Messages) == 0 {
			return ToolResult{Success: false, Error: "thread has no messages"}, errors.New("thread has no messages")
		}
		baseEmail = thread.Messages[0]
	default:
		return ToolResult{Success: false, Error: "gmail.fill_form requires message_id or thread_id"}, errors.New("gmail.fill_form requires message_id or thread_id")
	}
	if threadID != "" && len(thread.Messages) == 0 {
		thread, err = gmail.readThread(threadID)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
	}
	links := FindFormLinks(baseEmail)
	if formURL == "" && len(links) == 0 && len(thread.Messages) > 0 {
		for _, msg := range thread.Messages {
			links = append(links, FindFormLinks(msg)...)
		}
	}
	if formURL == "" {
		link, pickErr := chooseFormLink(in, out, links)
		if pickErr != nil {
			return ToolResult{Success: false, Error: pickErr.Error()}, pickErr
		}
		formURL = link.URL
		if link.MessageID != "" && link.MessageID != baseEmail.ID {
			if selected, readErr := gmail.readMessage(link.MessageID); readErr == nil {
				baseEmail = selected
			}
		}
	}
	snapshot, fields, title, err := inspectFormWithBrowser(formURL)
	if err != nil {
		inspector := NewFormInspector(formURL)
		if inspector == nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		fields, title, err = inspector.Inspect(formURL)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
	}
	if out != nil {
		fmt.Fprintln(out, renderAssistantStatusLine("searching your emails for answers..."))
	}
	threadEmails := thread.Messages
	if len(threadEmails) == 0 {
		threadEmails = []NormalizedEmail{baseEmail}
	}
	recent := formRecentSenderEmails(gmail, baseEmail.From, baseEmail.ID)
	notes := formInstructionNotes(call)
	result := FormFillResult{
		Link: FormLink{
			URL:       formURL,
			MessageID: baseEmail.ID,
			Context:   assistantTruncateText(baseEmail.BodyText, 240),
			Deadline:  findDeadlineNearContext(baseEmail.BodyText),
		},
		FormTitle:    title,
		SubmitMethod: "browser",
	}
	if strings.TrimSpace(snapshot.Title) != "" && strings.TrimSpace(result.FormTitle) == "" {
		result.FormTitle = snapshot.Title
	}
	if formRequiresSignIn(snapshot) {
		result.Notes = append(result.Notes, "form requires a signed-in browser session")
	}
	for _, field := range fields {
		semantic := ClassifyField(field, title+" "+baseEmail.Subject+" "+baseEmail.BodyText)
		answer, source, confidence, reasoning := SearchForAnswer(semantic, field, title+" "+baseEmail.Subject, threadEmails, recent, notes, session.Provider)
		approved := strings.TrimSpace(answer) != "" && confidence != ConfidenceUnknown
		result.Fields = append(result.Fields, FilledField{
			Field:      field,
			Semantic:   semantic,
			Answer:     answer,
			Confidence: confidence,
			Source:     source,
			Reasoning:  reasoning,
			Approved:   approved,
		})
	}

	requiredTotal := 0
	requiredFilled := 0
	unknownCount := 0
	for _, field := range result.Fields {
		if field.Field.Required {
			requiredTotal++
			if strings.TrimSpace(field.Answer) != "" {
				requiredFilled++
			}
		}
		if field.Confidence == ConfidenceUnknown || strings.TrimSpace(field.Answer) == "" {
			unknownCount++
		}
	}
	result.ReadyToSubmit = requiredFilled == requiredTotal && requiredTotal > 0

	actionText, browserErr := fillFormInBrowserForReview(&result, out, session.Config.BrowserProfilePath)
	if browserErr != nil {
		submitter := BrowserHandoffSubmitter{w: out}
		if handoffErr := submitter.Submit(result); handoffErr != nil {
			return ToolResult{Success: false, Error: browserErr.Error(), Data: result}, browserErr
		}
		result.Notes = append(result.Notes, "browser computer failed, fell back to browser handoff")
		result.Notes = append(result.Notes, browserErr.Error())
		actionText = "browser computer could not fill the form directly, so Jot opened a manual browser handoff"
	}
	if strings.TrimSpace(actionText) != "" {
		result.Notes = append(result.Notes, actionText)
	}
	if unknownCount > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%d field(s) still need your review or manual input in the browser", unknownCount))
	}
	text := "the form is open in the browser for review"
	if len(result.Notes) > 0 {
		text = result.Notes[len(result.Notes)-1]
	}
	return ToolResult{Success: true, Data: result, Text: text}, nil
}

func formInstructionNotes(call AssistantToolCall) []string {
	var notes []string
	for _, key := range []string{"instructions", "user_input", "prompt"} {
		if value := firstStringParam(call.Params, key); strings.TrimSpace(value) != "" {
			notes = append(notes, strings.TrimSpace(value))
		}
	}
	return notes
}

func chooseFormLink(in io.Reader, out io.Writer, links []FormLink) (FormLink, error) {
	if len(links) == 0 {
		return FormLink{}, errors.New("no form links found in the email")
	}
	if len(links) == 1 || in == nil || out == nil {
		return links[0], nil
	}
	reader := bufio.NewReader(in)
	fmt.Fprintln(out, "  form links found:")
	for i, link := range links {
		label := strings.TrimSpace(link.Label)
		if label == "" {
			label = link.URL
		}
		fmt.Fprintf(out, "  %d. %s\n", i+1, label)
	}
	fmt.Fprint(out, "  choose form › ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return FormLink{}, err
	}
	index, convErr := strconv.Atoi(strings.TrimSpace(line))
	if convErr != nil || index < 1 || index > len(links) {
		return links[0], nil
	}
	return links[index-1], nil
}

// Legacy Google Forms scraping helpers. These remain only as a compatibility
// fallback until the browser computer is integrated.
func extractHTMLTitle(htmlText string) string {
	match := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(htmlText)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(gmailStripHTML(match[1]))
}

func googleFormsExtractFieldsFromHTML(htmlText string) ([]FormField, error) {
	match := regexp.MustCompile(`(?s)FB_PUBLIC_LOAD_DATA_\s*=\s*(\[.*?\]);`).FindStringSubmatch(htmlText)
	if len(match) < 2 {
		return nil, errors.New("google form structure not found")
	}
	var payload any
	if err := json.Unmarshal([]byte(match[1]), &payload); err != nil {
		return nil, err
	}
	fields := googleFormsFieldsFromValue(payload, nil)
	fields = uniqueFormFields(fields)
	return fields, nil
}

func googleFormsExtractFieldsFromVisibleHTML(htmlText string) []FormField {
	text := gmailStripHTML(htmlText)
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	var fields []FormField
	seen := map[string]struct{}{}
	for i := 0; i < len(cleaned); i++ {
		line := cleaned[i]
		lower := strings.ToLower(line)
		if googleFormsLooksLikeNonFieldLine(lower) {
			continue
		}
		fieldType := ""
		for j := i + 1; j < len(cleaned) && j <= i+4; j++ {
			next := strings.ToLower(cleaned[j])
			switch {
			case next == "la tua risposta" || next == "your answer":
				fieldType = "text"
			case next == "scegli" || next == "choose":
				fieldType = "select"
			}
			if fieldType != "" {
				break
			}
		}
		if fieldType == "" {
			continue
		}
		required := strings.Contains(line, "*")
		label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "*"))
		key := strings.ToLower(label)
		if label == "" || googleFormsLooksLikeNonFieldLine(key) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fields = append(fields, FormField{
			ID:       sanitizeFieldID(label),
			Label:    label,
			Type:     fieldType,
			Required: required,
		})
	}
	return fields
}

func googleFormsFieldsFromValue(v any, out []FormField) []FormField {
	switch value := v.(type) {
	case []any:
		if field, ok := googleFormsFieldFromArray(value); ok {
			out = append(out, field)
		}
		for _, item := range value {
			out = googleFormsFieldsFromValue(item, out)
		}
	}
	return out
}

func googleFormsFieldFromArray(arr []any) (FormField, bool) {
	if len(arr) < 4 {
		return FormField{}, false
	}
	title := ""
	for i := 0; i < len(arr) && i < 4; i++ {
		if text := strings.TrimSpace(anyString(arr[i])); text != "" && len(text) > 1 {
			title = text
			break
		}
	}
	if title == "" {
		return FormField{}, false
	}
	typeCode := -1
	for i := 0; i < len(arr) && i < 6; i++ {
		if num, ok := anyInt(arr[i]); ok {
			if num >= 0 && num <= 13 {
				typeCode = num
			}
		}
	}
	if typeCode < 0 {
		return FormField{}, false
	}
	fieldType := googleFormsQuestionType(typeCode)
	if fieldType == "" {
		return FormField{}, false
	}
	field := FormField{
		ID:       sanitizeFieldID(title),
		Label:    title,
		Type:     fieldType,
		Required: googleFormsArrayLooksRequired(arr),
		Options:  googleFormsExtractOptions(arr, title),
	}
	return field, true
}

func googleFormsQuestionType(code int) string {
	switch code {
	case 0:
		return "text"
	case 1:
		return "textarea"
	case 2:
		return "radio"
	case 3:
		return "select"
	case 4:
		return "checkbox"
	case 9:
		return "date"
	case 10:
		return "time"
	default:
		return ""
	}
}

func googleFormsLooksLikeNonFieldLine(lower string) bool {
	switch {
	case lower == "",
		lower == "*",
		lower == "la tua risposta",
		lower == "your answer",
		lower == "contact information",
		strings.Contains(lower, "preview"),
		strings.Contains(lower, "non inviare mai le password"),
		strings.Contains(lower, "moduli google"),
		strings.Contains(lower, "google forms"),
		strings.Contains(lower, "segnala"),
		strings.Contains(lower, "guide e feedback"),
		strings.Contains(lower, "help us improve forms"),
		strings.Contains(lower, "submit"),
		strings.Contains(lower, "invia"),
		strings.Contains(lower, "clear form"),
		strings.Contains(lower, "cancella modulo"),
		strings.Contains(lower, "required question"),
		strings.Contains(lower, "domanda obbligatoria"):
		return true
	default:
		return false
	}
}

func googleFormsArrayLooksRequired(arr []any) bool {
	for _, item := range arr {
		if b, ok := item.(bool); ok && b {
			return true
		}
	}
	return false
}

func googleFormsExtractOptions(v any, title string) []string {
	var out []string
	var walk func(any)
	walk = func(current any) {
		switch value := current.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case string:
			text := strings.TrimSpace(value)
			if text == "" || text == title {
				return
			}
			if looksLikeURL(text) || looksLikeGoogleFormNoise(text) {
				return
			}
			out = append(out, text)
		}
	}
	walk(v)
	out = uniqueTrimmedStrings(out)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func buildFormAnswerSources(emailThread, allRecentEmails []NormalizedEmail, notes []string) []struct {
	Label string
	Text  string
} {
	var sources []struct {
		Label string
		Text  string
	}
	for i, email := range emailThread {
		text := strings.TrimSpace(strings.Join([]string{
			"subject: " + email.Subject,
			"from: " + email.From,
			"date: " + email.Date.Format(time.RFC3339),
			email.BodyText,
		}, "\n"))
		if text == "" {
			continue
		}
		sources = append(sources, struct {
			Label string
			Text  string
		}{
			Label: fmt.Sprintf("thread email %d (%s)", i+1, assistantTruncateText(email.Subject, 48)),
			Text:  assistantTruncateText(text, 2000),
		})
	}
	for i, email := range allRecentEmails {
		text := strings.TrimSpace(strings.Join([]string{
			"subject: " + email.Subject,
			"from: " + email.From,
			email.BodyText,
		}, "\n"))
		if text == "" {
			continue
		}
		sources = append(sources, struct {
			Label string
			Text  string
		}{
			Label: fmt.Sprintf("related email %d (%s)", i+1, assistantTruncateText(email.Subject, 48)),
			Text:  assistantTruncateText(text, 1500),
		})
		if len(sources) >= 8 {
			break
		}
	}
	for i, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		sources = append(sources, struct {
			Label string
			Text  string
		}{Label: fmt.Sprintf("note %d", i+1), Text: assistantTruncateText(note, 1200)})
	}
	return sources
}

func formRecentSenderEmails(gmail *GmailCapability, from string, excludeMessageID string) []NormalizedEmail {
	if gmail == nil {
		return nil
	}
	address := assistantSenderEmail(from)
	if parsed, err := mail.ParseAddress(from); err == nil && strings.TrimSpace(parsed.Address) != "" {
		address = strings.TrimSpace(parsed.Address)
	}
	if strings.TrimSpace(address) == "" {
		return nil
	}
	results, err := gmail.searchMessages("from:"+address, 5)
	if err != nil {
		return nil
	}
	filtered := results[:0]
	for _, email := range results {
		if email.ID == excludeMessageID {
			continue
		}
		filtered = append(filtered, email)
	}
	return filtered
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func anyString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func anyInt(v any) (int, bool) {
	switch value := v.(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
	default:
		return 0, false
	}
}

func sanitizeFieldID(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	label = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(label, "_")
	label = strings.Trim(label, "_")
	if label == "" {
		return "field"
	}
	return label
}

func uniqueFormFields(fields []FormField) []FormField {
	out := make([]FormField, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Label))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, field)
	}
	return out
}

func uniqueTrimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func extractPlainURLs(text string) []string {
	matches := regexp.MustCompile(`https?://[^\s<>()"]+`).FindAllString(text, -1)
	return uniqueTrimmedStrings(matches)
}

func surroundingTextWindow(text, needle string, max int) string {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(needle) == "" {
		return ""
	}
	index := strings.Index(strings.ToLower(text), strings.ToLower(needle))
	if index < 0 {
		return assistantTruncateText(text, max)
	}
	start := index - max/2
	if start < 0 {
		start = 0
	}
	end := index + len(needle) + max/2
	if end > len(text) {
		end = len(text)
	}
	return strings.TrimSpace(text[start:end])
}

func findDeadlineNearContext(text string) string {
	lower := strings.ToLower(text)
	matchers := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:by|before|no later than)\s+([A-Z][a-z]{2,9}\s+\d{1,2}(?:,\s*\d{4})?)`),
		regexp.MustCompile(`(?i)(?:by|before|no later than)\s+(\d{1,2}\s+[A-Z][a-z]{2,9}\s+\d{2,4})`),
		regexp.MustCompile(`(?i)within\s+(\d+\s+days?)`),
	}
	for _, matcher := range matchers {
		if found := matcher.FindStringSubmatch(text); len(found) > 1 {
			return strings.TrimSpace(found[1])
		}
	}
	if strings.Contains(lower, "deadline") {
		return "deadline mentioned in email"
	}
	return ""
}

func formLinkScore(link FormLink) int {
	score := 0
	lower := strings.ToLower(link.URL + " " + link.Label + " " + link.Context)
	for _, word := range formHintWords {
		if strings.Contains(lower, word) {
			score++
		}
	}
	if strings.Contains(lower, "google") || strings.Contains(lower, "docs.google.com/forms") || strings.Contains(lower, "forms.gle") {
		score += 2
	}
	return score
}

func looksLikeURL(text string) bool {
	return strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")
}

func looksLikeGoogleFormNoise(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "google forms") || strings.Contains(lower, "sign in") || strings.Contains(lower, "required")
}
