package main

import (
	"bufio"
	"bytes"
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
	"sync"
	"time"
)

const defaultAssistantMaxRounds = 8

const (
	assistantHistoryMaxMessages      = 12
	assistantHistoryMessageMaxChars  = 4000
	assistantHistoryToolMaxChars     = 2400
	assistantHistoryListPreviewLimit = 5
)

var (
	ErrAssistantCancelled      = errors.New("assistant action cancelled")
	ErrAssistantEditRequested  = errors.New("assistant edit requested")
	ErrAssistantNoProvider     = errors.New("assistant provider is not configured")
	ErrAssistantNoCapabilities = errors.New("assistant has no capabilities")
)

// AssistantSession carries the active provider, capability registry, and
// accumulated history for one assistant conversation.
type AssistantSession struct {
	Provider     ModelProvider
	Capabilities []Capability
	History      []Message
	Config       AssistantConfig
	Verbose      bool
	Format       string // "text" or "json"
	NoConfirm    bool
	Pending      *AssistantPendingAction
	ProgressFn   func(string)
}

type Message struct {
	Role    string
	Content string
	Tool    string
}

type Tool struct {
	Name        string
	Description string
	ParamSchema string
}

type ToolResult struct {
	Success bool
	Data    any
	Text    string
	Error   string
}

type Capability interface {
	Name() string
	Description() string
	Tools() []Tool
	Execute(toolName string, params map[string]any) (ToolResult, error)
}

type assistantProgressAware interface {
	SetProgressReporter(func(string))
}

// AssistantToolCall is the provider-agnostic tool directive parsed from model output.
type AssistantToolCall struct {
	Tool      string         `json:"tool"`
	Params    map[string]any `json:"params"`
	RawParams string         `json:"rawParams,omitempty"`
}

type AssistantToolExecution struct {
	Call      AssistantToolCall `json:"call"`
	Result    ToolResult        `json:"result"`
	Confirmed bool              `json:"confirmed"`
}

type AssistantTurnResult struct {
	Input         string                   `json:"input"`
	Prompt        string                   `json:"prompt,omitempty"`
	RawResponse   string                   `json:"rawResponse,omitempty"`
	FinalText     string                   `json:"finalText,omitempty"`
	LiveStatus    bool                     `json:"liveStatus,omitempty"`
	StreamedFinal bool                     `json:"streamedFinal,omitempty"`
	ToolCalls     []AssistantToolCall      `json:"toolCalls,omitempty"`
	Executions    []AssistantToolExecution `json:"executions,omitempty"`
	History       []Message                `json:"history,omitempty"`
	Warnings      []string                 `json:"warnings,omitempty"`
}

type AssistantCommandOptions struct {
	Interactive bool
	MaxRounds   int
}

type ConfirmationRequest struct {
	ToolName    string
	Description string
	Details     []string
	Params      map[string]any
}

type AssistantPendingAction struct {
	Kind       string
	Attachment *AssistantPendingAttachmentDownload
	DraftReply *AssistantPendingDraftReply
	FormFill   *AssistantPendingFormFill
}

type AssistantPendingAttachmentDownload struct {
	Items   []AssistantPendingAttachmentItem
	SaveDir string
}

type AssistantPendingAttachmentItem struct {
	MessageID  string
	ThreadID   string
	Subject    string
	From       string
	Date       time.Time
	Attachment AttachmentMeta
}

type assistantPendingAttachmentGroup struct {
	MessageID     string
	AttachmentIDs []string
	DownloadAll   bool
}

type AssistantPendingDraftReply struct {
	MessageID   string
	ThreadID    string
	Body        string
	To          string
	Subject     string
	SendAllowed bool
}

type AssistantPendingFormFill struct {
	MessageID string
	ThreadID  string
	FormURL   string
	Title     string
}

type assistantToolBinding struct {
	Capability Capability
	Tool       Tool
	FullName   string
	ShortName  string
}

const calendarAPIBaseURL = "https://www.googleapis.com/calendar/v3"

// CalendarCapability is a minimal Google Calendar capability built on top of
// the same OAuth token/config helpers used by Gmail.
type CalendarCapability struct {
	mu sync.Mutex
}

type calendarEventDateTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type calendarEventAttendee struct {
	Email string `json:"email"`
}

type calendarEventResponse struct {
	ID               string                  `json:"id"`
	Status           string                  `json:"status"`
	Summary          string                  `json:"summary"`
	Description      string                  `json:"description,omitempty"`
	Location         string                  `json:"location,omitempty"`
	HTMLLink         string                  `json:"htmlLink,omitempty"`
	CalendarID       string                  `json:"calendarId,omitempty"`
	Start            calendarEventDateTime   `json:"start"`
	End              calendarEventDateTime   `json:"end"`
	Attendees        []calendarEventAttendee `json:"attendees,omitempty"`
	RecurringEventID string                  `json:"recurringEventId,omitempty"`
}

type calendarListResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"items"`
}

type calendarFreeBusyRequest struct {
	TimeMin  string                        `json:"timeMin"`
	TimeMax  string                        `json:"timeMax"`
	TimeZone string                        `json:"timeZone,omitempty"`
	Items    []calendarFreeBusyRequestItem `json:"items"`
}

type calendarFreeBusyRequestItem struct {
	ID string `json:"id"`
}

type calendarFreeBusyResponse struct {
	Kind      string                              `json:"kind,omitempty"`
	TimeMin   string                              `json:"timeMin,omitempty"`
	TimeMax   string                              `json:"timeMax,omitempty"`
	Calendars map[string]calendarFreeBusyCalendar `json:"calendars"`
}

type calendarFreeBusyCalendar struct {
	Busy []calendarBusyPeriod `json:"busy"`
}

type calendarBusyPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type calendarEventsListResponse struct {
	Items         []calendarEventResponse `json:"items"`
	NextPageToken string                  `json:"nextPageToken,omitempty"`
	TimeZone      string                  `json:"timeZone,omitempty"`
}

func (c *CalendarCapability) Name() string { return "calendar" }
func (c *CalendarCapability) Description() string {
	return "Read, search, and manage Google Calendar events"
}
func (c *CalendarCapability) Tools() []Tool {
	return []Tool{
		{
			Name:        "calendar.status",
			Description: "Check whether the Google Calendar connection is ready and report the connected account",
			ParamSchema: `{}`,
		},
		{
			Name:        "calendar.create_event",
			Description: "Create a Google Calendar event on the primary calendar or a named calendar",
			ParamSchema: `{"type":"object","properties":{"calendar_id":{"type":"string"},"summary":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"timezone":{"type":"string"},"location":{"type":"string"},"description":{"type":"string"},"attendees":{"oneOf":[{"type":"array","items":{"type":"string"}},{"type":"string"}]},"all_day":{"type":"boolean"},"duration_minutes":{"type":"integer","minimum":1}},"required":["summary","start"]}`,
		},
		{
			Name:        "calendar.free_busy",
			Description: "Check busy windows for one or more calendars over a time range",
			ParamSchema: `{"type":"object","properties":{"calendar_id":{"type":"string"},"calendar_ids":{"oneOf":[{"type":"array","items":{"type":"string"}},{"type":"string"}]},"start":{"type":"string"},"end":{"type":"string"},"timezone":{"type":"string"}}}`,
		},
		{
			Name:        "calendar.find_events",
			Description: "Search calendar events by text and time window",
			ParamSchema: `{"type":"object","properties":{"calendar_id":{"type":"string"},"calendar_ids":{"oneOf":[{"type":"array","items":{"type":"string"}},{"type":"string"}]},"query":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"max_results":{"type":"integer","minimum":1}}}`,
		},
		{
			Name:        "calendar.update_event",
			Description: "Update an existing calendar event",
			ParamSchema: `{"type":"object","properties":{"calendar_id":{"type":"string"},"event_id":{"type":"string"},"query":{"type":"string"},"summary":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"timezone":{"type":"string"},"location":{"type":"string"},"description":{"type":"string"},"attendees":{"oneOf":[{"type":"array","items":{"type":"string"}},{"type":"string"}]},"all_day":{"type":"boolean"},"duration_minutes":{"type":"integer","minimum":1}}}`,
		},
		{
			Name:        "calendar.cancel_event",
			Description: "Cancel an existing calendar event",
			ParamSchema: `{"type":"object","properties":{"calendar_id":{"type":"string"},"event_id":{"type":"string"},"query":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"}}}`,
		},
	}
}
func (c *CalendarCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	switch toolName {
	case "calendar.status":
		return c.executeStatus()
	case "calendar.create_event":
		return c.executeCreateEvent(params)
	case "calendar.free_busy":
		return c.executeFreeBusy(params)
	case "calendar.find_events":
		return c.executeFindEvents(params)
	case "calendar.update_event":
		return c.executeUpdateEvent(params)
	case "calendar.cancel_event", "calendar.delete_event":
		return c.executeCancelEvent(params)
	default:
		return ToolResult{Success: false, Error: fmt.Sprintf("unknown calendar tool %q", toolName)}, fmt.Errorf("unknown calendar tool %q", toolName)
	}
}

func (c *CalendarCapability) authenticatedClient() (*GmailCapability, *http.Client, error) {
	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		return nil, nil, err
	}
	gmail := &GmailCapability{TokenPath: cfg.GmailTokenPath, CredPath: cfg.GmailCredPath}
	client := gmail.httpClient()
	if client == nil {
		return nil, nil, errors.New("calendar is not authenticated; run `jot assistant auth gmail` first")
	}
	return gmail, client, nil
}

func (c *CalendarCapability) executeStatus() (ToolResult, error) {
	gmail, probeClient, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	profile, profileErr := gmail.profile()
	if profileErr != nil {
		return ToolResult{
			Success: true,
			Data: map[string]any{
				"connected": false,
				"ready":     false,
				"email":     "",
				"reason":    profileErr.Error(),
			},
			Text: "calendar: not connected",
		}, nil
	}

	if probeClient == nil {
		return ToolResult{
			Success: true,
			Data: map[string]any{
				"connected": true,
				"ready":     false,
				"email":     profile.EmailAddress,
				"reason":    "gmail oauth token not available",
			},
			Text: fmt.Sprintf("calendar: connected (%s) but not ready", profile.EmailAddress),
		}, nil
	}

	req, err := http.NewRequest(http.MethodGet, calendarAPIURL("/users/me/calendarList?maxResults=1"), nil)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return ToolResult{
			Success: true,
			Data: map[string]any{
				"connected": true,
				"ready":     false,
				"email":     profile.EmailAddress,
				"reason":    err.Error(),
			},
			Text: fmt.Sprintf("calendar: connected (%s) but not ready", profile.EmailAddress),
		}, nil
	}
	var list calendarListResponse
	if err := gmailDecodeResponse(resp, &list); err != nil {
		return ToolResult{
			Success: true,
			Data: map[string]any{
				"connected": true,
				"ready":     false,
				"email":     profile.EmailAddress,
				"reason":    err.Error(),
			},
			Text: fmt.Sprintf("calendar: connected (%s) but not ready", profile.EmailAddress),
		}, nil
	}

	calendarName := "primary calendar"
	if len(list.Items) > 0 && strings.TrimSpace(list.Items[0].Summary) != "" {
		calendarName = list.Items[0].Summary
	}
	return ToolResult{
		Success: true,
		Data: map[string]any{
			"connected": true,
			"ready":     true,
			"email":     profile.EmailAddress,
			"calendar":  calendarName,
			"calendarId": func() string {
				if len(list.Items) > 0 && strings.TrimSpace(list.Items[0].ID) != "" {
					return list.Items[0].ID
				}
				return "primary"
			}(),
		},
		Text: fmt.Sprintf("calendar: ready (%s)", profile.EmailAddress),
	}, nil
}

func (c *CalendarCapability) executeCreateEvent(params map[string]any) (ToolResult, error) {
	_, client, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	summary := firstStringParam(params, "summary", "title", "subject")
	if summary == "" {
		return ToolResult{Success: false, Error: "summary is required"}, errors.New("summary is required")
	}

	calendarID := firstStringParam(params, "calendar_id", "calendar", "calendarId")
	if calendarID == "" {
		calendarID = "primary"
	}

	timezone := firstStringParam(params, "timezone", "time_zone", "timeZone")
	location := firstStringParam(params, "location", "where")
	description := firstStringParam(params, "description", "body", "notes")
	startRaw := firstStringParam(params, "start", "start_time", "when")
	endRaw := firstStringParam(params, "end", "end_time")
	if startRaw == "" {
		return ToolResult{Success: false, Error: "start is required"}, errors.New("start is required")
	}

	allDay := paramBool(params, "all_day", "allDay", "date_only")
	durationMinutes := paramInt(params, 30, "duration_minutes", "duration", "minutes")
	if durationMinutes <= 0 {
		durationMinutes = 30
	}

	eventRequest, startTime, endTime, allDayEvent, err := calendarBuildEventRequest(summary, description, location, timezone, calendarID, startRaw, endRaw, allDay, durationMinutes, params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	reqBody, err := json.Marshal(eventRequest)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	apiURL := calendarAPIURL("/calendars/" + url.PathEscape(calendarID) + "/events")
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	var created calendarEventResponse
	if err := gmailDecodeResponse(resp, &created); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if strings.TrimSpace(created.CalendarID) == "" {
		created.CalendarID = calendarID
	}
	if strings.TrimSpace(created.Summary) == "" {
		created.Summary = summary
	}
	if created.Start.DateTime == "" && created.Start.Date == "" && !startTime.IsZero() {
		created.Start.DateTime = startTime.Format(time.RFC3339)
	}
	if created.End.DateTime == "" && created.End.Date == "" && !endTime.IsZero() {
		created.End.DateTime = endTime.Format(time.RFC3339)
	}
	if allDayEvent {
		if created.Start.Date == "" && !startTime.IsZero() {
			created.Start.Date = startTime.Format("2006-01-02")
		}
		if created.End.Date == "" && !endTime.IsZero() {
			created.End.Date = endTime.Format("2006-01-02")
		}
	}

	summaryText := calendarEventSummaryText(created, allDayEvent)
	return ToolResult{
		Success: true,
		Data: map[string]any{
			"event":      created,
			"calendarId": created.CalendarID,
			"summary":    summary,
			"allDay":     allDayEvent,
		},
		Text: summaryText,
	}, nil
}

func (c *CalendarCapability) executeFreeBusy(params map[string]any) (ToolResult, error) {
	_, client, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	timezone := firstStringParam(params, "timezone", "time_zone", "timeZone")
	startRaw := firstStringParam(params, "start", "time_min", "from", "after")
	endRaw := firstStringParam(params, "end", "time_max", "to", "before")
	startTime, _, err := calendarParseTimeInputOrDefault(startRaw, timezone, time.Now())
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	endTime, _, err := calendarParseTimeInputOrDefault(endRaw, timezone, startTime.Add(7*24*time.Hour))
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if endTime.Before(startTime) {
		return ToolResult{Success: false, Error: "end must be after start"}, errors.New("end must be after start")
	}

	calendarIDs := calendarResolveCalendarIDs(params)
	reqBody := calendarFreeBusyRequest{
		TimeMin:  startTime.UTC().Format(time.RFC3339),
		TimeMax:  endTime.UTC().Format(time.RFC3339),
		TimeZone: timezone,
		Items:    make([]calendarFreeBusyRequestItem, 0, len(calendarIDs)),
	}
	for _, calendarID := range calendarIDs {
		reqBody.Items = append(reqBody.Items, calendarFreeBusyRequestItem{ID: calendarID})
	}
	var result calendarFreeBusyResponse
	if err := calendarDoJSON(client, http.MethodPost, calendarAPIURL("/freeBusy"), reqBody, &result); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	calendars := make(map[string]any, len(result.Calendars))
	totalBusy := 0
	for calendarID, cal := range result.Calendars {
		busy := make([]map[string]any, 0, len(cal.Busy))
		for _, slot := range cal.Busy {
			totalBusy++
			busy = append(busy, map[string]any{
				"start":        slot.Start,
				"end":          slot.End,
				"startDisplay": calendarDisplayTime(slot.Start),
				"endDisplay":   calendarDisplayTime(slot.End),
			})
		}
		calendars[calendarID] = map[string]any{
			"busy": busy,
		}
	}

	return ToolResult{
		Success: true,
		Data: map[string]any{
			"timeMin":     reqBody.TimeMin,
			"timeMax":     reqBody.TimeMax,
			"timezone":    timezone,
			"calendarIds": calendarIDs,
			"calendars":   calendars,
			"busyCount":   totalBusy,
		},
		Text: calendarFreeBusySummaryText(calendarIDs, result.Calendars, reqBody.TimeMin, reqBody.TimeMax),
	}, nil
}

func (c *CalendarCapability) executeFindEvents(params map[string]any) (ToolResult, error) {
	_, client, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	calendarIDs := calendarResolveCalendarIDs(params)
	query := firstStringParam(params, "query", "q", "search", "input")
	startRaw := firstStringParam(params, "start", "time_min", "from", "after")
	endRaw := firstStringParam(params, "end", "time_max", "to", "before")
	timezone := firstStringParam(params, "timezone", "time_zone", "timeZone")
	maxResults := paramInt(params, 10, "max_results", "max", "limit")
	if maxResults <= 0 {
		maxResults = 10
	}

	startTime, _, err := calendarParseTimeInputOrDefault(startRaw, timezone, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	endTime, _, err := calendarParseTimeInputOrDefault(endRaw, timezone, time.Now().Add(30*24*time.Hour))
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if endTime.Before(startTime) {
		return ToolResult{Success: false, Error: "end must be after start"}, errors.New("end must be after start")
	}

	events, err := c.calendarFindEventsAcrossCalendars(client, calendarIDs, query, startTime, endTime, maxResults)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	normalized := make([]calendarEventResponse, len(events))
	copy(normalized, events)
	sort.SliceStable(normalized, func(i, j int) bool {
		return calendarEventSortTime(normalized[i]).Before(calendarEventSortTime(normalized[j]))
	})

	dataEvents := make([]map[string]any, 0, len(normalized))
	for _, event := range normalized {
		dataEvents = append(dataEvents, map[string]any{
			"id":          event.ID,
			"calendarId":  event.CalendarID,
			"summary":     event.Summary,
			"description": event.Description,
			"location":    event.Location,
			"start":       event.Start,
			"end":         event.End,
			"attendees":   event.Attendees,
			"status":      event.Status,
			"htmlLink":    event.HTMLLink,
		})
	}

	return ToolResult{
		Success: true,
		Data: map[string]any{
			"calendarIds": calendarIDs,
			"query":       query,
			"timeMin":     startTime.UTC().Format(time.RFC3339),
			"timeMax":     endTime.UTC().Format(time.RFC3339),
			"maxResults":  maxResults,
			"events":      dataEvents,
			"count":       len(normalized),
		},
		Text: calendarEventListSummaryText(normalized, calendarIDs, query),
	}, nil
}

func (c *CalendarCapability) executeUpdateEvent(params map[string]any) (ToolResult, error) {
	_, client, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	resolvedCalendarID, existingEvent, err := c.calendarResolveEventForMutation(client, params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	requestBody, err := calendarBuildUpdatedEventRequest(existingEvent, params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	reqPath := "/calendars/" + url.PathEscape(resolvedCalendarID) + "/events/" + url.PathEscape(existingEvent.ID)
	var updated calendarEventResponse
	if err := calendarDoJSON(client, http.MethodPut, calendarAPIURL(reqPath), requestBody, &updated); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if strings.TrimSpace(updated.CalendarID) == "" {
		updated.CalendarID = resolvedCalendarID
	}
	if strings.TrimSpace(updated.ID) == "" {
		updated.ID = existingEvent.ID
	}

	data := map[string]any{
		"calendarId": resolvedCalendarID,
		"eventId":    updated.ID,
		"event":      updated,
	}
	return ToolResult{
		Success: true,
		Data:    data,
		Text:    calendarEventMutationSummaryText("updated", updated),
	}, nil
}

func (c *CalendarCapability) executeCancelEvent(params map[string]any) (ToolResult, error) {
	_, client, err := c.authenticatedClient()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	resolvedCalendarID, existingEvent, err := c.calendarResolveEventForMutation(client, params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	reqPath := "/calendars/" + url.PathEscape(resolvedCalendarID) + "/events/" + url.PathEscape(existingEvent.ID)
	if err := calendarDoNoContent(client, http.MethodDelete, calendarAPIURL(reqPath)); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	return ToolResult{
		Success: true,
		Data: map[string]any{
			"calendarId": resolvedCalendarID,
			"eventId":    existingEvent.ID,
			"event":      existingEvent,
			"cancelled":  true,
		},
		Text: calendarEventMutationSummaryText("cancelled", existingEvent),
	}, nil
}

func (c *CalendarCapability) calendarFindEventsAcrossCalendars(client *http.Client, calendarIDs []string, query string, startTime, endTime time.Time, maxResults int) ([]calendarEventResponse, error) {
	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}
	events := make([]calendarEventResponse, 0, maxResults)
	for _, calendarID := range calendarIDs {
		page, err := c.calendarFindEventsOnCalendar(client, calendarID, query, startTime, endTime, maxResults)
		if err != nil {
			return nil, err
		}
		events = append(events, page...)
	}
	return events, nil
}

func (c *CalendarCapability) calendarFindEventsOnCalendar(client *http.Client, calendarID, query string, startTime, endTime time.Time, maxResults int) ([]calendarEventResponse, error) {
	values := url.Values{}
	values.Set("singleEvents", "true")
	values.Set("showDeleted", "false")
	values.Set("maxResults", strconv.Itoa(maxResults))
	values.Set("timeMin", startTime.UTC().Format(time.RFC3339))
	values.Set("timeMax", endTime.UTC().Format(time.RFC3339))
	values.Set("orderBy", "startTime")
	if query != "" {
		values.Set("q", query)
	}
	var resp calendarEventsListResponse
	path := "/calendars/" + url.PathEscape(calendarID) + "/events?" + values.Encode()
	if err := calendarDoJSON(client, http.MethodGet, calendarAPIURL(path), nil, &resp); err != nil {
		return nil, err
	}
	events := make([]calendarEventResponse, 0, len(resp.Items))
	for _, event := range resp.Items {
		if strings.TrimSpace(event.CalendarID) == "" {
			event.CalendarID = calendarID
		}
		events = append(events, event)
	}
	return events, nil
}

func (c *CalendarCapability) calendarResolveEventForMutation(client *http.Client, params map[string]any) (string, calendarEventResponse, error) {
	calendarIDs := calendarResolveCalendarIDs(params)
	calendarID := ""
	if len(calendarIDs) > 0 {
		calendarID = calendarIDs[0]
	}
	if explicit := firstStringParam(params, "calendar_id", "calendar", "calendarId"); explicit != "" {
		calendarID = explicit
	}
	if calendarID == "" {
		calendarID = "primary"
	}

	if eventID := firstStringParam(params, "event_id", "eventId", "id"); eventID != "" {
		event, err := c.calendarGetEvent(client, calendarID, eventID)
		if err != nil {
			return "", calendarEventResponse{}, err
		}
		if strings.TrimSpace(event.CalendarID) == "" {
			event.CalendarID = calendarID
		}
		return calendarID, event, nil
	}

	query := firstStringParam(params, "query", "q", "search", "title", "summary", "subject")
	if query == "" {
		return "", calendarEventResponse{}, errors.New("event_id or query is required")
	}
	startRaw := firstStringParam(params, "start", "time_min", "from", "after")
	endRaw := firstStringParam(params, "end", "time_max", "to", "before")
	timezone := firstStringParam(params, "timezone", "time_zone", "timeZone")
	startTime, _, err := calendarParseTimeInputOrDefault(startRaw, timezone, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return "", calendarEventResponse{}, err
	}
	endTime, _, err := calendarParseTimeInputOrDefault(endRaw, timezone, time.Now().Add(365*24*time.Hour))
	if err != nil {
		return "", calendarEventResponse{}, err
	}
	if endTime.Before(startTime) {
		return "", calendarEventResponse{}, errors.New("end must be after start")
	}

	events, err := c.calendarFindEventsOnCalendar(client, calendarID, query, startTime, endTime, 10)
	if err != nil {
		return "", calendarEventResponse{}, err
	}
	chosen, ok := calendarChooseMatchingEvent(events, query)
	if !ok {
		if len(events) == 0 {
			return "", calendarEventResponse{}, fmt.Errorf("no calendar event matched %q", query)
		}
		return "", calendarEventResponse{}, fmt.Errorf("multiple events matched %q; provide event_id", query)
	}
	if strings.TrimSpace(chosen.CalendarID) == "" {
		chosen.CalendarID = calendarID
	}
	return chosen.CalendarID, chosen, nil
}

func (c *CalendarCapability) calendarGetEvent(client *http.Client, calendarID, eventID string) (calendarEventResponse, error) {
	var event calendarEventResponse
	path := "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID)
	if err := calendarDoJSON(client, http.MethodGet, calendarAPIURL(path), nil, &event); err != nil {
		return calendarEventResponse{}, err
	}
	return event, nil
}

func calendarDoJSON(client *http.Client, method, fullURL string, body any, dst any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, fullURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return gmailDecodeResponse(resp, dst)
}

func calendarDoNoContent(client *http.Client, method, fullURL string) error {
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var apiErr gmailAPIErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil {
			if apiErr.Error.Message != "" {
				return errors.New(apiErr.Error.Message)
			}
			if apiErr.Error.Error != "" {
				return errors.New(apiErr.Error.Error)
			}
		}
		return fmt.Errorf("calendar api returned %s", resp.Status)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func calendarParseTimeInputOrDefault(value, timezone string, def time.Time) (time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return def, false, nil
	}
	return calendarParseTimeInput(value, timezone)
}

func calendarEventIsAllDay(event calendarEventResponse) bool {
	return strings.TrimSpace(event.Start.Date) != "" || strings.TrimSpace(event.End.Date) != ""
}

func calendarEventSortTime(event calendarEventResponse) time.Time {
	start, _, _, ok := calendarEventRangeTimes(event)
	if ok {
		return start
	}
	return time.Time{}
}

func calendarEventRangeTimes(event calendarEventResponse) (time.Time, time.Time, bool, bool) {
	allDay := calendarEventIsAllDay(event)
	start, okStart := calendarParseEventDateTime(event.Start)
	end, okEnd := calendarParseEventDateTime(event.End)
	if !okStart && !okEnd {
		return time.Time{}, time.Time{}, allDay, false
	}
	return start, end, allDay, true
}

func calendarParseEventDateTime(value calendarEventDateTime) (time.Time, bool) {
	if strings.TrimSpace(value.DateTime) != "" {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(value.DateTime)); err == nil {
			return t, true
		}
	}
	if strings.TrimSpace(value.Date) != "" {
		if t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value.Date), time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func calendarEventExistingRawInputs(event calendarEventResponse) (string, string, string, bool) {
	allDay := calendarEventIsAllDay(event)
	startRaw := strings.TrimSpace(event.Start.DateTime)
	endRaw := strings.TrimSpace(event.End.DateTime)
	if allDay {
		if strings.TrimSpace(event.Start.Date) != "" {
			startRaw = strings.TrimSpace(event.Start.Date)
		}
		if strings.TrimSpace(event.End.Date) != "" {
			endRaw = strings.TrimSpace(event.End.Date)
		}
	}
	if startRaw == "" || endRaw == "" {
		start, end, detectedAllDay, ok := calendarEventRangeTimes(event)
		if ok {
			allDay = allDay || detectedAllDay
			if startRaw == "" {
				if allDay {
					startRaw = start.Format("2006-01-02")
				} else {
					startRaw = start.Format(time.RFC3339)
				}
			}
			if endRaw == "" {
				if allDay {
					if end.IsZero() && !start.IsZero() {
						end = start.AddDate(0, 0, 1)
					}
					endRaw = end.Format("2006-01-02")
				} else {
					if end.IsZero() && !start.IsZero() {
						end = start.Add(time.Duration(calendarEventDurationMinutes(event)) * time.Minute)
					}
					endRaw = end.Format(time.RFC3339)
				}
			}
		}
	}
	timezone := strings.TrimSpace(event.Start.TimeZone)
	if timezone == "" {
		timezone = strings.TrimSpace(event.End.TimeZone)
	}
	return startRaw, endRaw, timezone, allDay
}

func calendarEventDurationMinutes(event calendarEventResponse) int {
	start, end, _, ok := calendarEventRangeTimes(event)
	if !ok || start.IsZero() || end.IsZero() {
		return 30
	}
	dur := int(end.Sub(start).Minutes())
	if dur <= 0 {
		return 30
	}
	return dur
}

func calendarBuildUpdatedEventRequest(existing calendarEventResponse, params map[string]any) (map[string]any, error) {
	summary := firstStringParam(params, "summary", "title", "subject")
	if summary == "" {
		summary = strings.TrimSpace(existing.Summary)
	}
	if summary == "" {
		return nil, errors.New("summary is required")
	}

	description := firstStringParam(params, "description", "body", "notes")
	if description == "" {
		description = strings.TrimSpace(existing.Description)
	}
	location := firstStringParam(params, "location", "where")
	if location == "" {
		location = strings.TrimSpace(existing.Location)
	}
	timezone := firstStringParam(params, "timezone", "time_zone", "timeZone")

	startRaw, endRaw, existingTimezone, allDay := calendarEventExistingRawInputs(existing)
	if timezone == "" {
		timezone = existingTimezone
	}

	if provided := firstStringParam(params, "start", "start_time", "when"); provided != "" {
		startRaw = provided
	}
	if provided := firstStringParam(params, "end", "end_time"); provided != "" {
		endRaw = provided
	}
	if paramBool(params, "all_day", "allDay", "date_only") {
		allDay = true
	}

	durationMinutes := paramInt(params, calendarEventDurationMinutes(existing), "duration_minutes", "duration", "minutes")
	if durationMinutes <= 0 {
		durationMinutes = calendarEventDurationMinutes(existing)
	}
	if firstStringParam(params, "end", "end_time") == "" && firstStringParam(params, "start", "start_time", "when") != "" {
		endRaw = ""
	}

	request, _, _, _, err := calendarBuildEventRequest(summary, description, location, timezone, existing.CalendarID, startRaw, endRaw, allDay, durationMinutes, params)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func calendarChooseMatchingEvent(events []calendarEventResponse, query string) (calendarEventResponse, bool) {
	if len(events) == 0 {
		return calendarEventResponse{}, false
	}
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if normalizedQuery == "" {
		if len(events) == 1 {
			return events[0], true
		}
		return calendarEventResponse{}, false
	}
	var exact []calendarEventResponse
	var contains []calendarEventResponse
	for _, event := range events {
		summary := strings.ToLower(strings.TrimSpace(event.Summary))
		if summary == normalizedQuery {
			exact = append(exact, event)
			continue
		}
		if summary != "" && (strings.Contains(summary, normalizedQuery) || strings.Contains(normalizedQuery, summary)) {
			contains = append(contains, event)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], true
	case len(exact) > 1:
		return calendarEventResponse{}, false
	case len(contains) == 1:
		return contains[0], true
	case len(events) == 1:
		return events[0], true
	default:
		return calendarEventResponse{}, false
	}
}

func calendarEventDisplayText(event calendarEventResponse) string {
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		summary = "calendar event"
	}
	when := calendarEventTimeRangeText(event.Start, event.End, calendarEventIsAllDay(event))
	if when != "" {
		return summary + " — " + when
	}
	return summary
}

func calendarEventListSummaryText(events []calendarEventResponse, calendarIDs []string, query string) string {
	count := len(events)
	target := "calendar events"
	if len(calendarIDs) > 0 {
		if len(calendarIDs) == 1 {
			target = "calendar events on " + calendarIDs[0]
		} else {
			target = fmt.Sprintf("calendar events across %d calendars", len(calendarIDs))
		}
	}
	if query != "" {
		target += fmt.Sprintf(" for %q", query)
	}
	if count == 0 {
		return target + ": 0"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s: %d", target, count))
	limit := count
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%d. %s", i+1, calendarEventDisplayText(events[i])))
	}
	if count > limit {
		b.WriteString(fmt.Sprintf("\n+%d more", count-limit))
	}
	return b.String()
}

func calendarFreeBusySummaryText(calendarIDs []string, calendars map[string]calendarFreeBusyCalendar, timeMin, timeMax string) string {
	total := 0
	for _, cal := range calendars {
		total += len(cal.Busy)
	}
	window := strings.TrimSpace(calendarDisplayTime(timeMin))
	if window == "" {
		window = strings.TrimSpace(timeMin)
	}
	endWindow := strings.TrimSpace(calendarDisplayTime(timeMax))
	if endWindow == "" {
		endWindow = strings.TrimSpace(timeMax)
	}
	if len(calendarIDs) == 0 {
		if window != "" && endWindow != "" {
			return fmt.Sprintf("calendar free/busy: no busy windows between %s and %s", window, endWindow)
		}
		return "calendar free/busy: no busy windows"
	}
	if len(calendarIDs) == 1 {
		calendarID := calendarIDs[0]
		if window != "" && endWindow != "" {
			return fmt.Sprintf("calendar free/busy on %s: %d busy windows between %s and %s", calendarID, total, window, endWindow)
		}
		return fmt.Sprintf("calendar free/busy on %s: %d busy windows", calendarID, total)
	}
	if window != "" && endWindow != "" {
		return fmt.Sprintf("calendar free/busy: %d busy windows across %d calendars between %s and %s", total, len(calendarIDs), window, endWindow)
	}
	return fmt.Sprintf("calendar free/busy: %d busy windows across %d calendars", total, len(calendarIDs))
}

func calendarEventMutationSummaryText(action string, event calendarEventResponse) string {
	label := strings.TrimSpace(action)
	if label == "" {
		label = "updated"
	}
	return fmt.Sprintf("calendar event %s: %s", label, calendarEventDisplayText(event))
}

func calendarResolveCalendarIDs(params map[string]any) []string {
	ids := calendarStringListParam(params, "calendar_ids", "calendars", "calendarIds")
	if len(ids) > 0 {
		return ids
	}
	if id := firstStringParam(params, "calendar_id", "calendar", "calendarId"); id != "" {
		return []string{id}
	}
	return []string{"primary"}
}

func calendarAPIURL(path string) string {
	path = strings.TrimPrefix(path, "/")
	return calendarAPIBaseURL + "/" + path
}

func calendarBuildEventRequest(summary, description, location, timezone, calendarID, startRaw, endRaw string, allDay bool, durationMinutes int, params map[string]any) (map[string]any, time.Time, time.Time, bool, error) {
	startTime, startAllDay, err := calendarParseTimeInput(startRaw, timezone)
	if err != nil {
		return nil, time.Time{}, time.Time{}, false, err
	}
	endTime := time.Time{}
	endAllDay := false

	if endRaw != "" {
		endTime, endAllDay, err = calendarParseTimeInput(endRaw, timezone)
		if err != nil {
			return nil, time.Time{}, time.Time{}, false, err
		}
	}

	allDayEvent := allDay || startAllDay || endAllDay
	if allDayEvent {
		startDate := startTime
		endDate := endTime
		if startDate.IsZero() {
			return nil, time.Time{}, time.Time{}, false, errors.New("start date is required")
		}
		if endDate.IsZero() {
			endDate = startDate.AddDate(0, 0, 1)
		}
		event := map[string]any{
			"summary": summary,
			"start":   map[string]any{"date": startDate.Format("2006-01-02")},
			"end":     map[string]any{"date": endDate.Format("2006-01-02")},
		}
		if description != "" {
			event["description"] = description
		}
		if location != "" {
			event["location"] = location
		}
		if attendees := calendarEventAttendeesFromParams(params); len(attendees) > 0 {
			event["attendees"] = attendees
		}
		return event, startDate, endDate, true, nil
	}

	if endTime.IsZero() {
		endTime = startTime.Add(time.Duration(durationMinutes) * time.Minute)
	}
	if endTime.Before(startTime) {
		return nil, time.Time{}, time.Time{}, false, errors.New("end must be after start")
	}

	event := map[string]any{
		"summary": summary,
		"start":   map[string]any{"dateTime": startTime.Format(time.RFC3339)},
		"end":     map[string]any{"dateTime": endTime.Format(time.RFC3339)},
	}
	if timezone != "" {
		if tz := strings.TrimSpace(timezone); tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				event["start"].(map[string]any)["timeZone"] = loc.String()
				event["end"].(map[string]any)["timeZone"] = loc.String()
			} else {
				event["start"].(map[string]any)["timeZone"] = tz
				event["end"].(map[string]any)["timeZone"] = tz
			}
		}
	}
	if description != "" {
		event["description"] = description
	}
	if location != "" {
		event["location"] = location
	}
	if attendees := calendarEventAttendeesFromParams(params); len(attendees) > 0 {
		event["attendees"] = attendees
	}
	return event, startTime, endTime, false, nil
}

func calendarEventAttendeesFromParams(params map[string]any) []calendarEventAttendee {
	raw := calendarStringListParam(params, "attendees", "invitees", "to")
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	attendees := make([]calendarEventAttendee, 0, len(raw))
	for _, item := range raw {
		email := strings.TrimSpace(item)
		if email == "" {
			continue
		}
		if parsed, err := mail.ParseAddress(email); err == nil && strings.TrimSpace(parsed.Address) != "" {
			email = strings.TrimSpace(parsed.Address)
		}
		key := strings.ToLower(email)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		attendees = append(attendees, calendarEventAttendee{Email: email})
	}
	return attendees
}

func calendarStringListParam(params map[string]any, keys ...string) []string {
	for _, key := range keys {
		if params == nil {
			continue
		}
		value, ok := params[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return cloneAndTrimStrings(typed)
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if s := calendarStringFromAny(item); s != "" {
					out = append(out, s)
				}
			}
			return out
		case string:
			return splitAndTrimString(typed)
		default:
			if s := calendarStringFromAny(typed); s != "" {
				return splitAndTrimString(s)
			}
		}
	}
	return nil
}

func calendarStringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	s := strings.Trim(strings.TrimSpace(string(data)), "\"")
	return strings.TrimSpace(s)
}

func splitAndTrimString(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{value}
	}
	return out
}

func cloneAndTrimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func calendarParseTimeInput(value, timezone string) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false, errors.New("time value is required")
	}

	loc := time.Local
	if tz := strings.TrimSpace(timezone); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 3:04pm",
		"2006-01-02 3:04 PM",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			allDay := layout == "2006-01-02"
			return t, allDay, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("unable to parse time %q", value)
}

func calendarEventSummaryText(event calendarEventResponse, allDay bool) string {
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		summary = "calendar event"
	}
	when := calendarEventTimeRangeText(event.Start, event.End, allDay)
	calendarName := strings.TrimSpace(event.CalendarID)
	if calendarName == "" {
		calendarName = "primary"
	}
	if when != "" {
		return fmt.Sprintf("calendar event created: %s (%s) on %s", summary, when, calendarName)
	}
	return fmt.Sprintf("calendar event created: %s on %s", summary, calendarName)
}

func calendarEventTimeRangeText(start, end calendarEventDateTime, allDay bool) string {
	if allDay {
		if start.Date != "" && end.Date != "" {
			if start.Date == end.Date {
				return start.Date + " (all day)"
			}
			return start.Date + " to " + end.Date + " (all day)"
		}
		if start.Date != "" {
			return start.Date + " (all day)"
		}
	}
	if start.DateTime != "" && end.DateTime != "" {
		startTime := calendarDisplayTime(start.DateTime)
		endTime := calendarDisplayTime(end.DateTime)
		if startTime != "" && endTime != "" {
			return startTime + " to " + endTime
		}
	}
	if start.DateTime != "" {
		if t := calendarDisplayTime(start.DateTime); t != "" {
			return t
		}
	}
	if start.Date != "" {
		return start.Date + " (all day)"
	}
	return ""
}

func calendarDisplayTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}
	return parsed.Local().Format("Mon Jan 2 3:04pm")
}

func NewAssistantSession(provider ModelProvider, capabilities []Capability, config AssistantConfig) *AssistantSession {
	session := &AssistantSession{
		Provider:     provider,
		Capabilities: append([]Capability(nil), capabilities...),
		Config:       config,
		Verbose:      config.Verbose,
		Format:       normalizeAssistantFormat(config.DefaultFormat),
		NoConfirm:    !config.ConfirmByDefault,
	}
	if session.Format == "" {
		session.Format = "text"
	}
	return session
}

func normalizeAssistantFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return "text"
	case "json":
		return "json"
	default:
		return "text"
	}
}

func (s *AssistantSession) CloneHistory() []Message {
	if len(s.History) == 0 {
		return nil
	}
	history := s.History
	if len(history) > assistantHistoryMaxMessages {
		history = history[len(history)-assistantHistoryMaxMessages:]
	}
	out := make([]Message, len(history))
	for i, message := range history {
		out[i] = assistantHistoryMessage(message)
	}
	return out
}

func (s *AssistantSession) appendHistory(message Message) {
	s.History = append(s.History, assistantHistoryMessage(message))
	if len(s.History) > assistantHistoryMaxMessages {
		s.History = append([]Message(nil), s.History[len(s.History)-assistantHistoryMaxMessages:]...)
	}
}

func (s *AssistantSession) capabilityBindings() []assistantToolBinding {
	var bindings []assistantToolBinding
	for _, capability := range s.Capabilities {
		if capability == nil {
			continue
		}
		capName := strings.TrimSpace(capability.Name())
		for _, tool := range capability.Tools() {
			toolName := strings.TrimSpace(tool.Name)
			fullName := toolName
			if capName != "" && !strings.Contains(toolName, ".") {
				fullName = capName + "." + toolName
			}
			shortName := toolName
			if dot := strings.LastIndex(shortName, "."); dot >= 0 {
				shortName = shortName[dot+1:]
			}
			bindings = append(bindings, assistantToolBinding{
				Capability: capability,
				Tool:       tool,
				FullName:   fullName,
				ShortName:  shortName,
			})
		}
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].FullName == bindings[j].FullName {
			return bindings[i].Capability.Name() < bindings[j].Capability.Name()
		}
		return bindings[i].FullName < bindings[j].FullName
	})
	return bindings
}

func (s *AssistantSession) AllTools() []Tool {
	bindings := s.capabilityBindings()
	out := make([]Tool, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, Tool{
			Name:        binding.FullName,
			Description: binding.Tool.Description,
			ParamSchema: binding.Tool.ParamSchema,
		})
	}
	return out
}

func (s *AssistantSession) BuildSystemPrompt(now time.Time) string {
	var b strings.Builder
	b.WriteString("You are Jot Assistant, a terminal-native action agent.\n")
	b.WriteString("Use tools when they are necessary. Keep answers concise and operational.\n")
	b.WriteString("If a tool is needed, emit only the following format:\n")
	b.WriteString("TOOL: capability.tool_name\n")
	b.WriteString("PARAMS: {\"json\":\"object\"}\n")
	b.WriteString("Do not wrap tool calls in markdown fences. Do not mix extra commentary into tool-call output.\n")
	b.WriteString("When the task is complete, reply with plain text only.\n")
	b.WriteString("Keep the experience centered on four simple verbs: read, reply, schedule, clear.\n")
	b.WriteString("Use Gmail clear actions to finish inbox work after you summarize it: gmail.archive_thread to clear from inbox, gmail.mark_read to mark done, gmail.star_thread to keep something important in view.\n")
	b.WriteString("For reply work, prefer gmail.read_thread for context and gmail.draft_reply to prepare the reply before sending.\n")
	b.WriteString("If the user wants help filling a web form linked from an email, use gmail.fill_form after you have the relevant message_id or thread_id. The runtime will use the browser computer to inspect and fill the page.\n")
	b.WriteString("When an email has attachments and the user's task depends on their contents, use gmail.read_attachment to read them directly. Do not ask the user to download attachments just to inspect them.\n")
	b.WriteString("For scheduling, use calendar.free_busy before proposing meeting times, calendar.find_events to inspect existing calendar context, and calendar.update_event or calendar.cancel_event when the user wants to change or remove an existing event.\n")
	b.WriteString("The current time is ")
	b.WriteString(now.UTC().Format(time.RFC3339))
	b.WriteString(".\n")

	b.WriteString("Available tools:\n")
	for _, binding := range s.capabilityBindings() {
		b.WriteString("- ")
		b.WriteString(binding.FullName)
		if binding.Tool.Description != "" {
			b.WriteString(": ")
			b.WriteString(binding.Tool.Description)
		}
		b.WriteString("\n")
		if strings.TrimSpace(binding.Tool.ParamSchema) != "" {
			b.WriteString("  schema: ")
			b.WriteString(strings.TrimSpace(binding.Tool.ParamSchema))
			b.WriteString("\n")
		}
	}
	b.WriteString("Use the narrowest tool that solves the task. The runtime will ask for confirmation before any external-effect action that requires it.\n")
	return b.String()
}

func (s *AssistantSession) BuildMessages(userInput string, now time.Time) []Message {
	history := s.CloneHistory()
	msgs := make([]Message, 0, len(history)+2)
	msgs = append(msgs, Message{Role: "system", Content: s.BuildSystemPrompt(now)})
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: "user", Content: userInput})
	return msgs
}

func (s *AssistantSession) lookupTool(toolName string) (assistantToolBinding, bool) {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	if normalized == "" {
		return assistantToolBinding{}, false
	}
	for _, binding := range s.capabilityBindings() {
		if strings.EqualFold(binding.FullName, normalized) ||
			strings.EqualFold(binding.Tool.Name, normalized) ||
			strings.EqualFold(binding.ShortName, normalized) {
			return binding, true
		}
	}
	return assistantToolBinding{}, false
}

func (s *AssistantSession) ExecuteTool(toolName string, params map[string]any) (ToolResult, error) {
	binding, ok := s.lookupTool(toolName)
	if !ok {
		return ToolResult{Success: false, Error: fmt.Sprintf("unknown tool %q", toolName)}, fmt.Errorf("unknown tool %q", toolName)
	}
	callName := binding.Tool.Name
	if strings.Contains(binding.FullName, ".") && !strings.Contains(binding.Tool.Name, ".") {
		callName = binding.Tool.Name
	}
	if aware, ok := binding.Capability.(assistantProgressAware); ok {
		aware.SetProgressReporter(s.ProgressFn)
		defer aware.SetProgressReporter(nil)
	}
	return binding.Capability.Execute(callName, params)
}

func (s *AssistantSession) RunInteractive(ctx context.Context, in io.Reader, out io.Writer, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}
	_, _ = fmt.Fprintf(out, "\n  %s%s%s\n",
		"\x1b[38;2;196;168;130m", "jot assistant — connected to Gmail", "\x1b[0m")
	_, _ = fmt.Fprintf(out, "  %s%s%s\n\n",
		"\x1b[90m", "type a request, or 'exit' / 'quit' to leave.", "\x1b[0m")

	reader := bufio.NewReader(in)
	for {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := fmt.Fprintf(out, "\x1b[38;2;196;168;130m›\x1b[0m "); err != nil {
			return err
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimSpace(line)
		if isAssistantExitCommand(line) || (line == "" && errors.Is(err, io.EOF)) {
			return nil
		}
		if line == "" {
			continue
		}
		result, runErr := s.RunTurn(ctx, line, in, out, now)
		if runErr != nil {
			if errors.Is(runErr, ErrAssistantCancelled) || errors.Is(runErr, ErrAssistantEditRequested) {
				continue
			}
			return runErr
		}
		rendered, renderErr := RenderAssistantTurn(line, result, s.Provider, s.Format, now())
		if renderErr != nil {
			return renderErr
		}
		if strings.TrimSpace(rendered) != "" {
			if result != nil && result.StreamedFinal {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out, rendered); err != nil {
				return err
			}
		} else if result != nil && result.StreamedFinal {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out, ""); err != nil {
			return err
		}
	}
}

func (s *AssistantSession) RunTurn(ctx context.Context, userInput string, in io.Reader, out io.Writer, now func() time.Time) (*AssistantTurnResult, error) {
	return s.runTurnWithMaxRounds(ctx, userInput, in, out, now, defaultAssistantMaxRounds)
}

func (s *AssistantSession) runTurnWithMaxRounds(ctx context.Context, userInput string, in io.Reader, out io.Writer, now func() time.Time, maxRounds int) (*AssistantTurnResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if now == nil {
		now = time.Now
	}
	if s.Provider == nil {
		return nil, ErrAssistantNoProvider
	}
	if len(s.Capabilities) == 0 {
		return nil, ErrAssistantNoCapabilities
	}
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		return &AssistantTurnResult{Input: userInput, Warnings: []string{"empty input"}}, nil
	}
	if turn, handled, err := s.handlePendingUserInput(ctx, userInput, in, out); handled {
		return turn, err
	}

	history := s.CloneHistory()
	userMessage := Message{Role: "user", Content: userInput}
	history = append(history, userMessage)
	s.appendHistory(userMessage)

	turn := &AssistantTurnResult{Input: userInput}
	tools := s.AllTools()
	emittedStatus := map[string]struct{}{}
	var liveStatusMu sync.Mutex
	if shouldEmitAssistantLiveStatus(out, s.Format, s.Verbose) {
		s.ProgressFn = func(line string) {
			line = strings.TrimSpace(line)
			if line == "" {
				return
			}
			liveStatusMu.Lock()
			defer liveStatusMu.Unlock()
			if _, err := fmt.Fprintln(out, renderAssistantStatusLine(line)); err == nil {
				turn.LiveStatus = true
			}
		}
		defer func() { s.ProgressFn = nil }()
	} else {
		s.ProgressFn = nil
	}
	for round := 0; round < maxRounds; round++ {
		if ctx.Err() != nil {
			return turn, ctx.Err()
		}
		messages := append([]Message(nil), history...)
		messages = append([]Message{{Role: "system", Content: s.BuildSystemPrompt(now())}}, messages...)
		turn.Prompt = messages[0].Content

		allowStreaming := len(turn.Executions) == 0
		response, streamedFinal, err := s.chatWithOptionalStreaming(messages, tools, out, turn, &liveStatusMu, allowStreaming)
		if err != nil {
			return turn, err
		}
		turn.RawResponse = response
		parsed := ParseAssistantToolCalls(response)
		parsed.ToolCalls = normalizeAssistantToolCalls(userInput, parsed.ToolCalls)
		if len(parsed.ToolCalls) == 0 && strings.Contains(strings.ToUpper(response), "TOOL:") {
			if fallback := fallbackAssistantToolCalls(userInput); len(fallback) > 0 {
				parsed.ToolCalls = fallback
				turn.Warnings = append(turn.Warnings, "recovered malformed provider tool output with deterministic fallback")
			}
		}
		if len(parsed.Warnings) > 0 {
			turn.Warnings = append(turn.Warnings, parsed.Warnings...)
		}

		assistantMessage := Message{Role: "assistant", Content: response}
		history = append(history, assistantMessage)
		s.appendHistory(assistantMessage)

		if len(parsed.ToolCalls) == 0 {
			turn.FinalText = strings.TrimSpace(response)
			turn.StreamedFinal = streamedFinal && turn.FinalText != ""
			s.updatePendingFromTurn(userInput, turn)
			turn.History = append([]Message(nil), history...)
			return turn, nil
		}

		turn.ToolCalls = append(turn.ToolCalls, parsed.ToolCalls...)
		if shouldEmitAssistantLiveStatus(out, s.Format, s.Verbose) {
			for _, call := range parsed.ToolCalls {
				line := assistantStatusLineForToolCall(userInput, call)
				if line == "" {
					continue
				}
				if _, seen := emittedStatus[line]; seen {
					continue
				}
				if _, err := fmt.Fprintln(out, renderAssistantStatusLine(line)); err != nil {
					return turn, err
				}
				emittedStatus[line] = struct{}{}
				turn.LiveStatus = true
			}
		}
		for _, call := range parsed.ToolCalls {
			if ctx.Err() != nil {
				return turn, ctx.Err()
			}

			execution := AssistantToolExecution{Call: call}
			if strings.EqualFold(strings.TrimSpace(call.Tool), "gmail.fill_form") {
				if execution.Call.Params == nil {
					execution.Call.Params = map[string]any{}
				}
				if _, ok := execution.Call.Params["user_input"]; !ok {
					execution.Call.Params["user_input"] = userInput
				}
				call = execution.Call
			}
			confirm := assistantToolRequiresConfirmation(call)
			if s.NoConfirm && !isDeleteAssistantOperation(call.Tool) {
				confirm = false
			}
			if confirm {
				req := buildConfirmationRequest(call)
				confirmed, promptErr := PromptForConfirmation(in, out, req)
				if promptErr != nil {
					if errors.Is(promptErr, ErrAssistantEditRequested) || errors.Is(promptErr, ErrAssistantCancelled) {
						return turn, promptErr
					}
					return turn, promptErr
				}
				if !confirmed {
					return turn, ErrAssistantCancelled
				}
				execution.Confirmed = true
			}

			if s.Verbose {
				if _, err := fmt.Fprintln(out, renderVerboseToolCall(call)); err != nil {
					return turn, err
				}
			}

			result, execErr := s.executeToolWithRuntimeFlow(ctx, call, in, out)
			if execErr != nil {
				result = ToolResult{Success: false, Error: execErr.Error(), Text: execErr.Error()}
			}
			execution.Result = result
			turn.Executions = append(turn.Executions, execution)

			toolContent := toolResultMessageContent(result)
			history = append(history, Message{Role: "tool", Tool: call.Tool, Content: toolContent})
			s.appendHistory(Message{Role: "tool", Tool: call.Tool, Content: toolContent})

			if s.Verbose {
				if _, err := fmt.Fprintln(out, renderVerboseToolResult(result)); err != nil {
					return turn, err
				}
			}
			if assistantToolCompletesTurn(call, result) {
				turn.FinalText = strings.TrimSpace(result.Text)
				turn.History = append([]Message(nil), history...)
				s.updatePendingFromTurn(userInput, turn)
				return turn, nil
			}
		}
	}

	turn.History = append([]Message(nil), history...)
	s.Pending = nil
	return turn, fmt.Errorf("assistant exceeded %d tool rounds", maxRounds)
}

func assistantToolCompletesTurn(call AssistantToolCall, result ToolResult) bool {
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "gmail.fill_form":
		return result.Success
	default:
		return false
	}
}

func (s *AssistantSession) executeToolWithRuntimeFlow(ctx context.Context, call AssistantToolCall, in io.Reader, out io.Writer) (ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "gmail.fill_form":
		return executeAssistantFormFill(ctx, s, call, in, out)
	default:
		return s.ExecuteTool(call.Tool, call.Params)
	}
}

func (s *AssistantSession) handlePendingUserInput(ctx context.Context, userInput string, in io.Reader, out io.Writer) (*AssistantTurnResult, bool, error) {
	if s.Pending == nil {
		return nil, false, nil
	}
	switch s.Pending.Kind {
	case "gmail.download_attachment":
		return s.handlePendingAttachmentDownload(userInput, out)
	case "gmail.draft_reply":
		return s.handlePendingDraftReply(userInput, out)
	case "gmail.fill_form":
		return s.handlePendingFormFill(ctx, userInput, in, out)
	default:
		return nil, false, nil
	}
}

func (s *AssistantSession) handlePendingFormFill(ctx context.Context, userInput string, in io.Reader, out io.Writer) (*AssistantTurnResult, bool, error) {
	pending := s.Pending
	if pending == nil || pending.FormFill == nil {
		s.Pending = nil
		return nil, false, nil
	}
	input := strings.TrimSpace(userInput)
	if input == "" {
		return nil, false, nil
	}
	if assistantIsCancellationInput(strings.ToLower(input)) {
		s.Pending = nil
		return s.finishPendingTurn(userInput, "Form workflow cancelled.", nil, nil), true, nil
	}
	if !assistantLooksLikeFormFollowUpInput(input) {
		s.Pending = nil
		return nil, false, nil
	}

	call := AssistantToolCall{
		Tool: "gmail.fill_form",
		Params: map[string]any{
			"user_input": userInput,
		},
	}
	if strings.TrimSpace(pending.FormFill.MessageID) != "" {
		call.Params["message_id"] = pending.FormFill.MessageID
	}
	if strings.TrimSpace(pending.FormFill.ThreadID) != "" {
		call.Params["thread_id"] = pending.FormFill.ThreadID
	}
	if strings.TrimSpace(pending.FormFill.FormURL) != "" {
		call.Params["form_url"] = pending.FormFill.FormURL
	}

	result, err := executeAssistantFormFill(ctx, s, call, in, out)
	if err != nil {
		return s.finishPendingTurn(userInput, err.Error(), []AssistantToolExecution{{Call: call, Result: result}}, nil), true, nil
	}
	turn := s.finishPendingTurn(userInput, result.Text, []AssistantToolExecution{{Call: call, Result: result}}, nil)
	s.updatePendingFromTurn(userInput, turn)
	return turn, true, nil
}

func (s *AssistantSession) handlePendingAttachmentDownload(userInput string, out io.Writer) (*AssistantTurnResult, bool, error) {
	pending := s.Pending
	if pending == nil || pending.Attachment == nil {
		s.Pending = nil
		return nil, false, nil
	}
	input := strings.TrimSpace(userInput)
	lower := strings.ToLower(input)
	if lower == "" {
		return nil, false, nil
	}
	if assistantIsCancellationInput(lower) {
		s.Pending = nil
		return s.finishPendingTurn(userInput, "Attachment download cancelled.", nil, nil), true, nil
	}
	if !assistantLooksLikeAttachmentFollowUpInput(input, pending.Attachment.Items) {
		s.Pending = nil
		return nil, false, nil
	}

	saveDir, hasSaveDir := assistantExtractSaveDirInput(input)
	if hasSaveDir {
		pending.Attachment.SaveDir = saveDir
	}

	selected, selectedOK := assistantResolvePendingAttachments(input, pending.Attachment.Items)
	if !selectedOK && hasSaveDir && len(pending.Attachment.Items) == 1 {
		selected = append(selected, pending.Attachment.Items[0])
		selectedOK = true
	}
	if !selectedOK {
		return s.finishPendingTurn(userInput, assistantPendingAttachmentPrompt(pending.Attachment), nil, nil), true, nil
	}

	saveDir = strings.TrimSpace(pending.Attachment.SaveDir)
	if saveDir == "" {
		saveDir = strings.TrimSpace(s.Config.AttachmentSaveDir)
	}
	if saveDir == "" {
		saveDir = "."
	}

	grouped := assistantGroupPendingAttachmentSelections(selected, pending.Attachment.Items)
	var executions []AssistantToolExecution
	var files []gmailAttachmentDownloadFile
	var failures []string
	for _, group := range grouped {
		call := AssistantToolCall{
			Tool:   "gmail.download_attachment",
			Params: map[string]any{"message_id": group.MessageID, "save_dir": saveDir},
		}
		if group.DownloadAll {
			call.Params["download_all"] = true
		} else {
			call.Params["attachment_ids"] = append([]string(nil), group.AttachmentIDs...)
		}
		if s.Verbose && out != nil {
			if _, err := fmt.Fprintln(out, renderVerboseToolCall(call)); err != nil {
				return nil, true, err
			}
		}
		result, execErr := s.ExecuteTool(call.Tool, call.Params)
		if execErr != nil {
			result = ToolResult{Success: false, Error: execErr.Error(), Text: execErr.Error()}
		}
		executions = append(executions, AssistantToolExecution{Call: call, Result: result})
		if download, ok := result.Data.(gmailAttachmentDownloadResult); ok {
			if len(download.Files) > 0 {
				files = append(files, download.Files...)
			} else {
				files = append(files, gmailAttachmentDownloadFile{
					MessageID: download.MessageID,
					ThreadID:  download.ThreadID,
					Subject:   download.Subject,
					From:      download.From,
					Date:      download.Date,
					Filename:  download.Filename,
					SavedPath: download.SavedPath,
					Bytes:     download.Bytes,
				})
			}
		} else if strings.TrimSpace(result.Error) != "" {
			failures = append(failures, result.Error)
		}
		if s.Verbose && out != nil {
			if _, err := fmt.Fprintln(out, renderVerboseToolResult(result)); err != nil {
				return nil, true, err
			}
		}
	}

	s.Pending = nil
	if len(files) == 0 && len(failures) > 0 {
		return s.finishPendingTurn(userInput, failures[0], executions, nil), true, nil
	}
	finalText := gmailAttachmentDownloadSummary(files)
	return s.finishPendingTurn(userInput, finalText, executions, nil), true, nil
}

func (s *AssistantSession) handlePendingDraftReply(userInput string, out io.Writer) (*AssistantTurnResult, bool, error) {
	pending := s.Pending
	if pending == nil || pending.DraftReply == nil {
		s.Pending = nil
		return nil, false, nil
	}
	input := strings.TrimSpace(userInput)
	lower := strings.ToLower(input)
	if lower == "" {
		return nil, false, nil
	}
	if assistantIsCancellationInput(lower) {
		s.Pending = nil
		return s.finishPendingTurn(userInput, "Reply cancelled.", nil, nil), true, nil
	}

	sendIntent := assistantLooksAffirmative(lower) || strings.Contains(lower, "send")
	if !sendIntent {
		call := AssistantToolCall{
			Tool: "gmail.draft_reply",
			Params: map[string]any{
				"body": input,
			},
		}
		if pending.DraftReply.MessageID != "" {
			call.Params["message_id"] = pending.DraftReply.MessageID
		}
		if pending.DraftReply.ThreadID != "" {
			call.Params["thread_id"] = pending.DraftReply.ThreadID
		}
		if s.Verbose && out != nil {
			if _, err := fmt.Fprintln(out, renderVerboseToolCall(call)); err != nil {
				return nil, true, err
			}
		}
		result, execErr := s.ExecuteTool(call.Tool, call.Params)
		if execErr != nil {
			result = ToolResult{Success: false, Error: execErr.Error(), Text: execErr.Error()}
		}
		if s.Verbose && out != nil {
			if _, err := fmt.Fprintln(out, renderVerboseToolResult(result)); err != nil {
				return nil, true, err
			}
		}
		if data, ok := result.Data.(map[string]any); ok {
			pending.DraftReply.Body = firstStringParam(data, "body", "preview")
		} else {
			pending.DraftReply.Body = input
		}
		return s.finishPendingTurn(userInput, result.Text, []AssistantToolExecution{{Call: call, Result: result}}, nil), true, nil
	}

	if !pending.DraftReply.SendAllowed {
		return s.finishPendingTurn(userInput, "gmail send permission is not granted for this connection; run `jot assistant auth gmail` again to reconnect with send access", nil, nil), true, nil
	}

	call := AssistantToolCall{
		Tool: "gmail.draft_reply",
		Params: map[string]any{
			"body": pending.DraftReply.Body,
			"send": true,
		},
	}
	if pending.DraftReply.MessageID != "" {
		call.Params["message_id"] = pending.DraftReply.MessageID
	}
	if pending.DraftReply.ThreadID != "" {
		call.Params["thread_id"] = pending.DraftReply.ThreadID
	}
	if s.Verbose && out != nil {
		if _, err := fmt.Fprintln(out, renderVerboseToolCall(call)); err != nil {
			return nil, true, err
		}
	}
	result, execErr := s.ExecuteTool(call.Tool, call.Params)
	if execErr != nil {
		result = ToolResult{Success: false, Error: execErr.Error(), Text: execErr.Error()}
	}
	if s.Verbose && out != nil {
		if _, err := fmt.Fprintln(out, renderVerboseToolResult(result)); err != nil {
			return nil, true, err
		}
	}
	s.Pending = nil
	finalText := result.Text
	if strings.TrimSpace(finalText) == "" {
		finalText = "Reply sent."
	}
	return s.finishPendingTurn(userInput, finalText, []AssistantToolExecution{{Call: call, Result: result}}, nil), true, nil
}

func (s *AssistantSession) finishPendingTurn(userInput, finalText string, executions []AssistantToolExecution, warnings []string) *AssistantTurnResult {
	turn := &AssistantTurnResult{
		Input:      strings.TrimSpace(userInput),
		FinalText:  strings.TrimSpace(finalText),
		Executions: append([]AssistantToolExecution(nil), executions...),
		Warnings:   append([]string(nil), warnings...),
	}
	history := s.CloneHistory()
	userMessage := Message{Role: "user", Content: turn.Input}
	history = append(history, userMessage)
	s.appendHistory(userMessage)
	for _, execution := range executions {
		turn.ToolCalls = append(turn.ToolCalls, execution.Call)
		toolMessage := Message{Role: "tool", Tool: execution.Call.Tool, Content: toolResultMessageContent(execution.Result)}
		history = append(history, toolMessage)
		s.appendHistory(toolMessage)
	}
	if turn.FinalText != "" {
		assistantMessage := Message{Role: "assistant", Content: turn.FinalText}
		history = append(history, assistantMessage)
		s.appendHistory(assistantMessage)
	}
	turn.History = history
	return turn
}

func (s *AssistantSession) updatePendingFromTurn(userInput string, turn *AssistantTurnResult) {
	s.Pending = assistantPendingFromTurn(userInput, turn, s.Config)
}

func assistantPendingFromTurn(userInput string, turn *AssistantTurnResult, cfg AssistantConfig) *AssistantPendingAction {
	if turn == nil {
		return nil
	}
	if pending := assistantPendingFormFillFromTurn(turn); pending != nil {
		return &AssistantPendingAction{Kind: "gmail.fill_form", FormFill: pending}
	}
	if pending := assistantPendingDraftReplyFromTurn(turn); pending != nil {
		return &AssistantPendingAction{Kind: "gmail.draft_reply", DraftReply: pending}
	}
	if pending := assistantPendingAttachmentFromTurn(userInput, turn, cfg); pending != nil {
		return &AssistantPendingAction{Kind: "gmail.download_attachment", Attachment: pending}
	}
	return nil
}

func assistantPendingFormFillFromTurn(turn *AssistantTurnResult) *AssistantPendingFormFill {
	for i := len(turn.Executions) - 1; i >= 0; i-- {
		execution := turn.Executions[i]
		if !strings.EqualFold(strings.TrimSpace(execution.Call.Tool), "gmail.fill_form") || !execution.Result.Success {
			continue
		}
		result, ok := execution.Result.Data.(FormFillResult)
		if !ok || strings.TrimSpace(result.Link.URL) == "" {
			continue
		}
		if !assistantFormNeedsFollowUp(result) {
			return nil
		}
		return &AssistantPendingFormFill{
			MessageID: result.Link.MessageID,
			ThreadID:  firstStringParam(execution.Call.Params, "thread_id"),
			FormURL:   result.Link.URL,
			Title:     result.FormTitle,
		}
	}
	return nil
}

func assistantFormNeedsFollowUp(result FormFillResult) bool {
	for _, note := range result.Notes {
		lower := strings.ToLower(strings.TrimSpace(note))
		if strings.Contains(lower, "review") || strings.Contains(lower, "left blank") || strings.Contains(lower, "manual input") {
			return true
		}
	}
	for _, field := range result.Fields {
		if strings.TrimSpace(field.Answer) == "" {
			return true
		}
	}
	return false
}

func assistantLooksLikeFormFollowUpInput(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return false
	}
	for _, token := range []string{
		"form", "fill", "submit", "change", "update", "field", "comment", "comments",
		"question", "questions", "size", "colour", "color", "pink", "white", "black",
		"jumper", "shirt", "xs", "xl", "small", "medium", "large", "yes", "no", "rsvp",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func assistantPendingDraftReplyFromTurn(turn *AssistantTurnResult) *AssistantPendingDraftReply {
	for i := len(turn.Executions) - 1; i >= 0; i-- {
		execution := turn.Executions[i]
		if !strings.EqualFold(strings.TrimSpace(execution.Call.Tool), "gmail.draft_reply") {
			continue
		}
		data, ok := execution.Result.Data.(map[string]any)
		if !ok {
			continue
		}
		if assistantBoolValue(data["sent"]) {
			return nil
		}
		body := firstStringParam(data, "body", "preview")
		if body == "" {
			continue
		}
		return &AssistantPendingDraftReply{
			MessageID:   firstStringParam(data, "message_id", "id"),
			ThreadID:    firstStringParam(data, "thread_id"),
			Body:        body,
			To:          firstStringParam(data, "reply_to", "to"),
			Subject:     firstStringParam(data, "subject"),
			SendAllowed: assistantBoolValue(data["send_allowed"]),
		}
	}
	return nil
}

func assistantPendingAttachmentFromTurn(userInput string, turn *AssistantTurnResult, cfg AssistantConfig) *AssistantPendingAttachmentDownload {
	if !assistantLooksLikeAttachmentIntent(userInput, turn.FinalText) {
		return nil
	}
	if !assistantInvitesAttachmentFollowUp(turn.FinalText) {
		return nil
	}
	for i := len(turn.Executions) - 1; i >= 0; i-- {
		execution := turn.Executions[i]
		tool := strings.ToLower(strings.TrimSpace(execution.Call.Tool))
		if tool == "gmail.download_attachment" {
			return nil
		}
		if tool != "gmail.search" && tool != "gmail.list_attachments" {
			continue
		}
		emails, ok := execution.Result.Data.([]NormalizedEmail)
		if !ok {
			continue
		}
		items := assistantPendingAttachmentItems(emails)
		if len(items) == 0 {
			continue
		}
		saveDir, _ := assistantExtractSaveDirInput(userInput)
		if saveDir == "" {
			saveDir = strings.TrimSpace(cfg.AttachmentSaveDir)
		}
		return &AssistantPendingAttachmentDownload{
			Items:   items,
			SaveDir: saveDir,
		}
	}
	return nil
}

func assistantPendingAttachmentItems(emails []NormalizedEmail) []AssistantPendingAttachmentItem {
	items := make([]AssistantPendingAttachmentItem, 0)
	for _, email := range emails {
		for _, attachment := range email.Attachments {
			items = append(items, AssistantPendingAttachmentItem{
				MessageID:  email.ID,
				ThreadID:   email.ThreadID,
				Subject:    email.Subject,
				From:       email.From,
				Date:       email.Date,
				Attachment: attachment,
			})
		}
	}
	return items
}

func assistantLooksLikeAttachmentIntent(userInput, finalText string) bool {
	text := strings.ToLower(strings.TrimSpace(userInput + "\n" + finalText))
	return strings.Contains(text, "attachment") ||
		strings.Contains(text, "attachments") ||
		strings.Contains(text, "download") ||
		strings.Contains(text, "save")
}

func assistantInvitesAttachmentFollowUp(finalText string) bool {
	text := strings.ToLower(strings.TrimSpace(finalText))
	return strings.Contains(text, "reply `all`") ||
		strings.Contains(text, "reply \"all\"") ||
		strings.Contains(text, "download everything") ||
		strings.Contains(text, "download all") ||
		strings.Contains(text, "which attachments") ||
		strings.Contains(text, "save to")
}

func assistantLooksLikeAttachmentFollowUpInput(input string, items []AssistantPendingAttachmentItem) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return false
	}
	if assistantLooksAffirmative(lower) ||
		assistantIsCancellationInput(lower) ||
		strings.Contains(lower, "all") ||
		strings.Contains(lower, "both") ||
		strings.Contains(lower, "everything") ||
		strings.Contains(lower, "attachment") ||
		strings.Contains(lower, "download") ||
		strings.Contains(lower, "save to") {
		return true
	}
	if assistantExtractHasSelectionIndexes(lower, len(items)) {
		return true
	}
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Attachment.Filename))
		if name != "" && (strings.Contains(lower, name) || strings.Contains(name, lower)) {
			return true
		}
	}
	return false
}

func assistantResolvePendingAttachments(input string, items []AssistantPendingAttachmentItem) ([]AssistantPendingAttachmentItem, bool) {
	if len(items) == 0 {
		return nil, false
	}
	lower := strings.ToLower(strings.TrimSpace(input))
	if assistantLooksAffirmative(lower) || strings.Contains(lower, "all") || strings.Contains(lower, "both") || strings.Contains(lower, "everything") {
		return append([]AssistantPendingAttachmentItem(nil), items...), true
	}
	if indexes := assistantParseSelectionIndexes(lower, len(items)); len(indexes) > 0 {
		selected := make([]AssistantPendingAttachmentItem, 0, len(indexes))
		for _, idx := range indexes {
			selected = append(selected, items[idx])
		}
		return selected, true
	}
	var selected []AssistantPendingAttachmentItem
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Attachment.Filename))
		if name == "" {
			continue
		}
		if strings.Contains(lower, name) || strings.Contains(name, lower) {
			selected = append(selected, item)
		}
	}
	if len(selected) > 0 {
		return selected, true
	}
	return nil, false
}

func assistantParseSelectionIndexes(input string, max int) []int {
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	})
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(fields))
	var out []int
	for _, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || n <= 0 || n > max {
			continue
		}
		idx := n - 1
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	return out
}

func assistantExtractHasSelectionIndexes(input string, max int) bool {
	return len(assistantParseSelectionIndexes(input, max)) > 0
}

func assistantExtractSaveDirInput(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "current directory"), strings.Contains(lower, "here"):
		return ".", true
	case strings.Contains(lower, "save to "):
		dir := strings.TrimSpace(trimmed[strings.Index(lower, "save to ")+len("save to "):])
		dir = strings.Trim(dir, `"'`)
		if dir != "" {
			return dir, true
		}
	case strings.HasPrefix(trimmed, "./"), strings.HasPrefix(trimmed, ".\\"), strings.HasPrefix(trimmed, "~/"), strings.HasPrefix(trimmed, "/"), strings.HasPrefix(trimmed, `\`):
		return trimmed, true
	case len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/'):
		return trimmed, true
	}
	return "", false
}

func assistantPendingAttachmentPrompt(pending *AssistantPendingAttachmentDownload) string {
	if pending == nil || len(pending.Items) == 0 {
		return "No pending attachments to download."
	}
	saveDir := strings.TrimSpace(pending.SaveDir)
	if saveDir == "" {
		saveDir = "."
	}
	var names []string
	for i, item := range pending.Items {
		if i >= 4 {
			names = append(names, fmt.Sprintf("and %d more", len(pending.Items)-i))
			break
		}
		label := strings.TrimSpace(item.Attachment.Filename)
		if label == "" {
			label = item.Subject
		}
		names = append(names, label)
	}
	return fmt.Sprintf("I found %d attachment(s): %s. Reply `all` to download everything to %s, reply with attachment numbers like `1 2`, or say `save to ./dir`.", len(pending.Items), strings.Join(names, ", "), saveDir)
}

func assistantGroupPendingAttachmentSelections(selected []AssistantPendingAttachmentItem, universe []AssistantPendingAttachmentItem) []assistantPendingAttachmentGroup {
	if len(selected) == 0 {
		return nil
	}
	totalByMessage := make(map[string]int)
	for _, item := range universe {
		if strings.TrimSpace(item.MessageID) == "" {
			continue
		}
		totalByMessage[item.MessageID]++
	}

	type state struct {
		all bool
		ids []string
	}
	byMessage := make(map[string]*state)
	order := make([]string, 0, len(selected))
	for _, item := range selected {
		messageID := strings.TrimSpace(item.MessageID)
		if messageID == "" {
			continue
		}
		current, ok := byMessage[messageID]
		if !ok {
			current = &state{}
			byMessage[messageID] = current
			order = append(order, messageID)
		}
		if item.Attachment.AttachmentID != "" {
			current.ids = append(current.ids, item.Attachment.AttachmentID)
		}
		if totalByMessage[messageID] > 0 && len(current.ids) >= totalByMessage[messageID] {
			current.all = true
			current.ids = nil
		}
	}

	out := make([]assistantPendingAttachmentGroup, 0, len(order))
	for _, messageID := range order {
		current := byMessage[messageID]
		if current == nil {
			continue
		}
		group := assistantPendingAttachmentGroup{
			MessageID:   messageID,
			DownloadAll: current.all,
		}
		if !group.DownloadAll {
			group.AttachmentIDs = append(group.AttachmentIDs, current.ids...)
		}
		out = append(out, group)
	}
	return out
}

func assistantLooksAffirmative(input string) bool {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "y", "yes", "ok", "okay", "sure", "do it", "send", "download", "all":
		return true
	default:
		return false
	}
}

func assistantIsCancellationInput(input string) bool {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "n", "no", "cancel", "skip", "discard", "stop":
		return true
	default:
		return false
	}
}

func RunAssistantTurn(ctx context.Context, session *AssistantSession, userInput string, in io.Reader, out io.Writer, now func() time.Time) (*AssistantTurnResult, error) {
	if session == nil {
		return nil, errors.New("assistant session is nil")
	}
	return session.RunTurn(ctx, userInput, in, out, now)
}

func RunAssistantInteractive(ctx context.Context, session *AssistantSession, in io.Reader, out io.Writer, now func() time.Time) error {
	if session == nil {
		return errors.New("assistant session is nil")
	}
	return session.RunInteractive(ctx, in, out, now)
}

func RunAssistantCommand(ctx context.Context, session *AssistantSession, input string, in io.Reader, out io.Writer, now func() time.Time, opts AssistantCommandOptions) (*AssistantTurnResult, error) {
	if session == nil {
		return nil, errors.New("assistant session is nil")
	}
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = defaultAssistantMaxRounds
	}
	if opts.Interactive {
		if err := session.RunInteractive(ctx, in, out, now); err != nil {
			return nil, err
		}
		return &AssistantTurnResult{}, nil
	}
	return session.runTurnWithMaxRounds(ctx, input, in, out, now, opts.MaxRounds)
}

func PromptForConfirmation(in io.Reader, out io.Writer, req ConfirmationRequest) (bool, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	reader := bufio.NewReader(in)
	if _, err := io.WriteString(out, renderConfirmationPrompt(req)); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "", "n", "no", "cancel":
		return false, ErrAssistantCancelled
	case "e", "edit":
		return false, ErrAssistantEditRequested
	case "y", "yes", "confirm":
		return true, nil
	default:
		return false, ErrAssistantCancelled
	}
}

func renderConfirmationPrompt(req ConfirmationRequest) string {
	accent := "\x1b[38;2;196;168;130m"
	dim := "\x1b[90m"
	green := "\x1b[38;2;90;175;89m"
	reset := "\x1b[0m"
	rule := dim + "  " + strings.Repeat("─", 36) + reset

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(rule + "\n")
	b.WriteString("  " + accent + "action requires confirmation" + reset + "\n")
	b.WriteString(rule + "\n")
	if req.Description != "" {
		b.WriteString("  " + req.Description + "\n")
	}
	for _, detail := range req.Details {
		if strings.TrimSpace(detail) == "" {
			continue
		}
		b.WriteString("\n  " + dim + detail + reset + "\n")
	}
	b.WriteString("\n")
	// Styled buttons matching prototype
	yBtn := green + "[y]" + reset + dim + " confirm" + reset
	eBtn := accent + "[e]" + reset + dim + " edit" + reset
	nBtn := dim + "[n] cancel" + reset
	b.WriteString(fmt.Sprintf("  %s   %s   %s\n", yBtn, eBtn, nBtn))
	b.WriteString("  " + accent + "›" + reset + " ")
	return b.String()
}

func buildConfirmationRequest(call AssistantToolCall) ConfirmationRequest {
	req := ConfirmationRequest{
		ToolName:    call.Tool,
		Description: describeAssistantToolAction(call.Tool, call.Params),
		Params:      call.Params,
	}
	if req.Description == "" {
		req.Description = "confirm action for " + call.Tool
	}
	req.Details = assistantConfirmationDetails(call.Tool, call.Params)
	return req
}

func describeAssistantToolAction(toolName string, params map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch {
	case strings.Contains(name, "send_email"):
		if to := firstStringParam(params, "to", "recipient", "email"); to != "" {
			subject := firstStringParam(params, "subject")
			if subject != "" {
				return fmt.Sprintf("send reply to %s?\n  subject: %s", to, subject)
			}
			return fmt.Sprintf("send email to %s?", to)
		}
		return "send email?"
	case strings.Contains(name, "draft_reply"):
		if to := firstStringParam(params, "to", "recipient", "email"); to != "" {
			subject := firstStringParam(params, "subject")
			if subject != "" {
				return fmt.Sprintf("send reply to %s?\n  subject: %s", to, subject)
			}
			return fmt.Sprintf("send reply to %s?", to)
		}
		return "send reply?"
	case strings.Contains(name, "delete"):
		if id := firstStringParam(params, "id", "message_id", "thread_id"); id != "" {
			return fmt.Sprintf("delete %s?", id)
		}
		return "delete item?"
	case strings.Contains(name, "archive"):
		if id := firstStringParam(params, "id", "message_id", "thread_id"); id != "" {
			return fmt.Sprintf("archive %s?", id)
		}
		return "archive item?"
	case strings.Contains(name, "mark_read"):
		if id := firstStringParam(params, "id", "message_id", "thread_id"); id != "" {
			return fmt.Sprintf("mark %s as read?", id)
		}
		return "mark thread as read?"
	case strings.Contains(name, "star_thread"):
		if id := firstStringParam(params, "id", "message_id", "thread_id"); id != "" {
			return fmt.Sprintf("star %s?", id)
		}
		return "star thread?"
	case strings.Contains(name, "modify"):
		return "modify labels?"
	case strings.Contains(name, "download_attachment"):
		if file := firstStringParam(params, "filename", "name"); file != "" {
			return fmt.Sprintf("download attachment %s?", file)
		}
		return "download attachment?"
	case strings.Contains(name, "create_event"):
		title := firstStringParam(params, "summary", "title", "subject")
		when := firstStringParam(params, "start", "start_time", "when")
		if title != "" && when != "" {
			return fmt.Sprintf("create calendar event %q at %s?", title, when)
		}
		if title != "" {
			return fmt.Sprintf("create calendar event %q?", title)
		}
		if when != "" {
			return fmt.Sprintf("create calendar event at %s?", when)
		}
		return "create calendar event?"
	case strings.Contains(name, "update_event"):
		title := firstStringParam(params, "summary", "title", "subject")
		when := firstStringParam(params, "start", "start_time", "when")
		if title != "" && when != "" {
			return fmt.Sprintf("update calendar event to %q at %s?", title, when)
		}
		if title != "" {
			return fmt.Sprintf("update calendar event to %q?", title)
		}
		if when != "" {
			return fmt.Sprintf("update calendar event at %s?", when)
		}
		return "update calendar event?"
	case strings.Contains(name, "cancel_event"):
		if id := firstStringParam(params, "event_id", "id", "message_id", "thread_id"); id != "" {
			return fmt.Sprintf("cancel calendar event %s?", id)
		}
		if title := firstStringParam(params, "summary", "title", "subject"); title != "" {
			return fmt.Sprintf("cancel calendar event %q?", title)
		}
		return "cancel calendar event?"
	default:
		if toolName != "" {
			return "run " + toolName + "?"
		}
		return ""
	}
}

func assistantConfirmationDetails(toolName string, params map[string]any) []string {
	var details []string
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch {
	case strings.Contains(name, "send_email"), strings.Contains(name, "draft_reply"):
		if to := firstStringParam(params, "to", "recipient", "email"); to != "" {
			details = append(details, "to: "+to)
		}
		if subject := firstStringParam(params, "subject"); subject != "" {
			details = append(details, "subject: "+subject)
		}
		if body := firstStringParam(params, "body", "content"); body != "" {
			details = append(details, "body: "+truncateForPrompt(body, 240))
		}
	case strings.Contains(name, "delete"), strings.Contains(name, "archive"), strings.Contains(name, "mark_read"), strings.Contains(name, "star_thread"):
		if id := firstStringParam(params, "id", "message_id", "thread_id"); id != "" {
			details = append(details, "target: "+id)
		}
	case strings.Contains(name, "download_attachment"):
		if file := firstStringParam(params, "filename", "name"); file != "" {
			details = append(details, "file: "+file)
		}
		if dir := firstStringParam(params, "save_dir", "dir", "path"); dir != "" {
			details = append(details, "save to: "+dir)
		}
	case strings.Contains(name, "create_event"):
		if title := firstStringParam(params, "summary", "title", "subject"); title != "" {
			details = append(details, "title: "+title)
		}
		if when := firstStringParam(params, "start", "start_time", "when"); when != "" {
			details = append(details, "when: "+when)
		}
		if end := firstStringParam(params, "end", "end_time"); end != "" {
			details = append(details, "end: "+end)
		}
		if tz := firstStringParam(params, "timezone", "time_zone", "timeZone"); tz != "" {
			details = append(details, "timezone: "+tz)
		}
		if loc := firstStringParam(params, "location", "where"); loc != "" {
			details = append(details, "location: "+loc)
		}
		if cal := firstStringParam(params, "calendar_id", "calendar", "calendarId"); cal != "" {
			details = append(details, "calendar: "+cal)
		}
		if attendees := calendarStringListParam(params, "attendees", "invitees", "to"); len(attendees) > 0 {
			details = append(details, "attendees: "+strings.Join(attendees, ", "))
		}
	case strings.Contains(name, "update_event"):
		if id := firstStringParam(params, "event_id", "id"); id != "" {
			details = append(details, "event_id: "+id)
		}
		if title := firstStringParam(params, "summary", "title", "subject"); title != "" {
			details = append(details, "title: "+title)
		}
		if when := firstStringParam(params, "start", "start_time", "when"); when != "" {
			details = append(details, "start: "+when)
		}
		if end := firstStringParam(params, "end", "end_time"); end != "" {
			details = append(details, "end: "+end)
		}
		if tz := firstStringParam(params, "timezone", "time_zone", "timeZone"); tz != "" {
			details = append(details, "timezone: "+tz)
		}
		if cal := firstStringParam(params, "calendar_id", "calendar", "calendarId"); cal != "" {
			details = append(details, "calendar: "+cal)
		}
	case strings.Contains(name, "cancel_event"):
		if id := firstStringParam(params, "event_id", "id"); id != "" {
			details = append(details, "event_id: "+id)
		}
		if title := firstStringParam(params, "summary", "title", "subject"); title != "" {
			details = append(details, "title: "+title)
		}
		if when := firstStringParam(params, "start", "start_time", "when"); when != "" {
			details = append(details, "start: "+when)
		}
		if cal := firstStringParam(params, "calendar_id", "calendar", "calendarId"); cal != "" {
			details = append(details, "calendar: "+cal)
		}
	}
	if len(details) == 0 && len(params) > 0 {
		details = append(details, "params: "+compactJSONString(params))
	}
	return details
}

func shouldConfirmAssistantTool(toolName string) bool {
	return assistantToolRequiresConfirmation(AssistantToolCall{Tool: toolName})
}

func assistantToolRequiresConfirmation(call AssistantToolCall) bool {
	name := strings.ToLower(strings.TrimSpace(call.Tool))
	switch {
	case name == "":
		return false
	case strings.Contains(name, "search"),
		strings.Contains(name, "read_message"),
		strings.Contains(name, "read_thread"),
		strings.Contains(name, "read_attachment"),
		strings.Contains(name, "list_attachments"),
		strings.Contains(name, "download_attachment"),
		strings.Contains(name, "extract_actions"),
		strings.Contains(name, "status"):
		return false
	case strings.Contains(name, "draft_reply"):
		return paramBool(call.Params, "send", "deliver")
	case strings.Contains(name, "delete"),
		strings.Contains(name, "send"),
		strings.Contains(name, "archive"),
		strings.Contains(name, "mark_read"),
		strings.Contains(name, "star_thread"),
		strings.Contains(name, "modify"),
		strings.Contains(name, "update_event"),
		strings.Contains(name, "cancel_event"),
		strings.Contains(name, "create_event"):
		return true
	default:
		return false
	}
}

func isDeleteAssistantOperation(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	return strings.Contains(name, "delete")
}

func isAssistantExitCommand(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "exit", "quit", ":q", "q":
		return true
	default:
		return false
	}
}

func ParseAssistantToolCalls(text string) ParsedAssistantOutput {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	var cleaned []string
	var calls []AssistantToolCall
	var warnings []string

	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "TOOL:") {
			cleaned = append(cleaned, lines[i])
			i++
			continue
		}

		toolName := strings.TrimSpace(line[len("TOOL:"):])
		inlineParams := ""
		if idx := strings.Index(strings.ToUpper(toolName), "PARAMS:"); idx >= 0 {
			inlineParams = strings.TrimSpace(toolName[idx+len("PARAMS:"):])
			toolName = strings.TrimSpace(toolName[:idx])
		}
		if toolName == "" {
			warnings = append(warnings, "tool directive missing tool name")
			cleaned = append(cleaned, lines[i])
			i++
			continue
		}

		if inlineParams != "" {
			params, usedEnd, err := decodeToolParams(lines, i, inlineParams)
			if err != nil {
				warnings = append(warnings, err.Error())
				cleaned = append(cleaned, lines[i])
				i++
				continue
			}
			calls = append(calls, AssistantToolCall{
				Tool:      toolName,
				Params:    params,
				RawParams: strings.TrimSpace(joinLines(lines[i : usedEnd+1])),
			})
			i = usedEnd + 1
			continue
		}

		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) {
			warnings = append(warnings, "tool directive missing params block")
			cleaned = append(cleaned, lines[i])
			i++
			continue
		}

		paramsLine := strings.TrimSpace(lines[j])
		if !strings.HasPrefix(strings.ToUpper(paramsLine), "PARAMS:") {
			warnings = append(warnings, "tool directive missing PARAMS line")
			cleaned = append(cleaned, lines[i])
			i++
			continue
		}

		rawParams := strings.TrimSpace(paramsLine[len("PARAMS:"):])
		consumedEnd := j
		params, usedEnd, err := decodeToolParams(lines, j, rawParams)
		if err != nil {
			warnings = append(warnings, err.Error())
			cleaned = append(cleaned, lines[i:j+1]...)
			i = j + 1
			continue
		}
		consumedEnd = usedEnd
		calls = append(calls, AssistantToolCall{
			Tool:      toolName,
			Params:    params,
			RawParams: strings.TrimSpace(joinLines(lines[j : consumedEnd+1])),
		})
		i = consumedEnd + 1
	}

	return ParsedAssistantOutput{
		Text:      strings.TrimSpace(joinLines(cleaned)),
		ToolCalls: calls,
		Warnings:  warnings,
	}
}

type ParsedAssistantOutput struct {
	Text      string
	ToolCalls []AssistantToolCall
	Warnings  []string
}

func decodeToolParams(lines []string, paramLineIndex int, initial string) (map[string]any, int, error) {
	raw := strings.TrimSpace(initial)
	if raw == "" {
		var chunks []string
		for k := paramLineIndex + 1; k < len(lines); k++ {
			next := strings.TrimSpace(lines[k])
			if next == "" && len(chunks) == 0 {
				continue
			}
			if strings.HasPrefix(strings.ToUpper(next), "TOOL:") {
				break
			}
			chunks = append(chunks, lines[k])
			candidate := stripCodeFence(strings.TrimSpace(joinLines(chunks)))
			if params, err := parseJSONObject(candidate); err == nil {
				return params, k, nil
			}
		}
		return nil, paramLineIndex, errors.New("tool directive missing valid JSON params")
	}

	candidate := stripCodeFence(raw)
	if params, err := parseJSONObject(candidate); err == nil {
		return params, paramLineIndex, nil
	}

	var chunks []string
	chunks = append(chunks, raw)
	for k := paramLineIndex + 1; k < len(lines); k++ {
		next := strings.TrimSpace(lines[k])
		if strings.HasPrefix(strings.ToUpper(next), "TOOL:") {
			break
		}
		chunks = append(chunks, lines[k])
		candidate := stripCodeFence(strings.TrimSpace(joinLines(chunks)))
		if params, err := parseJSONObject(candidate); err == nil {
			return params, k, nil
		}
	}
	return nil, paramLineIndex, errors.New("tool directive missing valid JSON params")
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) >= 1 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "```" {
			lines = lines[:len(lines)-1]
		}
	}
	return strings.TrimSpace(joinLines(lines))
}

func parseJSONObject(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty params block")
	}
	if recovered, ok := recoverMalformedJSONObject(raw); ok {
		raw = recovered
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("params block contains trailing data")
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("tool params must be a JSON object")
	}
	if unwrapped, ok := unwrapJSONObject(obj); ok {
		obj = unwrapped
	}
	return obj, nil
}

func recoverMalformedJSONObject(raw string) (string, bool) {
	const prefix = `{"json":"`
	const suffix = `"}`
	if strings.HasPrefix(raw, prefix) && strings.HasSuffix(raw, suffix) {
		inner := strings.TrimSpace(raw[len(prefix) : len(raw)-len(suffix)])
		if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
			return inner, true
		}
	}
	return "", false
}

func unwrapJSONObject(obj map[string]any) (map[string]any, bool) {
	value, ok := obj["json"]
	if !ok {
		return nil, false
	}
	switch inner := value.(type) {
	case map[string]any:
		return inner, true
	case string:
		inner = strings.TrimSpace(inner)
		if inner == "" || inner == "{}" {
			cleaned := make(map[string]any, len(obj))
			for key, val := range obj {
				if key == "json" {
					continue
				}
				cleaned[key] = val
			}
			return cleaned, true
		}
		if parsed, err := parseJSONObject(inner); err == nil {
			return parsed, true
		}
	}
	return nil, false
}

func normalizeAssistantToolCalls(userInput string, calls []AssistantToolCall) []AssistantToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]AssistantToolCall, 0, len(calls))
	for _, call := range calls {
		call.Params = normalizeAssistantToolParams(call.Params)
		call = normalizeAssistantToolCall(userInput, call)
		out = append(out, call)
	}
	return out
}

func normalizeAssistantToolParams(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	if unwrapped, ok := unwrapJSONObject(params); ok {
		params = unwrapped
	}
	cleaned := make(map[string]any, len(params))
	for key, value := range params {
		if key == "json" {
			continue
		}
		cleaned[key] = value
	}
	return cleaned
}

func normalizeAssistantToolCall(userInput string, call AssistantToolCall) AssistantToolCall {
	switch strings.ToLower(strings.TrimSpace(call.Tool)) {
	case "gmail.search":
		query := firstStringParam(call.Params, "query", "input")
		if query == "" {
			if fallback := mapNLToGmailQuery(userInput); fallback != "" {
				call.Params["query"] = fallback
			}
		}
		if _, ok := call.Params["max"]; !ok {
			call.Params["max"] = 10
		}
	case "gmail.read_message":
		id := strings.ToLower(strings.TrimSpace(firstStringParam(call.Params, "id", "message_id")))
		if id == "" || id == "none specified" || id == "none" || id == "unknown" {
			if fallback := mapNLToGmailQuery(userInput); fallback != "" {
				call.Tool = "gmail.search"
				call.Params = map[string]any{"query": fallback, "max": 10}
			}
		}
	}
	return call
}

func fallbackAssistantToolCalls(userInput string) []AssistantToolCall {
	if query := mapNLToGmailQuery(userInput); query != "" {
		return []AssistantToolCall{{
			Tool:   "gmail.search",
			Params: map[string]any{"query": query, "max": 10},
		}}
	}
	return nil
}

func FormatAssistantTurnResult(result *AssistantTurnResult, format string) (string, error) {
	if result == nil {
		return "", nil
	}
	switch normalizeAssistantFormat(format) {
	case "json":
		payload := map[string]any{
			"input":      result.Input,
			"prompt":     result.Prompt,
			"response":   result.RawResponse,
			"finalText":  result.FinalText,
			"toolCalls":  result.ToolCalls,
			"executions": result.Executions,
			"warnings":   result.Warnings,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		var b strings.Builder
		if strings.TrimSpace(result.FinalText) != "" {
			b.WriteString(strings.TrimSpace(result.FinalText))
			return b.String(), nil
		}
		if len(result.Executions) > 0 {
			for _, execution := range result.Executions {
				if execution.Result.Text != "" {
					b.WriteString("- ")
					b.WriteString(execution.Result.Text)
					b.WriteString("\n")
					continue
				}
				if execution.Result.Error != "" {
					b.WriteString("- error: ")
					b.WriteString(execution.Result.Error)
					b.WriteString("\n")
					continue
				}
				if execution.Result.Data != nil {
					b.WriteString("- ")
					b.WriteString(compactJSONString(execution.Result.Data))
					b.WriteString("\n")
				}
			}
		}
		for _, warning := range result.Warnings {
			b.WriteString("warning: ")
			b.WriteString(warning)
			b.WriteString("\n")
		}
		return strings.TrimSpace(b.String()), nil
	}
}

func renderVerboseToolCall(call AssistantToolCall) string {
	return "[tool] " + call.Tool + " " + compactJSONString(call.Params)
}

func shouldEmitAssistantLiveStatus(out io.Writer, format string, verbose bool) bool {
	if out == nil || verbose {
		return false
	}
	return normalizeAssistantFormat(format) == "text"
}

func shouldStreamAssistantResponse(out io.Writer, format string, verbose bool) bool {
	if out == nil || verbose {
		return false
	}
	return normalizeAssistantFormat(format) == "text"
}

func (s *AssistantSession) chatWithOptionalStreaming(messages []Message, tools []Tool, out io.Writer, turn *AssistantTurnResult, liveStatusMu *sync.Mutex, allowStreaming bool) (string, bool, error) {
	streamingProvider, ok := s.Provider.(StreamingModelProvider)
	if !allowStreaming || !ok || !shouldStreamAssistantResponse(out, s.Format, s.Verbose) {
		response, err := s.Provider.Chat(messages, tools)
		return response, false, err
	}

	streamer := &assistantLiveResponseStreamer{
		out:                   out,
		turn:                  turn,
		liveStatusMu:          liveStatusMu,
		now:                   time.Now,
		plainFlushInterval:    70 * time.Millisecond,
		markdownFlushInterval: 120 * time.Millisecond,
		maxBufferedBytes:      768,
	}
	response, err := streamingProvider.ChatStream(messages, tools, streamer.OnDelta)
	if err != nil {
		return "", false, err
	}
	if err := streamer.Finish(); err != nil {
		return "", false, err
	}
	return response, streamer.Emitted(), nil
}

type assistantLiveResponseStreamer struct {
	out                   io.Writer
	turn                  *AssistantTurnResult
	liveStatusMu          *sync.Mutex
	now                   func() time.Time
	plainFlushInterval    time.Duration
	markdownFlushInterval time.Duration
	maxBufferedBytes      int
	lastFlushAt           time.Time
	full                  strings.Builder
	pending               strings.Builder
	decided               bool
	toolCall              bool
	emitted               bool
}

func (s *assistantLiveResponseStreamer) OnDelta(chunk string) error {
	if chunk == "" {
		return nil
	}
	s.full.WriteString(chunk)
	s.pending.WriteString(chunk)
	if !s.decided {
		switch assistantStreamingDecision(s.pending.String()) {
		case assistantStreamDecisionUndecided:
			return nil
		case assistantStreamDecisionToolCall:
			s.decided = true
			s.toolCall = true
			return nil
		case assistantStreamDecisionText:
			s.decided = true
			return s.flushPending(false)
		}
	}
	if s.toolCall {
		return nil
	}
	return s.flushPending(false)
}

func (s *assistantLiveResponseStreamer) Finish() error {
	if !s.decided {
		switch assistantStreamingDecisionFinal(s.pending.String()) {
		case assistantStreamDecisionToolCall:
			s.decided = true
			s.toolCall = true
			return nil
		case assistantStreamDecisionText:
			s.decided = true
			return s.flushPending(true)
		default:
			return nil
		}
	}
	if s.toolCall {
		return nil
	}
	return s.flushPending(true)
}

func (s *assistantLiveResponseStreamer) Emitted() bool {
	return s.emitted
}

func (s *assistantLiveResponseStreamer) flushPending(final bool) error {
	for s.pending.Len() > 0 {
		if s.out == nil {
			s.pending.Reset()
			return nil
		}

		pendingText := s.pending.String()
		if s.shouldDelayFlush(final, pendingText) {
			return nil
		}
		rendered, consumed := renderAssistantStreamingMarkdownChunk(pendingText, final)
		if consumed <= 0 {
			return nil
		}
		remainder := pendingText[consumed:]
		s.pending.Reset()
		if remainder != "" {
			s.pending.WriteString(remainder)
		}

		if rendered == "" {
			if !final {
				return nil
			}
			continue
		}
		if s.liveStatusMu != nil {
			s.liveStatusMu.Lock()
			_, err := io.WriteString(s.out, rendered)
			s.liveStatusMu.Unlock()
			if err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(s.out, rendered); err != nil {
				return err
			}
		}
		s.emitted = true
		if now := s.nowTime(); !now.IsZero() {
			s.lastFlushAt = now
		}
		if s.turn != nil {
			s.turn.LiveStatus = true
		}
		if !final {
			return nil
		}
	}
	return nil
}

func (s *assistantLiveResponseStreamer) shouldDelayFlush(final bool, pendingText string) bool {
	if final || !s.emitted {
		return false
	}
	if s.maxBufferedBytes > 0 && len(pendingText) >= s.maxBufferedBytes {
		return false
	}
	if s.lastFlushAt.IsZero() {
		return false
	}
	if assistantStreamingShouldFlushImmediately(pendingText) {
		return false
	}
	interval := s.flushIntervalForPendingText(pendingText)
	if interval <= 0 {
		return false
	}
	return s.nowTime().Sub(s.lastFlushAt) < interval
}

func (s *assistantLiveResponseStreamer) nowTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *assistantLiveResponseStreamer) flushIntervalForPendingText(pendingText string) time.Duration {
	if assistantStreamingLooksMarkdownHeavy(pendingText) {
		return s.markdownFlushInterval
	}
	return s.plainFlushInterval
}

func assistantStreamingShouldFlushImmediately(text string) bool {
	if text == "" {
		return false
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if strings.HasPrefix(normalized, "\n") {
		return true
	}
	if strings.Contains(normalized, "\n\n") {
		return true
	}
	lines := strings.Split(normalized, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return assistantMarkdownHeadingLevel(line) > 0
	}
	return false
}

func assistantStreamingLooksMarkdownHeavy(text string) bool {
	if text == "" {
		return false
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if strings.ContainsAny(normalized, "#*`_|") {
		return true
	}
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if assistantMarkdownHeadingLevel(line) > 0 {
			return true
		}
		if assistantMarkdownBulletContent(line) != "" {
			return true
		}
		if _, content := assistantMarkdownOrderedContent(line); content != "" {
			return true
		}
		if strings.HasPrefix(line, "|") && strings.Count(line, "|") >= 2 {
			return true
		}
	}
	return false
}

type assistantStreamDecision int

const (
	assistantStreamDecisionUndecided assistantStreamDecision = iota
	assistantStreamDecisionToolCall
	assistantStreamDecisionText
)

func assistantStreamingDecision(text string) assistantStreamDecision {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if trimmed == "" {
		return assistantStreamDecisionUndecided
	}
	upper := strings.ToUpper(trimmed)
	if assistantStreamingLooksLikeToolOutput(upper) {
		return assistantStreamDecisionToolCall
	}
	if assistantStreamingLooksLikePlainFinalText(trimmed) {
		return assistantStreamDecisionText
	}
	if len(trimmed) < 120 {
		return assistantStreamDecisionUndecided
	}
	return assistantStreamDecisionText
}

func assistantStreamingDecisionFinal(text string) assistantStreamDecision {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if trimmed == "" {
		return assistantStreamDecisionUndecided
	}
	upper := strings.ToUpper(trimmed)
	if assistantStreamingLooksLikeToolOutput(upper) {
		return assistantStreamDecisionToolCall
	}
	return assistantStreamDecisionText
}

func assistantStreamingLooksLikeToolOutput(upper string) bool {
	switch {
	case strings.HasPrefix(upper, "TOOL:"),
		strings.Contains(upper, "\nTOOL:"),
		strings.Contains(upper, "PARAMS:"),
		strings.Contains(upper, "<FUNCTION_CALLS>"),
		strings.Contains(upper, "<INVOKE NAME="):
		return true
	default:
		return false
	}
}

func assistantStreamingLooksLikePlainFinalText(text string) bool {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		return false
	}
	if len(trimmed) < 120 && strings.Contains(normalized, "\n\n") {
		return false
	}
	if len(trimmed) < 120 && !strings.Contains(normalized, "\n") {
		return false
	}
	if strings.Contains(normalized, "\n") {
		lines := strings.Split(normalized, "\n")
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" {
			return true
		}
	}
	switch {
	case strings.HasSuffix(trimmed, "."),
		strings.HasSuffix(trimmed, "!"),
		strings.HasSuffix(trimmed, "?"),
		strings.HasSuffix(trimmed, ":"):
		return true
	default:
		return len(trimmed) >= 220
	}
}

func renderVerboseToolResult(result ToolResult) string {
	if result.Error != "" {
		return "[done] error: " + result.Error
	}
	if result.Text != "" {
		return "[done] " + result.Text
	}
	if result.Data != nil {
		return "[done] " + compactJSONString(result.Data)
	}
	return "[done]"
}

func toolResultMessageContent(result ToolResult) string {
	payload := map[string]any{
		"success": result.Success,
		"text":    truncateForPrompt(result.Text, 280),
		"error":   truncateForPrompt(result.Error, 280),
		"data":    assistantSummarizeToolData(result.Data),
	}
	return truncateForPrompt(compactJSONString(payload), assistantHistoryToolMaxChars)
}

func compactJSONString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func assistantHistoryMessage(message Message) Message {
	limit := assistantHistoryMessageMaxChars
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
		limit = assistantHistoryToolMaxChars
	}
	message.Content = truncateForPrompt(message.Content, limit)
	return message
}

func assistantSummarizeToolData(data any) any {
	switch value := data.(type) {
	case nil:
		return nil
	case []NormalizedEmail:
		return assistantSummarizeEmailsForHistory(value)
	case NormalizedEmail:
		return assistantSummarizeEmailForHistory(value)
	case gmailThreadResult:
		return map[string]any{
			"threadId":        value.ThreadID,
			"subject":         truncateForPrompt(value.Subject, 160),
			"participants":    previewStrings(value.Participants, assistantHistoryListPreviewLimit, 80),
			"messageCount":    value.MessageCount,
			"attachmentCount": value.AttachmentCount,
			"messages":        assistantSummarizeEmailsForHistory(value.Messages),
		}
	case gmailAttachmentContentResult:
		return assistantSummarizeAttachmentReadForHistory(value)
	case []gmailAttachmentContentResult:
		out := make([]any, 0, assistantMinInt(len(value), assistantHistoryListPreviewLimit))
		for i, item := range value {
			if i >= assistantHistoryListPreviewLimit {
				break
			}
			out = append(out, assistantSummarizeAttachmentReadForHistory(item))
		}
		return map[string]any{"count": len(value), "items": out}
	case gmailAttachmentDownloadResult:
		return assistantSummarizeAttachmentDownloadForHistory(value)
	case []AttachmentMeta:
		out := make([]any, 0, assistantMinInt(len(value), assistantHistoryListPreviewLimit))
		for i, item := range value {
			if i >= assistantHistoryListPreviewLimit {
				break
			}
			out = append(out, map[string]any{
				"filename": item.Filename,
				"mimeType": item.MimeType,
				"size":     item.SizeBytes,
			})
		}
		return map[string]any{"count": len(value), "attachments": out}
	case AttachmentContent:
		return map[string]any{
			"textPreview": truncateForPrompt(value.Text, 240),
			"tableRows":   len(value.Tables),
			"warnings":    previewStrings(value.Warnings, assistantHistoryListPreviewLimit, 120),
		}
	case ExtractedActions:
		return map[string]any{
			"summary":     truncateForPrompt(value.Summary, 180),
			"actionItems": previewStrings(value.ActionItems, assistantHistoryListPreviewLimit, 120),
			"deadlines":   len(value.Deadlines),
			"meetingReqs": len(value.MeetingReqs),
			"entities":    len(value.Entities),
		}
	case map[string]any:
		return assistantSummarizeMapForHistory(value)
	case []map[string]any:
		out := make([]any, 0, assistantMinInt(len(value), assistantHistoryListPreviewLimit))
		for i, item := range value {
			if i >= assistantHistoryListPreviewLimit {
				break
			}
			out = append(out, assistantSummarizeMapForHistory(item))
		}
		return map[string]any{"count": len(value), "items": out}
	default:
		text := compactJSONString(value)
		return truncateForPrompt(text, 480)
	}
}

func assistantSummarizeEmailsForHistory(emails []NormalizedEmail) map[string]any {
	items := make([]any, 0, assistantMinInt(len(emails), assistantHistoryListPreviewLimit))
	for i, email := range emails {
		if i >= assistantHistoryListPreviewLimit {
			break
		}
		items = append(items, assistantSummarizeEmailForHistory(email))
	}
	return map[string]any{
		"count":  len(emails),
		"emails": items,
	}
}

func assistantSummarizeEmailForHistory(email NormalizedEmail) map[string]any {
	return map[string]any{
		"id":          email.ID,
		"threadId":    email.ThreadID,
		"from":        truncateForPrompt(email.From, 120),
		"subject":     truncateForPrompt(email.Subject, 160),
		"snippet":     truncateForPrompt(email.Snippet, 180),
		"date":        email.Date.Format(time.RFC3339),
		"unread":      email.Unread,
		"attachments": len(email.Attachments),
		"links":       len(email.Links),
	}
}

func assistantSummarizeAttachmentReadForHistory(result gmailAttachmentContentResult) map[string]any {
	return map[string]any{
		"messageId":   result.MessageID,
		"threadId":    result.ThreadID,
		"subject":     truncateForPrompt(result.Subject, 160),
		"from":        truncateForPrompt(result.From, 120),
		"filename":    result.Attachment.Filename,
		"mimeType":    result.Attachment.MimeType,
		"readable":    result.Readable,
		"error":       truncateForPrompt(result.Error, 180),
		"preview":     truncateForPrompt(result.Preview, 220),
		"textPreview": truncateForPrompt(result.Content.Text, 220),
		"tableRows":   len(result.Content.Tables),
	}
}

func assistantSummarizeAttachmentDownloadForHistory(result gmailAttachmentDownloadResult) map[string]any {
	if len(result.Files) > 0 {
		files := make([]any, 0, assistantMinInt(len(result.Files), assistantHistoryListPreviewLimit))
		for i, file := range result.Files {
			if i >= assistantHistoryListPreviewLimit {
				break
			}
			files = append(files, map[string]any{
				"filename": file.Filename,
				"bytes":    file.Bytes,
				"path":     truncateForPrompt(file.SavedPath, 140),
			})
		}
		return map[string]any{
			"count": result.Count,
			"files": files,
		}
	}
	return map[string]any{
		"filename": result.Filename,
		"bytes":    result.Bytes,
		"path":     truncateForPrompt(result.SavedPath, 140),
	}
}

func assistantSummarizeMapForHistory(m map[string]any) map[string]any {
	if len(m) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any)
	for _, key := range keys {
		if len(out) >= assistantHistoryListPreviewLimit {
			break
		}
		switch value := m[key].(type) {
		case string:
			out[key] = truncateForPrompt(value, 180)
		case []string:
			out[key] = previewStrings(value, assistantHistoryListPreviewLimit, 120)
		case []any:
			out[key] = fmt.Sprintf("%d items", len(value))
		case map[string]any:
			out[key] = fmt.Sprintf("%d keys", len(value))
		default:
			out[key] = value
		}
	}
	if len(keys) > assistantHistoryListPreviewLimit {
		out["_truncatedKeys"] = len(keys) - assistantHistoryListPreviewLimit
	}
	return out
}

func previewStrings(items []string, maxItems, maxChars int) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, assistantMinInt(len(items), maxItems))
	for i, item := range items {
		if i >= maxItems {
			break
		}
		out = append(out, truncateForPrompt(item, maxChars))
	}
	return out
}

func assistantMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func firstStringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if params == nil {
			continue
		}
		if value, ok := params[key]; ok {
			if s, ok := value.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
			if marshaled, err := json.Marshal(value); err == nil {
				s := strings.Trim(strings.TrimSpace(string(marshaled)), "\"")
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func truncateForPrompt(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
