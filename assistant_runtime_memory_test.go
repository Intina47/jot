package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantBuildMessages_RecallsRelevantMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	mem := NewAssistantMemoryAt(path)
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Key:          "passport number",
		Value:        "A1234567",
		Evidence:     "passport scan",
		SourceType:   "browser",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationToolVerified,
		ObservedAt:   time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddObservation(passport) error = %v", err)
	}
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "preference",
		Key:          "favorite snack",
		Value:        "chips",
		Evidence:     "casual note",
		SourceType:   "user",
		Confidence:   MemoryConfidenceMedium,
		Verification: MemoryVerificationUserConfirmed,
		ObservedAt:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddObservation(snack) error = %v", err)
	}

	session := NewAssistantSession(
		&sequentialTestProvider{},
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)

	messages := session.BuildMessages("what is my passport number?", time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC))
	if len(messages) < 3 {
		t.Fatalf("expected system, memory, and user messages, got %#v", messages)
	}
	if !strings.Contains(messages[1].Content, "passport number") {
		t.Fatalf("expected recall message to include relevant passport memory, got %q", messages[1].Content)
	}
	if strings.Contains(messages[1].Content, "favorite snack") {
		t.Fatalf("expected irrelevant memory to be filtered out, got %q", messages[1].Content)
	}
}

func TestAssistantRunTurn_ConsolidatesMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	provider := &sequentialTestProvider{responses: []string{
		"That sounds like an important legal meeting next week.",
		`{"items":[{"scope":"situation","key":"child support lawyer meeting","value":"meeting next week","evidence":"legal meeting next week","confidence":"high","verification":"inferred","shouldStore":true}]}`,
	}}
	session := NewAssistantSession(
		provider,
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "I have a lawyer meeting next week", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if len(result.MemoryWrites) != 1 {
		t.Fatalf("expected one memory write, got %#v", result.MemoryWrites)
	}
	if !strings.Contains(result.MemoryWrites[0].Key, "lawyer") {
		t.Fatalf("expected stored memory to capture the inferred situation, got %#v", result.MemoryWrites[0])
	}

	loaded, err := LoadAssistantMemoryAt(path)
	if err != nil {
		t.Fatalf("LoadAssistantMemoryAt returned error: %v", err)
	}
	observations := loaded.Observations()
	if len(observations) != 1 {
		t.Fatalf("expected one persisted observation, got %#v", observations)
	}
	if !strings.Contains(observations[0].Key, "lawyer") {
		t.Fatalf("expected persisted observation to capture the inferred situation, got %#v", observations[0])
	}
}

func TestAssistantRunTurn_ConsolidatesInferredMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	provider := &sequentialTestProvider{responses: []string{
		"Ntina will probably finish training in late June.",
		`{"items":[{"kind":"situation","bucket":"tentative","scope":"user","subject":"Ntina","key":"army graduation timing","summary":"Ntina will likely finish basic training in late June","value":"late June graduation","evidence":"training timing discussed in the turn","inferenceReason":"basic training duration implies a late June finish","confidence":"medium","verification":"inferred","inferred":true,"shouldStore":true}]}`,
	}}
	session := NewAssistantSession(
		provider,
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "Ntina is starting army basic training", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if len(result.MemoryWrites) != 1 {
		t.Fatalf("expected one memory write, got %#v", result.MemoryWrites)
	}
	if result.MemoryWrites[0].InferenceReason == "" {
		t.Fatalf("expected inferred memory write, got %#v", result.MemoryWrites[0])
	}

	loaded, err := LoadAssistantMemoryAt(path)
	if err != nil {
		t.Fatalf("LoadAssistantMemoryAt returned error: %v", err)
	}
	inferences := loaded.Inferences()
	if len(inferences) != 1 {
		t.Fatalf("expected one persisted inference, got %#v", inferences)
	}
	if inferences[0].InferenceReason == "" {
		t.Fatalf("expected inference reason to persist, got %#v", inferences[0])
	}
}

func TestAssistantMemoryConsolidationExecutions_AreSourceAware(t *testing.T) {
	executions := []AssistantToolExecution{
		{
			Call: AssistantToolCall{Tool: "gmail.search"},
			Result: ToolResult{Success: true, Data: []NormalizedEmail{{
				ID:       "msg-1",
				ThreadID: "thread-1",
				Subject:  "Army training schedule",
				From:     "coach@example.com",
				Snippet:  "Graduation will likely be in late June.",
			}}},
		},
		{
			Call: AssistantToolCall{Tool: "setup.status_service"},
			Result: ToolResult{Success: true, Data: map[string]any{
				"service":   "go",
				"connected": true,
				"path":      `C:\Users\2435808\scoop\apps\go\current\bin\go.exe`,
			}},
		},
	}

	payload := assistantMemoryConsolidationExecutions(executions)
	if len(payload) != 2 {
		t.Fatalf("expected 2 execution payloads, got %#v", payload)
	}
	if got := payload[0]["sourceType"]; got != "gmail" {
		t.Fatalf("expected gmail source type, got %#v", got)
	}
	gmailHints, _ := payload[0]["sourceHints"].([]map[string]any)
	if len(gmailHints) == 0 || gmailHints[0]["sourceId"] != "msg-1" {
		t.Fatalf("expected gmail source hints with message id, got %#v", payload[0]["sourceHints"])
	}
	if got := payload[1]["sourceType"]; got != "local_machine" {
		t.Fatalf("expected local_machine source type, got %#v", got)
	}
}

func TestAssistantRunTurn_ConsolidatesSourceAwareObservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	provider := &sequentialTestProvider{responses: []string{
		"TOOL: gmail.search\nPARAMS: {\"query\":\"army training\",\"max\":3}",
		"I found an email with the likely graduation timing.",
		`{"items":[{"kind":"situation","bucket":"scheduled","scope":"user","subject":"Ntina","key":"army graduation timing","summary":"Graduation likely happens in late June","value":"late June graduation","evidence":"Email about army training schedule","evidenceRefs":["msg-1"],"sourceType":"gmail","sourceId":"msg-1","confidence":"high","shouldStore":true}]}`,
	}}
	capability := fakeAssistantCapability{
		name:  "gmail",
		tools: []Tool{{Name: "gmail.search"}},
		execute: func(toolName string, params map[string]any) (ToolResult, error) {
			return ToolResult{Success: true, Data: []NormalizedEmail{{
				ID:       "msg-1",
				ThreadID: "thread-1",
				Subject:  "Army training schedule",
				From:     "coach@example.com",
				Snippet:  "Graduation will likely be in late June.",
			}}}, nil
		},
	}
	session := NewAssistantSession(provider, []Capability{capability}, AssistantConfig{MemoryPath: path})

	var out bytes.Buffer
	result, err := session.RunTurn(context.Background(), "when will Ntina finish army training?", strings.NewReader(""), &out, time.Now)
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if len(result.MemoryWrites) != 1 {
		t.Fatalf("expected one memory write, got %#v", result.MemoryWrites)
	}
	if result.MemoryWrites[0].SourceType != "gmail" || result.MemoryWrites[0].SourceID != "msg-1" {
		t.Fatalf("expected gmail source metadata, got %#v", result.MemoryWrites[0])
	}
	if result.MemoryWrites[0].Verification != MemoryVerificationToolVerified {
		t.Fatalf("expected tool-verified memory from gmail evidence, got %#v", result.MemoryWrites[0])
	}
}

func TestAssistantBuildMessages_PrefersActiveMemoryForCurrentIntent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	mem := NewAssistantMemoryAt(path)
	now := time.Now().UTC()
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "current project",
		Value:        "jot memory",
		SourceType:   "journal",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketActive,
		ObservedAt:   now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(active project) error = %v", err)
	}
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "profile",
		Subject:      "Ntina",
		Key:          "career interest",
		Value:        "building developer tools",
		SourceType:   "user",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketDurable,
		ObservedAt:   now.Add(-30 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(durable profile) error = %v", err)
	}

	session := NewAssistantSession(
		&sequentialTestProvider{},
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)
	messages := session.BuildMessages("what am I working on right now?", now)
	if len(messages) < 3 {
		t.Fatalf("expected system, memory, and user messages, got %#v", messages)
	}
	if !strings.Contains(messages[1].Content, "current project = jot memory") {
		t.Fatalf("expected active current project memory to be recalled, got %q", messages[1].Content)
	}
}

func TestAssistantBuildMessages_PrefersScheduledMemoryForNearFutureIntent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	mem := NewAssistantMemoryAt(path)
	now := time.Now().UTC()
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:          "user",
		Subject:        "Ntina",
		Key:            "lawyer meeting",
		Value:          "child support lawyer meeting",
		SourceType:     "calendar",
		Confidence:     MemoryConfidenceHigh,
		Verification:   MemoryVerificationToolVerified,
		Bucket:         MemoryBucketScheduled,
		ObservedAt:     now.Add(-2 * time.Hour),
		EffectiveStart: now.Add(6 * 24 * time.Hour),
		EffectiveEnd:   now.Add(6*24*time.Hour + time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(scheduled) error = %v", err)
	}
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "profile",
		Subject:      "Ntina",
		Key:          "family status",
		Value:        "has children",
		SourceType:   "user",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketDurable,
		ObservedAt:   now.Add(-90 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(durable) error = %v", err)
	}

	session := NewAssistantSession(
		&sequentialTestProvider{},
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)
	messages := session.BuildMessages("what do I have next week?", now)
	if len(messages) < 3 {
		t.Fatalf("expected system, memory, and user messages, got %#v", messages)
	}
	if !strings.Contains(messages[1].Content, "lawyer meeting") {
		t.Fatalf("expected scheduled memory to be recalled for near-future intent, got %q", messages[1].Content)
	}
}

func TestAssistantBuildMessages_DurableFactStillWinsWhenDirectlyRelevant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	mem := NewAssistantMemoryAt(path)
	now := time.Now().UTC()
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "current project",
		Value:        "jot env",
		SourceType:   "journal",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketActive,
		ObservedAt:   now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(active) error = %v", err)
	}
	if _, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "passport number",
		Value:        "A1234567",
		SourceType:   "browser",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationToolVerified,
		Bucket:       MemoryBucketDurable,
		ObservedAt:   now.Add(-10 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("AddObservation(durable passport) error = %v", err)
	}

	session := NewAssistantSession(
		&sequentialTestProvider{},
		[]Capability{fakeAssistantCapability{name: "notes", tools: []Tool{{Name: "notes.echo"}}}},
		AssistantConfig{MemoryPath: path},
	)
	messages := session.BuildMessages("what is my passport number right now?", now)
	if len(messages) < 3 {
		t.Fatalf("expected system, memory, and user messages, got %#v", messages)
	}
	if !strings.Contains(messages[1].Content, "passport number = A1234567") {
		t.Fatalf("expected durable directly relevant fact to still be recalled, got %q", messages[1].Content)
	}
}
