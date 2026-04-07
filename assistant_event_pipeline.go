package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	processEventMaxAge       = 24 * time.Hour
	processEventDedupWindow  = 2 * time.Minute
	processEventFeedMinimum  = 40
	processEventDefaultScope = "local-process"
)

var (
	processEventNoiseProcesses = []string{
		"system idle process",
		"system",
		"svchost",
		"explorer",
		"services",
		"searchui",
		"conhost",
	}
	processEventNoiseTypes   = []string{"heartbeat", "tick", "poll", "status", "health", "alive"}
	processEventCrashSignals = []string{"crash", "fail", "hang", "panic", "exception", "oom", "terminate"}
)

// LocalProcessEvent represents a normalized event emitted by a local process or service.
type LocalProcessEvent struct {
	ID             string
	Timestamp      time.Time
	ProcessName    string
	PID            int
	Level          string
	Type           string
	Message        string
	Detail         string
	Source         string
	CommandLine    string
	Duration       time.Duration
	DurationMillis int
	CPUPercent     float64
	MemoryMB       float64
	Tags           []string
	Metadata       map[string]string
}

// ProcessEventPipelineResult contains the artifacts emitted by the pipeline.
type ProcessEventPipelineResult struct {
	Observations []MemoryObservation
	Inferences   []MemoryInference
	Feed         []AssistantFeedItem
}

// LoadProcessEvents reads newline-delimited JSON events from path.
func LoadProcessEvents(path string) ([]LocalProcessEvent, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	events := make([]LocalProcessEvent, 0, 32)
	for scanner.Scan() {
		rawLine := strings.TrimSpace(scanner.Text())
		if rawLine == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(rawLine), &raw); err != nil {
			continue
		}
		event := localProcessEventFromRaw(raw)
		if event.Timestamp.IsZero() {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events, nil
}

// BuildProcessEventSignals filters events, infers signals, and produces memory/feed items.
func BuildProcessEventSignals(events []LocalProcessEvent, now time.Time) ProcessEventPipelineResult {
	now = normalizeMemoryTime(now)
	filtered := filterProcessEvents(events, now)
	if len(filtered) == 0 {
		return ProcessEventPipelineResult{}
	}
	result := ProcessEventPipelineResult{
		Observations: make([]MemoryObservation, 0, len(filtered)),
		Inferences:   make([]MemoryInference, 0, len(filtered)),
		Feed:         make([]AssistantFeedItem, 0, len(filtered)),
	}
	for _, event := range filtered {
		meta := buildProcessEventMeta(event)
		observation := processEventObservation(event, meta)
		inference := processEventInference(event, meta)
		if observation.ID != "" {
			result.Observations = append(result.Observations, observation)
		}
		if inference.ID != "" {
			result.Inferences = append(result.Inferences, inference)
		}
		if feed, ok := processEventFeedItem(event, meta, now, observation.ID, inference.ID); ok {
			result.Feed = append(result.Feed, feed)
		}
	}
	return result
}

func filterProcessEvents(events []LocalProcessEvent, now time.Time) []LocalProcessEvent {
	out := make([]LocalProcessEvent, 0, len(events))
	seen := map[string]time.Time{}
	threshold := now.Add(-processEventMaxAge)
	for _, event := range events {
		if event.Timestamp.Before(threshold) || event.Timestamp.After(now.Add(5*time.Minute)) {
			continue
		}
		if isProcessEventNoise(event) {
			continue
		}
		key := processEventFingerprint(event)
		if last, ok := seen[key]; ok && event.Timestamp.Sub(last) < processEventDedupWindow {
			continue
		}
		seen[key] = event.Timestamp
		out = append(out, event)
	}
	return out
}

type processEventMeta struct {
	Title      string
	Summary    string
	Eyebrow    string
	Importance int
	Confidence MemoryConfidence
	Bucket     MemoryBucket
	Severity   int
}

func buildProcessEventMeta(event LocalProcessEvent) processEventMeta {
	severity := severityValue(event.Level)
	importance := clampInt(15+severity*20, 15, 100)
	if strings.Contains(strings.ToLower(event.Type), "crash") {
		importance = clampInt(importance+20, 1, 100)
	}
	if event.Duration >= time.Minute {
		importance = clampInt(importance+10, 1, 100)
	}
	meta := processEventMeta{
		Title:      eventTitle(event),
		Summary:    assistantDefaultString(event.Message, event.Detail),
		Eyebrow:    "Local signal",
		Importance: importance,
		Confidence: eventConfidence(severity, event),
		Bucket:     eventBucket(severity),
		Severity:   severity,
	}
	return meta
}

func processEventObservation(event LocalProcessEvent, meta processEventMeta) MemoryObservation {
	if !shouldObserveEvent(meta, event) {
		return MemoryObservation{}
	}
	fingerprint := processEventFingerprint(event)
	value := assistantDefaultString(event.Message, event.Detail)
	observation := MemoryObservation{
		ID:                   "process-observation:" + fingerprint,
		Scope:                processEventDefaultScope,
		Subject:              event.ProcessName,
		Key:                  eventKey(event),
		Summary:              assistantDefaultString(meta.Title, event.ProcessName),
		Value:                value,
		Evidence:             value,
		SourceType:           processEventDefaultScope,
		SourceID:             firstNonEmpty(event.Source, event.CommandLine, fingerprint),
		ObservedAt:           event.Timestamp,
		EffectiveAt:          event.Timestamp,
		EffectiveStart:       event.Timestamp,
		EffectiveEnd:         event.Timestamp.Add(event.Duration),
		Confidence:           meta.Confidence,
		Verification:         MemoryVerificationInferred,
		Kind:                 MemoryKindSituation,
		Bucket:               meta.Bucket,
		Importance:           meta.Importance,
		RetrievalText:        value,
		InferenceReason:      "derived from local process telemetry",
		EvidenceRefs:         event.Tags,
		SourceObservationIDs: []string{fingerprint},
	}
	return observation
}

func processEventInference(event LocalProcessEvent, meta processEventMeta) MemoryInference {
	if !shouldInferEvent(meta, event) {
		return MemoryInference{}
	}
	fingerprint := processEventFingerprint(event)
	value := assistantDefaultString(event.Message, event.Detail)
	return MemoryInference{
		ID:              "process-inference:" + fingerprint,
		Kind:            MemoryKindSituation,
		Bucket:          MemoryBucketActive,
		Scope:           processEventDefaultScope,
		Subject:         event.ProcessName,
		Key:             eventKey(event),
		Summary:         assistantDefaultString("Observed instability", meta.Title),
		Value:           value,
		Evidence:        value,
		EvidenceRefs:    event.Tags,
		SourceType:      processEventDefaultScope,
		SourceID:        firstNonEmpty(event.Source, fingerprint),
		ObservedAt:      event.Timestamp,
		EffectiveStart:  event.Timestamp,
		EffectiveEnd:    event.Timestamp.Add(event.Duration),
		Confidence:      meta.Confidence,
		Verification:    MemoryVerificationInferred,
		Importance:      clampInt(meta.Importance+10, 1, 100),
		InferenceReason: "aggregated from telemetry noise filter",
		RetrievalText:   value,
	}
}

func processEventFeedItem(event LocalProcessEvent, meta processEventMeta, now time.Time, obsID, infID string) (AssistantFeedItem, bool) {
	if meta.Importance < processEventFeedMinimum && !hasCrashSignal(event) {
		return AssistantFeedItem{}, false
	}
	fingerprint := processEventFingerprint(event)
	body := assistantDefaultString(event.Detail, event.CommandLine)
	item := AssistantFeedItem{
		ID:         "process-feed:" + fingerprint,
		Key:        "process-feed:" + fingerprint,
		Kind:       AssistantFeedKindFollowUpNeeded,
		Status:     AssistantFeedStatusNew,
		Eyebrow:    meta.Eyebrow,
		Title:      assistantDefaultString(meta.Title, "Local process event"),
		Summary:    assistantDefaultString(meta.Summary, event.ProcessName),
		Body:       assistantDefaultString(body, event.Message),
		Reason:     "caught by local telemetry pipeline",
		SourceType: processEventDefaultScope,
		SourceID:   firstNonEmpty(event.Source, event.CommandLine, fingerprint),
		Confidence: meta.Confidence,
		Importance: meta.Importance,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if event.Duration > 0 {
		item.DueAt = now.Add(event.Duration)
	}
	if obsID != "" {
		item.MemoryRefs = append(item.MemoryRefs, obsID)
	}
	if infID != "" {
		item.MemoryRefs = append(item.MemoryRefs, infID)
	}
	if event.CommandLine != "" {
		item.Links = append(item.Links, AssistantFeedLink{
			Label:   "command line",
			Preview: event.CommandLine,
		})
	}
	return item, true
}

func shouldObserveEvent(meta processEventMeta, event LocalProcessEvent) bool {
	return meta.Severity >= severityValue("warn") || hasCrashSignal(event) || event.Duration >= time.Minute
}

func shouldInferEvent(meta processEventMeta, event LocalProcessEvent) bool {
	return meta.Severity >= severityValue("error") || hasCrashSignal(event)
}

func processEventFingerprint(event LocalProcessEvent) string {
	builder := strings.Builder{}
	builder.WriteString(strings.ToLower(strings.TrimSpace(event.ProcessName)))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(event.Type)))
	builder.WriteString("|")
	builder.WriteString(strings.ToLower(strings.TrimSpace(event.Level)))
	builder.WriteString("|")
	builder.WriteString(strings.TrimSpace(event.Message))
	if event.ID != "" {
		builder.WriteString("|")
		builder.WriteString(event.ID)
	}
	hash := sha1.Sum([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

func eventTitle(event LocalProcessEvent) string {
	if event.ProcessName == "" {
		return "Local process event"
	}
	parts := []string{event.ProcessName}
	if label := humanizeEventType(event.Type); label != "" {
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}

func eventKey(event LocalProcessEvent) string {
	key := strings.ToLower(strings.TrimSpace(event.ProcessName))
	if label := humanizeEventType(event.Type); label != "" {
		key = key + ":" + strings.ToLower(label)
	}
	return strings.Trim(key, ":")
}

func humanizeEventType(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	return strings.ToUpper(string(raw[0])) + raw[1:]
}

func severityValue(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fatal", "error":
		return 3
	case "warn", "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func eventConfidence(severity int, event LocalProcessEvent) MemoryConfidence {
	if severity >= severityValue("error") || hasCrashSignal(event) {
		return MemoryConfidenceHigh
	}
	if severity >= severityValue("warn") {
		return MemoryConfidenceMedium
	}
	return MemoryConfidenceLow
}

func eventBucket(severity int) MemoryBucket {
	if severity >= severityValue("error") {
		return MemoryBucketActive
	}
	if severity >= severityValue("warn") {
		return MemoryBucketTentative
	}
	return MemoryBucketDurable
}

func isProcessEventNoise(event LocalProcessEvent) bool {
	if strings.TrimSpace(event.ProcessName) == "" {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(event.ProcessName))
	for _, noisy := range processEventNoiseProcesses {
		if strings.Contains(name, noisy) {
			return true
		}
	}
	for _, typ := range processEventNoiseTypes {
		if strings.EqualFold(event.Type, typ) {
			return true
		}
	}
	if severityValue(event.Level) < severityValue("warn") && !hasCrashSignal(event) {
		return true
	}
	return false
}

func hasCrashSignal(event LocalProcessEvent) bool {
	raw := strings.ToLower(strings.TrimSpace(event.Message + " " + event.Detail + " " + event.Type))
	for _, keyword := range processEventCrashSignals {
		if strings.Contains(raw, keyword) {
			return true
		}
	}
	return false
}

func localProcessEventFromRaw(raw map[string]any) LocalProcessEvent {
	event := LocalProcessEvent{
		ID:          firstString(raw, "id", "eventId", "sourceId"),
		ProcessName: firstString(raw, "process", "service", "source", "name"),
		Level:       firstString(raw, "level", "severity"),
		Type:        firstString(raw, "type", "event", "action", "state"),
		Message:     firstString(raw, "message", "msg", "description"),
		Detail:      firstString(raw, "detail", "details"),
		Source:      firstString(raw, "app", "source"),
		CommandLine: firstString(raw, "cmd", "command", "commandLine"),
	}
	if event.ProcessName == "" {
		event.ProcessName = event.Source
	}
	if timestamp := parseProcessEventTimestamp(raw); !timestamp.IsZero() {
		event.Timestamp = timestamp
	}
	if seconds := parseInt(raw, "durationSeconds"); seconds > 0 {
		event.Duration = time.Duration(seconds) * time.Second
		event.DurationMillis = seconds * 1000
	} else if millis := parseInt(raw, "durationMillis", "elapsedMs"); millis > 0 {
		event.Duration = time.Duration(millis) * time.Millisecond
		event.DurationMillis = millis
	}
	event.PID = parseInt(raw, "pid")
	event.CPUPercent = parseFloat(raw, "cpuPercent", "cpu")
	event.MemoryMB = parseFloat(raw, "memoryMb", "memory")
	event.Tags = parseStringSlice(raw, "tags")
	if metadata := parseStringMap(raw, "metadata", "meta"); len(metadata) > 0 {
		event.Metadata = metadata
	}
	return event
}

func parseProcessEventTimestamp(raw map[string]any) time.Time {
	for _, key := range []string{"ts", "timestamp", "time", "loggedAt", "observedAt"} {
		if data, ok := raw[key]; ok {
			if value := firstStringValue(data); value != "" {
				if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
					return t.UTC()
				}
				if t, err := time.Parse(time.RFC3339, value); err == nil {
					return t.UTC()
				}
				if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
					return t.UTC()
				}
				if t, err := time.Parse(time.RFC1123, value); err == nil {
					return t.UTC()
				}
			}
		}
	}
	return time.Time{}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text := firstStringValue(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstStringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return ""
	}
}

func parseInt(raw map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch v := value.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case json.Number:
				if i, err := v.Int64(); err == nil {
					return int(i)
				}
			case string:
				if parsed, err := parseStringToInt(v); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func parseStringToInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	var parsed int
	_, err := fmt.Sscanf(value, "%d", &parsed)
	return parsed, err
}

func parseFloat(raw map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch v := value.(type) {
			case float64:
				return v
			case int:
				return float64(v)
			case json.Number:
				if f, err := v.Float64(); err == nil {
					return f
				}
			case string:
				if parsed, err := parseStringToFloat(v); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func parseStringToFloat(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	var parsed float64
	_, err := fmt.Sscanf(value, "%f", &parsed)
	return parsed, err
}

func parseStringSlice(raw map[string]any, key string) []string {
	if value, ok := raw[key]; ok {
		switch v := value.(type) {
		case []any:
			out := make([]string, 0, len(v))
			for _, entry := range v {
				if text := firstStringValue(entry); text != "" {
					out = append(out, text)
				}
			}
			return out
		case string:
			text := strings.TrimSpace(v)
			if text == "" {
				return nil
			}
			parts := strings.Split(text, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
	}
	return nil
}

func parseStringMap(raw map[string]any, keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch nested := value.(type) {
			case map[string]any:
				for k, v := range nested {
					if text := firstStringValue(v); text != "" {
						out[k] = text
					}
				}
			case map[string]string:
				for k, v := range nested {
					if text := strings.TrimSpace(v); text != "" {
						out[k] = text
					}
				}
			}
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
