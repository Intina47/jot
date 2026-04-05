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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	Checked     bool
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

type BrowserFormQuestionState struct {
	Field     FormField
	Answer    string
	Visible   bool
	Filled    bool
	Verified  bool
	Required  bool
	Selected  []string
	Reasoning string
}

type browserPlannedAction struct {
	Field       FilledField
	Question    BrowserFormQuestionState
	TargetLabel string
}

type BrowserFormPageModel struct {
	Title              string
	SectionTitle       string
	Questions          []BrowserFormQuestionState
	RequiredTotal      int
	RequiredAnswered   int
	RequiredUnanswered []string
	NextAvailable      bool
	SubmitAvailable    bool
	VisionUsed         bool
	VisionConfidence   string
}

type BrowserVisionQuestion struct {
	Label      string   `json:"label"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	Visible    bool     `json:"visible"`
	Answered   bool     `json:"answered"`
	Answer     string   `json:"answer"`
	Options    []string `json:"options"`
	Confidence string   `json:"confidence"`
}

type BrowserVisionPageModel struct {
	Title           string                  `json:"title"`
	SectionTitle    string                  `json:"sectionTitle"`
	NextAvailable   bool                    `json:"nextAvailable"`
	SubmitAvailable bool                    `json:"submitAvailable"`
	Questions       []BrowserVisionQuestion `json:"questions"`
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

func inspectFormWithBrowser(formURL string) (*BrowserPerception, BrowserPageSnapshot, []FormField, string, error) {
	browser, err := NewBrowserComputer(BrowserComputerOptions{StartURL: formURL, Headless: true})
	if err != nil {
		return nil, BrowserPageSnapshot{}, nil, "", err
	}
	defer browser.Close()
	var (
		bestPerception *BrowserPerception
		bestSnapshot   BrowserPageSnapshot
		bestFields     []FormField
		bestTitle      string
		lastErr        error
	)
	for attempt := 0; attempt < 8; attempt++ {
		perception, snapshot, err := browserPerceptionForFill(browser)
		if err != nil {
			lastErr = err
			if attempt < 7 {
				time.Sleep(time.Duration(200+attempt*200) * time.Millisecond)
				continue
			}
			break
		}
		fields := browserFormFieldsFromSnapshot(snapshot)
		if len(fields) > len(bestFields) || (len(fields) == len(bestFields) && browserInspectionLooksMoreSpecific(fields, bestFields)) {
			bestPerception = perception
			bestSnapshot = snapshot
			bestFields = fields
			bestTitle = snapshot.Title
		}
		if browserInspectionLooksReady(snapshot, fields) {
			return perception, snapshot, fields, snapshot.Title, nil
		}
		time.Sleep(time.Duration(200+attempt*200) * time.Millisecond)
	}
	if len(bestFields) > 0 {
		return bestPerception, bestSnapshot, bestFields, bestTitle, nil
	}
	if lastErr != nil {
		return nil, BrowserPageSnapshot{}, nil, "", lastErr
	}
	return nil, BrowserPageSnapshot{}, nil, "", errors.New("no browser-observed form fields were detected")
}

func browserInspectionLooksReady(snapshot BrowserPageSnapshot, fields []FormField) bool {
	if len(fields) == 0 {
		return false
	}
	if len(fields) == 1 {
		return !browserFieldLooksGeneric(fields[0])
	}
	generic := 0
	for _, field := range fields {
		if browserFieldLooksGeneric(field) {
			generic++
		}
	}
	return generic < len(fields)
}

func browserInspectionLooksMoreSpecific(fields, best []FormField) bool {
	if len(best) == 0 {
		return true
	}
	if len(fields) != len(best) {
		return len(fields) > len(best)
	}
	score := func(in []FormField) int {
		total := 0
		for _, field := range in {
			if !browserFieldLooksGeneric(field) {
				total += 2
			}
			if len(field.Options) > 0 {
				total++
			}
			if strings.TrimSpace(field.Label) != "" {
				total++
			}
		}
		return total
	}
	return score(fields) > score(best)
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
	fields = semanticCleanBrowserFormFields(fields, snapshot.Title)
	if browserSnapshotNeedsSupplement(snapshot, fields) {
		fields = mergeFormFields(fields, browserSupplementalFormFields(snapshot))
		fields = semanticCleanBrowserFormFields(fields, snapshot.Title)
	}
	if len(fields) == 0 {
		return browserSupplementalFormFields(snapshot)
	}
	return fields
}

func browserSnapshotNeedsSupplement(snapshot BrowserPageSnapshot, fields []FormField) bool {
	if len(fields) == 0 {
		return true
	}
	if len(fields) > 2 {
		return false
	}
	generic := 0
	for _, field := range fields {
		if browserFieldLooksGeneric(field) {
			generic++
		}
	}
	return generic == len(fields)
}

func browserFieldLooksGeneric(field FormField) bool {
	label := strings.ToLower(strings.TrimSpace(field.Label))
	if label == "" {
		return true
	}
	for _, phrase := range []string{
		"single line text",
		"multiple choice",
		"short answer",
		"paragraph",
		"drop-down",
		"dropdown",
		"choice",
		"response",
	} {
		if strings.Contains(label, phrase) {
			return true
		}
	}
	if len(field.Options) == 0 && len(label) <= 20 {
		switch label {
		case "name", "email", "address", "phone", "phone number", "date", "comment", "comments", "colour preference", "color preference", "shirt size":
			return false
		}
		if strings.Count(label, " ") <= 2 {
			return true
		}
	}
	return false
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
	rawLabel := strings.TrimSpace(element.Label)
	label := browserCleanQuestionLabel(rawLabel)
	if browserLooksLikeGenericElementLabel(rawLabel) || browserLooksLikeGenericElementLabel(label) || strings.EqualFold(label, "Your answer") || strings.EqualFold(label, "Other") {
		label = ""
	}
	if label == "" {
		label = browserCleanQuestionLabel(element.GroupLabel)
	}
	if label == "" {
		label = browserCleanQuestionLabel(element.Name)
	}
	if label == "" {
		label = browserCleanQuestionLabel(element.Placeholder)
	}
	if label == "" {
		for _, part := range strings.Split(element.Context, "|") {
			part = browserCleanQuestionLabel(part)
			if part == "" || browserLooksLikeGenericElementLabel(part) || strings.EqualFold(part, "Your answer") {
				continue
			}
			label = part
			break
		}
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

func browserLooksLikeGenericElementLabel(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	if lower == "" {
		return true
	}
	for _, phrase := range []string{
		"single line",
		"single line text",
		"multiple choice",
		"short answer",
		"paragraph",
		"other response",
		"your answer",
	} {
		if lower == phrase || lower == phrase+"." {
			return true
		}
	}
	return false
}

func browserCleanQuestionLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	lower := strings.ToLower(label)
	for _, suffix := range []string{
		" single line text.",
		" single line text",
		" multiple choice.",
		" multiple choice",
		" short answer.",
		" short answer",
		" paragraph text.",
		" paragraph text",
		" text.",
		" text",
	} {
		if strings.HasSuffix(lower, suffix) {
			label = strings.TrimSpace(label[:len(label)-len(suffix)])
			break
		}
	}
	return strings.TrimSpace(label)
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

func buildBrowserFormPageModel(snapshot BrowserPageSnapshot, planned []FilledField) BrowserFormPageModel {
	model := BrowserFormPageModel{Title: snapshot.Title}
	for _, plannedField := range planned {
		state := BrowserFormQuestionState{
			Field:    plannedField.Field,
			Answer:   plannedField.Answer,
			Required: plannedField.Field.Required,
			Visible:  browserFieldExistsOnPage(snapshot, plannedField.Field),
			Filled:   browserFieldHasAnyAnswer(snapshot, plannedField.Field),
		}
		if strings.TrimSpace(plannedField.Answer) != "" {
			state.Verified = browserFieldAnswerVerified(snapshot, plannedField.Field, plannedField.Answer)
		}
		if state.Field.Type == "radio" || state.Field.Type == "checkbox" {
			state.Selected = browserSelectedOptions(snapshot, plannedField.Field)
		}
		if state.Required {
			model.RequiredTotal++
			if browserQuestionVerifiedStrict(state) {
				model.RequiredAnswered++
			} else {
				model.RequiredUnanswered = append(model.RequiredUnanswered, state.Field.Label)
			}
		}
		model.Questions = append(model.Questions, state)
	}
	for _, element := range snapshot.Elements {
		label := strings.ToLower(strings.TrimSpace(element.Label))
		if label == "" {
			continue
		}
		if strings.Contains(label, "next") {
			model.NextAvailable = true
		}
		if strings.Contains(label, "submit") {
			model.SubmitAvailable = true
		}
	}
	return model
}

func browserVisionPrompt(snapshot BrowserPageSnapshot, planned []FilledField) string {
	var b strings.Builder
	b.WriteString("You are reviewing a screenshot of a live web form.\n")
	b.WriteString("Return JSON only.\n")
	b.WriteString("Describe only the currently visible form section.\n")
	b.WriteString("Identify visible questions, whether they are required, whether they appear answered, and whether a next or submit button is visible.\n")
	b.WriteString("Do not invent hidden fields. Be conservative.\n")
	if strings.TrimSpace(snapshot.Title) != "" {
		b.WriteString("Page title: ")
		b.WriteString(snapshot.Title)
		b.WriteString("\n")
	}
	if strings.TrimSpace(snapshot.URL) != "" {
		b.WriteString("URL: ")
		b.WriteString(snapshot.URL)
		b.WriteString("\n")
	}
	if len(planned) > 0 {
		b.WriteString("Known DOM/planned fields:\n")
		for i, field := range planned {
			b.WriteString(fmt.Sprintf("%d. %s", i+1, field.Field.Label))
			if field.Field.Required {
				b.WriteString(" [required]")
			}
			if len(field.Field.Options) > 0 {
				b.WriteString(" options=")
				b.WriteString(strings.Join(field.Field.Options, ", "))
			}
			if strings.TrimSpace(field.Answer) != "" {
				b.WriteString(" plannedAnswer=")
				b.WriteString(field.Answer)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func browserVisionSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":           map[string]any{"type": "string"},
			"sectionTitle":    map[string]any{"type": "string"},
			"nextAvailable":   map[string]any{"type": "boolean"},
			"submitAvailable": map[string]any{"type": "boolean"},
			"questions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":      map[string]any{"type": "string"},
						"type":       map[string]any{"type": "string"},
						"required":   map[string]any{"type": "boolean"},
						"visible":    map[string]any{"type": "boolean"},
						"answered":   map[string]any{"type": "boolean"},
						"answer":     map[string]any{"type": "string"},
						"options":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"confidence": map[string]any{"type": "string"},
					},
					"required": []string{"label"},
				},
			},
		},
	}
}

func fuseBrowserPageModels(dom BrowserFormPageModel, vision BrowserVisionPageModel) BrowserFormPageModel {
	if len(vision.Questions) == 0 && !vision.NextAvailable && !vision.SubmitAvailable {
		return dom
	}
	dom.VisionUsed = true
	index := map[string]int{}
	for i, question := range dom.Questions {
		index[strings.ToLower(strings.TrimSpace(question.Field.Label))] = i
	}
	for _, question := range vision.Questions {
		label := strings.TrimSpace(question.Label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if idx, ok := index[key]; ok {
			existing := dom.Questions[idx]
			if !existing.Visible {
				existing.Visible = question.Visible
			}
			if !existing.Filled && question.Answered {
				existing.Filled = true
				existing.Answer = strings.TrimSpace(question.Answer)
			}
			if existing.Answer == "" && strings.TrimSpace(question.Answer) != "" {
				existing.Answer = strings.TrimSpace(question.Answer)
			}
			if !existing.Required && question.Required {
				existing.Required = true
			}
			if existing.Reasoning == "" && strings.TrimSpace(question.Confidence) != "" {
				existing.Reasoning = "vision: " + strings.TrimSpace(question.Confidence)
			}
			dom.Questions[idx] = existing
			continue
		}
		field := FormField{
			ID:       sanitizeFieldID(label),
			Label:    label,
			Type:     browserVisionFieldType(question),
			Required: question.Required,
			Options:  uniqueTrimmedStrings(question.Options),
		}
		dom.Questions = append(dom.Questions, BrowserFormQuestionState{
			Field:     field,
			Answer:    strings.TrimSpace(question.Answer),
			Visible:   question.Visible,
			Filled:    question.Answered || strings.TrimSpace(question.Answer) != "",
			Verified:  question.Answered || strings.TrimSpace(question.Answer) != "",
			Required:  question.Required,
			Selected:  uniqueTrimmedStrings(question.Options),
			Reasoning: "vision: " + strings.TrimSpace(question.Confidence),
		})
	}
	dom.NextAvailable = dom.NextAvailable || vision.NextAvailable
	dom.SubmitAvailable = dom.SubmitAvailable || vision.SubmitAvailable
	if strings.TrimSpace(vision.SectionTitle) != "" {
		dom.SectionTitle = strings.TrimSpace(vision.SectionTitle)
	}
	dom.RequiredTotal = 0
	dom.RequiredAnswered = 0
	dom.RequiredUnanswered = nil
	for _, question := range dom.Questions {
		if !question.Required {
			continue
		}
		dom.RequiredTotal++
		if question.Filled {
			dom.RequiredAnswered++
		} else {
			dom.RequiredUnanswered = append(dom.RequiredUnanswered, question.Field.Label)
		}
	}
	dom.VisionConfidence = browserVisionConfidenceLabel(vision.Questions)
	return dom
}

func browserVisionFieldType(question BrowserVisionQuestion) string {
	fieldType := strings.ToLower(strings.TrimSpace(question.Type))
	switch fieldType {
	case "text", "textarea", "radio", "checkbox", "email", "phone", "date", "select":
		return fieldType
	default:
		if len(question.Options) > 1 {
			return "radio"
		}
		return "text"
	}
}

func buildBrowserFormPageModelWithVision(provider ModelProvider, browser BrowserComputer, snapshot BrowserPageSnapshot, planned []FilledField) BrowserFormPageModel {
	model := buildBrowserFormPageModel(snapshot, planned)
	if provider == nil {
		return model
	}
	perception, _, err := browserPerceptionForFill(browser)
	if err != nil || perception == nil {
		return model
	}
	vision, err := browserVisionPageModel(*perception, planned, provider)
	if err != nil {
		return model
	}
	return mergeBrowserVisionPageModel(model, vision)
}

func browserVisionPageModel(perception BrowserPerception, planned []FilledField, provider ModelProvider) (BrowserVisionPageModel, error) {
	visionProvider, ok := provider.(VisionModelProvider)
	if !ok {
		return BrowserVisionPageModel{}, errors.New("vision provider is not available")
	}
	if len(perception.Screenshot) == 0 {
		return BrowserVisionPageModel{}, errors.New("browser perception has no screenshot")
	}
	schema := `{
  "type": "object",
  "properties": {
    "title": {"type": "string"},
    "sectionTitle": {"type": "string"},
    "nextAvailable": {"type": "boolean"},
    "submitAvailable": {"type": "boolean"},
    "questions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "label": {"type": "string"},
          "type": {"type": "string"},
          "required": {"type": "boolean"},
          "visible": {"type": "boolean"},
          "answered": {"type": "boolean"},
          "answer": {"type": "string"},
          "options": {"type": "array", "items": {"type": "string"}},
          "confidence": {"type": "string"}
        },
        "required": ["label", "visible", "answered"]
      }
    }
  },
  "required": ["questions", "nextAvailable", "submitAvailable"]
}`
	resp, err := visionProvider.VisionChat([]VisionMessage{{
		Role:    "user",
		Content: browserVisionPrompt(perception.Snapshot, planned),
		Images:  []VisionImage{{Data: perception.Screenshot, MIMEType: "image/png"}},
	}}, nil, json.RawMessage(schema))
	if err != nil {
		return BrowserVisionPageModel{}, err
	}
	var parsed BrowserVisionPageModel
	if err := json.Unmarshal([]byte(extractJSONObject(resp)), &parsed); err != nil {
		return BrowserVisionPageModel{}, err
	}
	return parsed, nil
}

func mergeBrowserVisionPageModel(model BrowserFormPageModel, vision BrowserVisionPageModel) BrowserFormPageModel {
	model.VisionUsed = true
	if strings.TrimSpace(vision.Title) != "" {
		model.Title = strings.TrimSpace(vision.Title)
	}
	if strings.TrimSpace(vision.SectionTitle) != "" {
		model.SectionTitle = strings.TrimSpace(vision.SectionTitle)
	}
	model.NextAvailable = model.NextAvailable || vision.NextAvailable
	model.SubmitAvailable = model.SubmitAvailable || vision.SubmitAvailable
	index := map[string]int{}
	for i, question := range model.Questions {
		index[strings.ToLower(strings.TrimSpace(question.Field.Label))] = i
	}
	for _, question := range vision.Questions {
		label := strings.TrimSpace(question.Label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if idx, ok := index[key]; ok {
			current := model.Questions[idx]
			current.Visible = current.Visible || question.Visible
			current.Required = current.Required || question.Required
			current.Filled = current.Filled || question.Answered
			if current.Answer == "" && strings.TrimSpace(question.Answer) != "" {
				current.Answer = strings.TrimSpace(question.Answer)
			}
			if !current.Verified && current.Answer != "" && strings.EqualFold(strings.TrimSpace(current.Answer), strings.TrimSpace(question.Answer)) {
				current.Verified = true
			}
			if current.Reasoning == "" && strings.TrimSpace(question.Confidence) != "" {
				current.Reasoning = "vision: " + strings.TrimSpace(question.Confidence)
			}
			model.Questions[idx] = current
			continue
		}
		field := FormField{
			ID:       sanitizeFieldID(label),
			Label:    label,
			Type:     browserVisionFieldType(question),
			Required: question.Required,
			Options:  uniqueTrimmedStrings(question.Options),
		}
		model.Questions = append(model.Questions, BrowserFormQuestionState{
			Field:     field,
			Answer:    strings.TrimSpace(question.Answer),
			Visible:   question.Visible,
			Filled:    question.Answered || strings.TrimSpace(question.Answer) != "",
			Verified:  question.Answered || strings.TrimSpace(question.Answer) != "",
			Required:  question.Required,
			Selected:  uniqueTrimmedStrings(question.Options),
			Reasoning: "vision: " + strings.TrimSpace(question.Confidence),
		})
	}
	model.RequiredTotal = 0
	model.RequiredAnswered = 0
	model.RequiredUnanswered = nil
	for _, question := range model.Questions {
		if !question.Required {
			continue
		}
		model.RequiredTotal++
		if browserQuestionVerifiedStrict(question) {
			model.RequiredAnswered++
		} else {
			model.RequiredUnanswered = append(model.RequiredUnanswered, question.Field.Label)
		}
	}
	model.VisionConfidence = browserVisionConfidence(vision)
	return model
}

func browserVisionConfidence(vision BrowserVisionPageModel) string {
	best := ""
	for _, question := range vision.Questions {
		candidate := strings.ToUpper(strings.TrimSpace(question.Confidence))
		switch candidate {
		case "HIGH":
			return "HIGH"
		case "MEDIUM":
			if best != "HIGH" {
				best = "MEDIUM"
			}
		case "LOW":
			if best == "" {
				best = "LOW"
			}
		}
	}
	return best
}

func browserVisionFields(vision BrowserVisionPageModel) []FormField {
	fields := make([]FormField, 0, len(vision.Questions))
	for _, question := range vision.Questions {
		label := strings.TrimSpace(question.Label)
		if label == "" {
			continue
		}
		fields = append(fields, FormField{
			ID:       sanitizeFieldID(label),
			Label:    label,
			Type:     browserVisionFieldType(question),
			Required: question.Required,
			Options:  uniqueTrimmedStrings(question.Options),
		})
	}
	return uniqueFormFields(fields)
}

func browserVisionConfidenceLabel(questions []BrowserVisionQuestion) string {
	best := ""
	for _, question := range questions {
		conf := strings.ToUpper(strings.TrimSpace(question.Confidence))
		switch conf {
		case "HIGH":
			return "high"
		case "MEDIUM":
			if best == "" {
				best = "medium"
			}
		case "LOW":
			if best == "" {
				best = "low"
			}
		}
	}
	return best
}

func browserFieldExistsOnPage(snapshot BrowserPageSnapshot, field FormField) bool {
	for _, element := range snapshot.Elements {
		if !element.Visible || element.Disabled {
			continue
		}
		if browserElementMatchesField(element, field) {
			return true
		}
	}
	return false
}

func browserFieldHasAnyAnswer(snapshot BrowserPageSnapshot, field FormField) bool {
	switch field.Type {
	case "radio", "checkbox":
		return len(browserSelectedOptions(snapshot, field)) > 0
	default:
		for _, element := range snapshot.Elements {
			if !element.Visible || element.Disabled || !browserElementMatchesField(element, field) {
				continue
			}
			if strings.TrimSpace(element.Value) != "" {
				return true
			}
		}
		return false
	}
}

func browserSelectedOptions(snapshot BrowserPageSnapshot, field FormField) []string {
	selected := make([]string, 0)
	for _, element := range snapshot.Elements {
		if !element.Visible || element.Disabled {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(element.Role))
		if role != "radio" && role != "checkbox" {
			continue
		}
		group := strings.ToLower(strings.TrimSpace(element.GroupLabel))
		if group == "" {
			group = strings.ToLower(strings.TrimSpace(browserSemanticGroupLabel(element)))
		}
		if group != strings.ToLower(strings.TrimSpace(field.Label)) {
			continue
		}
		if element.Checked {
			selected = append(selected, strings.TrimSpace(element.Label))
		}
	}
	return uniqueTrimmedStrings(selected)
}

func browserFieldAnswerVerified(snapshot BrowserPageSnapshot, field FormField, answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	switch field.Type {
	case "radio", "checkbox":
		selected := browserSelectedOptions(snapshot, field)
		for _, option := range selected {
			if browserAnswerMatchesOption(answer, option) {
				return true
			}
		}
		return false
	case "select":
		for _, element := range snapshot.Elements {
			if !element.Visible || element.Disabled || !browserElementMatchesField(element, field) {
				continue
			}
			if browserTextMatchesAnswer(element.Value, answer) {
				return true
			}
			for _, option := range element.Options {
				if browserAnswerMatchesOption(answer, option) && browserTextMatchesAnswer(element.Value, option) {
					return true
				}
			}
		}
		return false
	default:
		for _, element := range snapshot.Elements {
			if !element.Visible || element.Disabled || !browserElementMatchesField(element, field) {
				continue
			}
			if browserTextMatchesAnswer(element.Value, answer) {
				return true
			}
		}
		return false
	}
}

func browserTextMatchesAnswer(value, answer string) bool {
	return browserNormalizeComparable(value) == browserNormalizeComparable(answer)
}

func browserNormalizeComparable(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func browserAnswerMatchesOption(answer, option string) bool {
	answer = strings.TrimSpace(answer)
	option = strings.TrimSpace(option)
	if answer == "" || option == "" {
		return false
	}
	if strings.EqualFold(answer, option) || browserNormalizeComparable(answer) == browserNormalizeComparable(option) {
		return true
	}
	switch browserNormalizeComparable(answer) {
	case "small":
		if browserNormalizeComparable(option) == "s" {
			return true
		}
	case "medium":
		if browserNormalizeComparable(option) == "m" {
			return true
		}
	case "large":
		if browserNormalizeComparable(option) == "l" {
			return true
		}
	case "extra small":
		if browserNormalizeComparable(option) == "xs" {
			return true
		}
	case "extra large":
		if browserNormalizeComparable(option) == "xl" {
			return true
		}
	}
	return false
}

func browserElementMatchesField(element BrowserPageElement, field FormField) bool {
	fieldLabel := strings.ToLower(strings.TrimSpace(field.Label))
	if fieldLabel == "" {
		return false
	}
	candidates := []string{
		element.Label,
		element.GroupLabel,
		element.Name,
		element.Placeholder,
		element.Context,
	}
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if candidate == fieldLabel || strings.Contains(candidate, fieldLabel) || strings.Contains(fieldLabel, candidate) {
			return true
		}
	}
	return false
}

func browserSubmissionGateSatisfied(model BrowserFormPageModel) bool {
	if len(model.Questions) == 0 {
		return false
	}
	if browserRequiredPendingCount(model) > 0 {
		return false
	}
	return model.SubmitAvailable && !model.NextAvailable
}

func browserRequiredPendingCount(model BrowserFormPageModel) int {
	pending := 0
	for _, question := range model.Questions {
		if !question.Required {
			continue
		}
		if !browserQuestionVerifiedStrict(question) {
			pending++
		}
	}
	return pending
}

func browserQuestionVerifiedStrict(question BrowserFormQuestionState) bool {
	if !question.Required {
		return true
	}
	if strings.TrimSpace(question.Answer) == "" {
		return false
	}
	if !question.Verified {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(question.Field.Type)) {
	case "radio", "checkbox":
		return len(question.Selected) > 0
	default:
		return true
	}
}

func browserCanAdvancePage(model BrowserFormPageModel) bool {
	if !model.NextAvailable {
		return false
	}
	if len(model.RequiredUnanswered) > 0 {
		return false
	}
	if browserSubmissionGateSatisfied(model) {
		return false
	}
	return true
}

func browserAdvanceFormPage(browser BrowserComputer, before BrowserPageSnapshot, model BrowserFormPageModel) (BrowserPageSnapshot, bool, error) {
	if !browserCanAdvancePage(model) {
		return before, false, nil
	}
	targets := []string{"Next", "Continue", "Review"}
	beforeSig := browserSnapshotSignature(before)
	for _, target := range targets {
		if err := browser.Click(target); err != nil {
			continue
		}
		_, after, err := browserPerceptionForFill(browser)
		if err != nil {
			return before, false, err
		}
		if browserSnapshotSignature(after) != beforeSig {
			return after, true, nil
		}
	}
	return before, false, nil
}

func browserSnapshotSignature(snapshot BrowserPageSnapshot) string {
	var parts []string
	parts = append(parts, browserNormalizeComparable(snapshot.Title))
	parts = append(parts, browserNormalizeComparable(snapshot.URL))
	visible := 0
	for _, element := range snapshot.Elements {
		if !element.Visible || element.Disabled {
			continue
		}
		label := browserElementSignatureLabel(element)
		if label == "" {
			continue
		}
		parts = append(parts, browserNormalizeComparable(label))
		visible++
		if visible >= 12 {
			break
		}
	}
	return strings.Join(parts, "|")
}

func browserElementSignatureLabel(element BrowserPageElement) string {
	for _, candidate := range []string{
		element.Label,
		element.GroupLabel,
		element.Name,
		element.Placeholder,
		element.Context,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func fillFormInBrowserForReview(result *FormFillResult, out io.Writer, browserProfilePath string, provider ModelProvider) (string, error) {
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
	_, snapshot, err := browserPerceptionForFill(browser)
	if err != nil {
		return "", err
	}
	if formRequiresSignIn(snapshot) {
		return "form requires a signed-in browser session before Jot can fill it automatically", nil
	}
	if out != nil {
		fmt.Fprintln(out, renderAssistantStatusLine("opening browser and filling suggested answers..."))
	}
	for round := 0; round < 20; round++ {
		model := buildBrowserFormPageModelWithVision(provider, browser, snapshot, result.Fields)
		action, ok := browserNextPlannedAction(result.Fields, model)
		if ok {
			label := strings.TrimSpace(action.TargetLabel)
			if label == "" {
				label = strings.TrimSpace(action.Field.Field.Label)
			}
			if out != nil {
				fmt.Fprintln(out, renderAssistantStatusLine(fmt.Sprintf("filling question %d/%d: %s...", browserVerifiedQuestionCount(model)+1, max(1, len(model.Questions)), label)))
			}
			if err := browserApplyPlannedAction(browser, action); err != nil {
				return "", err
			}
			_, snapshot, err = browserPerceptionForFill(browser)
			if err != nil {
				return "", err
			}
			if !browserActionVerified(snapshot, action) {
				result.Notes = append(result.Notes, fmt.Sprintf("could not verify %s after filling it in the browser", label))
			}
			continue
		}
		if browserCanAdvancePage(model) {
			nextSnapshot, advanced, advanceErr := browserAdvanceFormPage(browser, snapshot, model)
			if advanceErr != nil {
				return "", advanceErr
			}
			if advanced {
				if out != nil {
					fmt.Fprintln(out, renderAssistantStatusLine("moving to the next form page..."))
				}
				snapshot = nextSnapshot
				continue
			}
		}
		break
	}
	model := buildBrowserFormPageModelWithVision(provider, browser, snapshot, result.Fields)
	if browserSubmissionGateSatisfied(model) {
		result.ReadyToSubmit = true
	} else {
		result.ReadyToSubmit = false
	}
	return browserCompletionAuditMessage(model), nil
}

func browserPerceptionForFill(browser BrowserComputer) (*BrowserPerception, BrowserPageSnapshot, error) {
	type perceiver interface {
		Perceive() (BrowserPerception, error)
	}
	if p, ok := browser.(perceiver); ok {
		perception, err := p.Perceive()
		if err != nil {
			return nil, BrowserPageSnapshot{}, err
		}
		return &perception, perception.Snapshot, nil
	}
	snapshot, err := browser.Snapshot()
	if err != nil {
		return nil, BrowserPageSnapshot{}, err
	}
	return nil, snapshot, nil
}

func browserPlannedFillActions(fields []FilledField, model BrowserFormPageModel) []FilledField {
	actions := make([]FilledField, 0, len(fields))
	visible := map[string]BrowserFormQuestionState{}
	hasVisibleQuestions := false
	for _, question := range model.Questions {
		if question.Visible {
			hasVisibleQuestions = true
			visible[strings.ToLower(strings.TrimSpace(question.Field.Label))] = question
		}
	}
	for _, field := range fields {
		if field.Skipped || strings.TrimSpace(field.Answer) == "" {
			continue
		}
		if !hasVisibleQuestions {
			if browserQuestionAlreadyVerified(model, field.Field, field.Answer) {
				continue
			}
			actions = append(actions, field)
			continue
		}
		_, ok := visible[strings.ToLower(strings.TrimSpace(field.Field.Label))]
		if !ok {
			continue
		}
		if browserQuestionAlreadyVerified(model, field.Field, field.Answer) {
			continue
		}
		actions = append(actions, field)
	}
	return actions
}

func browserNextPlannedAction(fields []FilledField, model BrowserFormPageModel) (browserPlannedAction, bool) {
	questions := append([]BrowserFormQuestionState(nil), model.Questions...)
	sort.SliceStable(questions, func(i, j int) bool {
		if questions[i].Required != questions[j].Required {
			return questions[i].Required
		}
		if questions[i].Verified != questions[j].Verified {
			return !questions[i].Verified
		}
		if questions[i].Filled != questions[j].Filled {
			return !questions[i].Filled
		}
		return strings.ToLower(strings.TrimSpace(questions[i].Field.Label)) < strings.ToLower(strings.TrimSpace(questions[j].Field.Label))
	})
	for _, question := range questions {
		if !question.Visible || browserQuestionAlreadySatisfied(question) {
			continue
		}
		if action, ok := browserResolveQuestionAction(fields, question, model); ok {
			return action, true
		}
	}
	return browserPlannedAction{}, false
}

func browserResolveQuestionAction(fields []FilledField, question BrowserFormQuestionState, model BrowserFormPageModel) (browserPlannedAction, bool) {
	var fallback *FilledField
	for _, field := range fields {
		if field.Skipped || strings.TrimSpace(field.Answer) == "" {
			continue
		}
		if browserQuestionAlreadyVerified(model, field.Field, field.Answer) {
			continue
		}
		if !browserFieldMatchesQuestion(field, question) {
			continue
		}
		candidate := field
		if browserFieldQuestionStrength(field, question) >= 3 {
			return browserPlannedAction{
				Field:       candidate,
				Question:    question,
				TargetLabel: browserBestQuestionTargetLabel(question, field.Field),
			}, true
		}
		if fallback == nil {
			tmp := candidate
			fallback = &tmp
		}
	}
	if fallback != nil {
		return browserPlannedAction{
			Field:       *fallback,
			Question:    question,
			TargetLabel: browserBestQuestionTargetLabel(question, fallback.Field),
		}, true
	}
	return browserPlannedAction{}, false
}

func browserQuestionAlreadySatisfied(question BrowserFormQuestionState) bool {
	if question.Verified {
		return true
	}
	if !question.Required && question.Filled {
		return true
	}
	return false
}

func browserFieldMatchesQuestion(field FilledField, question BrowserFormQuestionState) bool {
	if browserFieldQuestionStrength(field, question) > 0 {
		return true
	}
	return false
}

func browserFieldQuestionStrength(field FilledField, question BrowserFormQuestionState) int {
	fieldLabel := browserNormalizeComparable(field.Field.Label)
	questionLabel := browserNormalizeComparable(question.Field.Label)
	if fieldLabel == "" || questionLabel == "" {
		return 0
	}
	score := 0
	if fieldLabel == questionLabel {
		score += 5
	}
	if strings.Contains(fieldLabel, questionLabel) || strings.Contains(questionLabel, fieldLabel) {
		score += 3
	}
	if field.Field.Type != "" && question.Field.Type != "" && field.Field.Type == question.Field.Type {
		score++
	}
	if field.Semantic != SemanticUnknown && field.Semantic == ClassifyField(question.Field, question.Field.Label) {
		score++
	}
	if len(field.Field.Options) > 0 && len(question.Field.Options) > 0 {
		for _, have := range field.Field.Options {
			for _, want := range question.Field.Options {
				if browserAnswerMatchesOption(have, want) {
					score++
					return score
				}
			}
		}
	}
	return score
}

func browserBestQuestionTargetLabel(question BrowserFormQuestionState, field FormField) string {
	for _, candidate := range []string{
		question.Field.Label,
		field.Label,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func browserQuestionAlreadyVerified(model BrowserFormPageModel, field FormField, answer string) bool {
	for _, question := range model.Questions {
		if strings.EqualFold(strings.TrimSpace(question.Field.Label), strings.TrimSpace(field.Label)) &&
			strings.EqualFold(strings.TrimSpace(question.Answer), strings.TrimSpace(answer)) &&
			question.Verified {
			return true
		}
	}
	return false
}

func browserApplyFilledField(browser BrowserComputer, field FilledField) error {
	switch field.Field.Type {
	case "radio", "checkbox":
		return browser.Click(field.Answer)
	case "select":
		return browser.Select(field.Field.Label, field.Answer)
	default:
		return browser.Type(field.Field.Label, field.Answer)
	}
}

func browserApplyPlannedAction(browser BrowserComputer, action browserPlannedAction) error {
	target := strings.TrimSpace(action.TargetLabel)
	if target == "" {
		target = strings.TrimSpace(action.Field.Field.Label)
	}
	switch action.Field.Field.Type {
	case "radio", "checkbox":
		return browser.Click(action.Field.Answer)
	case "select":
		return browser.Select(target, action.Field.Answer)
	default:
		return browser.Type(target, action.Field.Answer)
	}
}

func browserActionVerified(snapshot BrowserPageSnapshot, action browserPlannedAction) bool {
	field := action.Field.Field
	if strings.TrimSpace(action.TargetLabel) != "" {
		field.Label = action.TargetLabel
	}
	return browserFieldAnswerVerified(snapshot, field, action.Field.Answer)
}

func browserCompletionAuditMessage(model BrowserFormPageModel) string {
	verified := 0
	for _, question := range model.Questions {
		if question.Verified {
			verified++
		}
	}
	if pending := browserRequiredPendingCount(model); pending > 0 {
		labels := browserRequiredPendingLabels(model)
		if len(labels) == 0 {
			labels = append(labels, model.RequiredUnanswered...)
		}
		return fmt.Sprintf("the form is open in the browser with %d verified answer(s). %d required question(s) still need your review: %s", verified, pending, strings.Join(labels, ", "))
	}
	if browserSubmissionGateSatisfied(model) {
		return fmt.Sprintf("the form is open in the browser with %d verified answer(s). the required questions appear complete; review it there and submit when you're happy", verified)
	}
	if browserCanAdvancePage(model) {
		return fmt.Sprintf("the current page is complete with %d verified answer(s). move to the next page in the browser to continue", verified)
	}
	if model.SubmitAvailable {
		return fmt.Sprintf("submit is visible, but the form is not ready yet. %d required question(s) still need attention", browserRequiredPendingCount(model))
	}
	return fmt.Sprintf("the form is open in the browser with %d verified answer(s). review it there before continuing", verified)
}

func browserRequiredPendingLabels(model BrowserFormPageModel) []string {
	labels := make([]string, 0)
	for _, question := range model.Questions {
		if !question.Required {
			continue
		}
		if browserQuestionVerifiedStrict(question) {
			continue
		}
		labels = append(labels, strings.TrimSpace(question.Field.Label))
	}
	return uniqueTrimmedStrings(labels)
}

func browserVerifiedQuestionCount(model BrowserFormPageModel) int {
	count := 0
	for _, question := range model.Questions {
		if browserQuestionVerifiedStrict(question) {
			count++
		}
	}
	return count
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
	if out != nil {
		fmt.Fprintln(out, renderAssistantStatusLine("inspecting form fields..."))
	}
	baseEmail, threadEmails, recent, formURL, err := resolveAssistantFormContext(gmail, call, in, out)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	perception, snapshot, fields, title, err := inspectFormWithBrowser(formURL)
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
	if perception != nil {
		if vision, visionErr := browserVisionPageModel(*perception, nil, session.Provider); visionErr == nil {
			fields = mergeFormFields(fields, browserVisionFields(vision))
		}
	}
	if out != nil {
		status := "searching available context for answers..."
		if len(threadEmails) > 0 || len(recent) > 0 {
			status = "searching your emails for answers..."
		}
		fmt.Fprintln(out, renderAssistantStatusLine(status))
	}
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

	unknownCount := 0
	for _, field := range result.Fields {
		if field.Confidence == ConfidenceUnknown || strings.TrimSpace(field.Answer) == "" {
			unknownCount++
		}
	}
	result.ReadyToSubmit = false

	actionText, browserErr := fillFormInBrowserForReview(&result, out, session.Config.BrowserProfilePath, session.Provider)
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

func resolveAssistantFormContext(gmail *GmailCapability, call AssistantToolCall, in io.Reader, out io.Writer) (NormalizedEmail, []NormalizedEmail, []NormalizedEmail, string, error) {
	messageID := firstStringParam(call.Params, "message_id", "id")
	threadID := firstStringParam(call.Params, "thread_id")
	formURL := firstStringParam(call.Params, "form_url", "url")

	baseEmail := NormalizedEmail{
		Subject:  "Direct form link",
		BodyText: strings.TrimSpace(formURL),
	}
	var threadEmails []NormalizedEmail
	var recent []NormalizedEmail

	if messageID == "" && threadID == "" {
		if strings.TrimSpace(formURL) == "" {
			return NormalizedEmail{}, nil, nil, "", errors.New("gmail.fill_form requires form_url, message_id, or thread_id")
		}
		return baseEmail, threadEmails, recent, formURL, nil
	}
	if gmail == nil {
		return NormalizedEmail{}, nil, nil, "", errors.New("gmail capability is not configured")
	}

	var thread gmailThreadResult
	var err error
	switch {
	case messageID != "":
		baseEmail, err = gmail.readMessage(messageID)
		if err != nil {
			return NormalizedEmail{}, nil, nil, "", err
		}
		threadID = baseEmail.ThreadID
	case threadID != "":
		thread, err = gmail.readThread(threadID)
		if err != nil {
			return NormalizedEmail{}, nil, nil, "", err
		}
		if len(thread.Messages) == 0 {
			return NormalizedEmail{}, nil, nil, "", errors.New("thread has no messages")
		}
		baseEmail = thread.Messages[0]
	}
	if threadID != "" && len(thread.Messages) == 0 {
		thread, err = gmail.readThread(threadID)
		if err != nil {
			return NormalizedEmail{}, nil, nil, "", err
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
			return NormalizedEmail{}, nil, nil, "", pickErr
		}
		formURL = link.URL
		if link.MessageID != "" && link.MessageID != baseEmail.ID {
			if selected, readErr := gmail.readMessage(link.MessageID); readErr == nil {
				baseEmail = selected
			}
		}
	}
	threadEmails = thread.Messages
	if len(threadEmails) == 0 {
		threadEmails = []NormalizedEmail{baseEmail}
	}
	recent = formRecentSenderEmails(gmail, baseEmail.From, baseEmail.ID)
	if strings.TrimSpace(baseEmail.BodyText) == "" {
		baseEmail.BodyText = strings.TrimSpace(formURL)
	}
	return baseEmail, threadEmails, recent, formURL, nil
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
	lower := strings.ToLower(htmlText)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	openEnd := strings.Index(lower[start:], ">")
	if openEnd < 0 {
		return ""
	}
	openEnd += start
	closeStart := strings.Index(lower[openEnd+1:], "</title>")
	if closeStart < 0 {
		return ""
	}
	closeStart += openEnd + 1
	return strings.TrimSpace(gmailStripHTML(htmlText[openEnd+1 : closeStart]))
}

func googleFormsExtractFieldsFromHTML(htmlText string) ([]FormField, error) {
	payloadText := googleFormsEmbeddedJSON(htmlText)
	if payloadText == "" {
		return nil, errors.New("google form structure not found")
	}
	var payload any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return nil, err
	}
	fields := googleFormsFieldsFromValue(payload, nil)
	fields = uniqueFormFields(fields)
	return fields, nil
}

func googleFormsEmbeddedJSON(htmlText string) string {
	marker := "FB_PUBLIC_LOAD_DATA_"
	idx := strings.Index(htmlText, marker)
	if idx < 0 {
		return ""
	}
	rest := htmlText[idx+len(marker):]
	eq := strings.Index(rest, "=")
	if eq < 0 {
		return ""
	}
	rest = rest[eq+1:]
	start := strings.Index(rest, "[")
	if start < 0 {
		return ""
	}
	return extractBalancedJSONArray(rest[start:])
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
	var b strings.Builder
	lastUnderscore := false
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	label = strings.Trim(b.String(), "_")
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
	var out []string
	for i := 0; i < len(text); i++ {
		if !(strings.HasPrefix(text[i:], "http://") || strings.HasPrefix(text[i:], "https://")) {
			continue
		}
		j := i
		for j < len(text) && !unicode.IsSpace(rune(text[j])) && !strings.ContainsRune(`<>()"`, rune(text[j])) {
			j++
		}
		out = append(out, text[i:j])
		i = j
	}
	return uniqueTrimmedStrings(out)
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
	for _, marker := range []string{"no later than", "before", "by"} {
		if found, ok := deadlinePhraseAfterMarker(text, marker, 5); ok {
			return found
		}
	}
	if found, ok := deadlineWithinPhrase(text); ok {
		return found
	}
	if strings.Contains(lower, "deadline") {
		return "deadline mentioned in email"
	}
	return ""
}

func deadlinePhraseAfterMarker(text, marker string, maxWords int) (string, bool) {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return "", false
	}
	phrase := strings.TrimSpace(text[idx+len(marker):])
	if phrase == "" {
		return "", false
	}
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return "", false
	}
	if len(words) > maxWords {
		words = words[:maxWords]
	}
	candidate := strings.TrimSpace(strings.Join(words, " "))
	candidate = strings.Trim(candidate, ".,;:")
	if candidate == "" {
		return "", false
	}
	return candidate, true
}

func deadlineWithinPhrase(text string) (string, bool) {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "within ")
	if idx < 0 {
		return "", false
	}
	phrase := strings.TrimSpace(text[idx+len("within "):])
	words := strings.Fields(phrase)
	if len(words) < 2 {
		return "", false
	}
	candidate := strings.Trim(strings.Join(words[:2], " "), ".,;:")
	if strings.HasPrefix(strings.ToLower(words[1]), "day") {
		return candidate, true
	}
	return "", false
}

func extractBalancedJSONArray(text string) string {
	start := strings.IndexByte(text, '[')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
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
