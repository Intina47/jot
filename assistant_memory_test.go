package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantMemory_AddObservationAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	mem := NewAssistantMemoryAt(path)

	first := MemoryObservation{
		Scope:        "user",
		Key:          "passport number",
		Value:        "A1234567",
		Evidence:     "passport scan",
		SourceType:   "browser",
		SourceID:     "page-1",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationToolVerified,
		ObservedAt:   time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
	}
	stored, err := mem.AddObservation(first)
	if err != nil {
		t.Fatalf("AddObservation() error = %v", err)
	}
	if stored.ID == "" {
		t.Fatal("expected stored observation ID")
	}

	second := MemoryObservation{
		Scope:        "contact",
		ContactAlias: "Palma codes",
		Key:          "beer preference",
		Value:        "stella",
		SourceType:   "gmail",
		SourceID:     "msg-1",
		Confidence:   MemoryConfidenceMedium,
		Verification: MemoryVerificationUserConfirmed,
		ObservedAt:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		EffectiveAt:  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}
	storedContact, err := mem.AddObservation(second)
	if err != nil {
		t.Fatalf("AddObservation(contact) error = %v", err)
	}
	if storedContact.ContactID == "" {
		t.Fatal("expected contact id to be assigned")
	}

	loaded, err := LoadAssistantMemoryAt(path)
	if err != nil {
		t.Fatalf("LoadAssistantMemoryAt() error = %v", err)
	}
	if got := len(loaded.Observations()); got != 2 {
		t.Fatalf("expected 2 observations after reload, got %d", got)
	}
	if loaded.SchemaVersion != assistantMemorySchemaVersion {
		t.Fatalf("expected schema version %d, got %d", assistantMemorySchemaVersion, loaded.SchemaVersion)
	}
	if got := len(loaded.Facts()); got != 2 {
		t.Fatalf("expected 2 facts after reload, got %d", got)
	}
	id, ok := loaded.ResolveContactID("Palma codes")
	if !ok {
		t.Fatal("expected contact alias to resolve")
	}
	if id != storedContact.ContactID {
		t.Fatalf("expected alias to resolve to %q, got %q", storedContact.ContactID, id)
	}
}

func TestAssistantMemory_BestFactsPrefersUserConfirmed(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))

	baseTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	_, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Key:          "share code",
		Value:        "ABC 123 456",
		SourceType:   "gmail",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationToolVerified,
		ObservedAt:   baseTime,
	})
	if err != nil {
		t.Fatalf("AddObservation(tool) error = %v", err)
	}
	_, err = mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Key:          "share code",
		Value:        "XYZ 987 654",
		SourceType:   "user",
		Confidence:   MemoryConfidenceMedium,
		Verification: MemoryVerificationUserConfirmed,
		ObservedAt:   baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("AddObservation(user) error = %v", err)
	}

	fact, ok := mem.BestFact("share code")
	if !ok {
		t.Fatal("expected best fact")
	}
	if fact.Value != "XYZ 987 654" {
		t.Fatalf("expected user-confirmed fact to win, got %q", fact.Value)
	}
	if fact.Verification != MemoryVerificationUserConfirmed {
		t.Fatalf("expected user-confirmed verification, got %q", fact.Verification)
	}
}

func TestAssistantMemory_MigratesLegacySchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantMemoryFileName)
	legacy := map[string]any{
		"owner": "ntina",
		"contactsByID": map[string]any{
			"contact-abc": map[string]any{
				"id":      "contact-abc",
				"label":   "Palma",
				"aliases": []string{"Palma"},
			},
		},
		"observations": []map[string]any{
			{
				"scope":        "contact",
				"contactAlias": "Palma",
				"key":          "meeting note",
				"value":        "met on Friday",
				"sourceType":   "gmail",
				"confidence":   MemoryConfidenceHigh,
				"verification": MemoryVerificationUserConfirmed,
				"observedAt":   "2026-04-01T10:00:00Z",
			},
		},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	loaded, err := LoadAssistantMemoryAt(path)
	if err != nil {
		t.Fatalf("LoadAssistantMemoryAt() error = %v", err)
	}
	if loaded.SchemaVersion != assistantMemorySchemaVersion {
		t.Fatalf("expected migrated schema version %d, got %d", assistantMemorySchemaVersion, loaded.SchemaVersion)
	}
	if got := len(loaded.Observations()); got != 1 {
		t.Fatalf("expected 1 migrated observation, got %d", got)
	}
	fact, ok := loaded.BestFact("meeting note")
	if !ok {
		t.Fatal("expected migrated fact")
	}
	if fact.Kind == "" {
		t.Fatal("expected migrated fact kind to be populated")
	}
	if fact.Bucket == "" {
		t.Fatal("expected migrated fact bucket to be populated")
	}
	if fact.RetrievalText == "" {
		t.Fatal("expected migrated fact retrieval text to be populated")
	}
	if !strings.Contains(strings.ToLower(fact.RetrievalText), "meeting note") {
		t.Fatalf("expected retrieval text to mention key, got %q", fact.RetrievalText)
	}
}

func TestAssistantMemory_RankingPrefersDurableHighImportance(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))

	baseTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	_, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Key:          "army training date",
		Value:        "basic training ends on June 10",
		SourceType:   "user",
		Confidence:   MemoryConfidenceLow,
		Verification: MemoryVerificationInferred,
		Bucket:       MemoryBucketTentative,
		Importance:   10,
		ObservedAt:   baseTime,
	})
	if err != nil {
		t.Fatalf("AddObservation(tentative) error = %v", err)
	}
	_, err = mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Key:          "army training date",
		Value:        "basic training ends on June 24",
		SourceType:   "user",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketDurable,
		Importance:   90,
		ObservedAt:   baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("AddObservation(durable) error = %v", err)
	}

	fact, ok := mem.BestFact("army training date")
	if !ok {
		t.Fatal("expected best fact")
	}
	if fact.Value != "basic training ends on June 24" {
		t.Fatalf("expected durable fact to win, got %q", fact.Value)
	}
	if fact.Bucket != MemoryBucketDurable {
		t.Fatalf("expected durable bucket, got %q", fact.Bucket)
	}

	results := mem.SearchResults("when does army training end", 1)
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if !strings.Contains(strings.ToLower(results[0].Text), "june 24") {
		t.Fatalf("expected semantic search to return the durable fact, got %q", results[0].Text)
	}
}

func TestAssistantMemory_LinkContactAlias(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))
	id, err := mem.LinkContactAlias("contact-abc", "Palma codes")
	if err != nil {
		t.Fatalf("LinkContactAlias() error = %v", err)
	}
	if id != "contact-abc" {
		t.Fatalf("expected contact id contact-abc, got %q", id)
	}
	resolved, ok := mem.ResolveContactID("palma codes")
	if !ok {
		t.Fatal("expected alias to resolve after linking")
	}
	if resolved != "contact-abc" {
		t.Fatalf("expected resolved id contact-abc, got %q", resolved)
	}
	contacts := mem.Contacts()
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if len(contacts[0].Aliases) != 1 || contacts[0].Aliases[0] != "Palma codes" {
		t.Fatalf("unexpected aliases: %#v", contacts[0].Aliases)
	}
}

func TestAssistantMemory_RepeatedEvidenceReinforcesExistingObservation(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))
	firstTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	secondTime := firstTime.Add(48 * time.Hour)

	first, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "army graduation timing",
		Summary:      "Graduation likely happens in late June",
		Value:        "late June graduation",
		Evidence:     "first email hint",
		SourceType:   "gmail",
		SourceID:     "msg-1",
		Confidence:   MemoryConfidenceMedium,
		Verification: MemoryVerificationInferred,
		Bucket:       MemoryBucketTentative,
		Importance:   30,
		ObservedAt:   firstTime,
	})
	if err != nil {
		t.Fatalf("AddObservation(first) error = %v", err)
	}

	second, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "army graduation timing",
		Summary:      "Graduation likely happens in late June",
		Value:        "late June graduation",
		Evidence:     "calendar confirmation",
		SourceType:   "calendar",
		SourceID:     "evt-1",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationToolVerified,
		Bucket:       MemoryBucketScheduled,
		Importance:   60,
		ObservedAt:   secondTime,
	})
	if err != nil {
		t.Fatalf("AddObservation(second) error = %v", err)
	}

	observations := mem.Observations()
	if len(observations) != 1 {
		t.Fatalf("expected reinforcement to keep 1 observation, got %#v", observations)
	}
	if first.ID != second.ID {
		t.Fatalf("expected reinforced observation to keep same identity, got %q and %q", first.ID, second.ID)
	}
	if observations[0].Verification != MemoryVerificationToolVerified {
		t.Fatalf("expected stronger verification after reinforcement, got %#v", observations[0])
	}
	if observations[0].Confidence != MemoryConfidenceHigh {
		t.Fatalf("expected stronger confidence after reinforcement, got %#v", observations[0])
	}
	if observations[0].Bucket != MemoryBucketScheduled {
		t.Fatalf("expected stronger bucket after reinforcement, got %#v", observations[0])
	}
	if !strings.Contains(observations[0].Evidence, "calendar confirmation") {
		t.Fatalf("expected merged evidence, got %#v", observations[0])
	}
}

func TestAssistantMemory_ConflictingTemporalEvidenceExpiresOldMemory(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	change := start.Add(7 * 24 * time.Hour)

	_, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "current project",
		Summary:      "Working on Jot memory",
		Value:        "jot memory",
		SourceType:   "journal",
		SourceID:     "entry-1",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketActive,
		ObservedAt:   start,
	})
	if err != nil {
		t.Fatalf("AddObservation(first) error = %v", err)
	}
	_, err = mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "current project",
		Summary:      "Working on Jot env",
		Value:        "jot env",
		SourceType:   "journal",
		SourceID:     "entry-2",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationUserConfirmed,
		Bucket:       MemoryBucketActive,
		ObservedAt:   change,
	})
	if err != nil {
		t.Fatalf("AddObservation(second) error = %v", err)
	}

	observations := mem.Observations()
	if len(observations) != 2 {
		t.Fatalf("expected old and new observations to remain, got %#v", observations)
	}
	expiredCount := 0
	activeCount := 0
	for _, item := range observations {
		switch item.Bucket {
		case MemoryBucketExpired:
			expiredCount++
			if item.EffectiveEnd.IsZero() {
				t.Fatalf("expected expired memory to have effective end, got %#v", item)
			}
		case MemoryBucketActive:
			activeCount++
		}
	}
	if expiredCount != 1 || activeCount != 1 {
		t.Fatalf("expected one expired and one active observation, got %#v", observations)
	}
	fact, ok := mem.BestFact("current project")
	if !ok {
		t.Fatal("expected best fact")
	}
	if fact.Value != "jot env" {
		t.Fatalf("expected latest active project to win, got %#v", fact)
	}
}

func TestAssistantMemory_ScheduledRolloverToActiveAndExpired(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))
	now := time.Now().UTC()

	active, err := mem.AddObservation(MemoryObservation{
		Scope:          "user",
		Subject:        "Ntina",
		Key:            "army training window",
		Value:          "basic training underway",
		SourceType:     "calendar",
		SourceID:       "evt-active",
		Confidence:     MemoryConfidenceHigh,
		Verification:   MemoryVerificationToolVerified,
		Bucket:         MemoryBucketScheduled,
		ObservedAt:     now.Add(-48 * time.Hour),
		EffectiveStart: now.Add(-24 * time.Hour),
		EffectiveEnd:   now.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AddObservation(active rollover) error = %v", err)
	}
	if active.Bucket != MemoryBucketActive {
		t.Fatalf("expected scheduled memory to roll over to active, got %#v", active)
	}

	expired, err := mem.AddObservation(MemoryObservation{
		Scope:          "user",
		Subject:        "Ntina",
		Key:            "old army training window",
		Value:          "basic training completed",
		SourceType:     "calendar",
		SourceID:       "evt-expired",
		Confidence:     MemoryConfidenceHigh,
		Verification:   MemoryVerificationToolVerified,
		Bucket:         MemoryBucketActive,
		ObservedAt:     now.Add(-20 * 24 * time.Hour),
		EffectiveStart: now.Add(-18 * 24 * time.Hour),
		EffectiveEnd:   now.Add(-2 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AddObservation(expired rollover) error = %v", err)
	}
	if expired.Bucket != MemoryBucketExpired {
		t.Fatalf("expected elapsed memory to roll over to expired, got %#v", expired)
	}
}

func TestAssistantMemory_ConfidenceDecayForStaleInferredMemory(t *testing.T) {
	mem := NewAssistantMemoryAt(filepath.Join(t.TempDir(), assistantMemoryFileName))
	old := time.Now().UTC().Add(-200 * 24 * time.Hour)

	item, err := mem.AddObservation(MemoryObservation{
		Scope:        "user",
		Subject:      "Ntina",
		Key:          "possible graduation date",
		Value:        "late June",
		SourceType:   "gmail",
		SourceID:     "msg-old",
		Confidence:   MemoryConfidenceHigh,
		Verification: MemoryVerificationInferred,
		Bucket:       MemoryBucketTentative,
		ObservedAt:   old,
	})
	if err != nil {
		t.Fatalf("AddObservation(stale inferred) error = %v", err)
	}
	if item.Confidence != MemoryConfidenceUnknown {
		t.Fatalf("expected stale inferred memory confidence to decay to unknown, got %#v", item)
	}
}
