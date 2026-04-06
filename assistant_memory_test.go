package main

import (
	"path/filepath"
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
