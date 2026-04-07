package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantFeed_AddItemAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, assistantFeedFileName)
	feed := NewAssistantFeedAt(path)

	item, err := feed.AddItem(AssistantFeedItem{
		Key:        "deadline:army-basic",
		Kind:       AssistantFeedKindPrepPlan,
		Title:      "Army basic training prep",
		Summary:    "basic training starts next week",
		SourceType: "gmail",
		SourceID:   "msg-1",
		Confidence: MemoryConfidenceHigh,
		Importance: 80,
		DueAt:      time.Date(2026, 4, 13, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}
	if item.ID == "" {
		t.Fatal("expected stored feed item ID")
	}

	loaded, err := LoadAssistantFeedAt(path)
	if err != nil {
		t.Fatalf("LoadAssistantFeedAt() error = %v", err)
	}
	items := loaded.VisibleItems(time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC))
	if len(items) != 1 {
		t.Fatalf("expected 1 visible item, got %d", len(items))
	}
	if items[0].Title != "Army basic training prep" {
		t.Fatalf("unexpected loaded title: %q", items[0].Title)
	}
	if items[0].RetrievalText == "" {
		t.Fatal("expected retrieval text to be populated")
	}
}

func TestAssistantFeed_ApplyActionUpdatesStatus(t *testing.T) {
	feed := NewAssistantFeedAt(filepath.Join(t.TempDir(), assistantFeedFileName))
	stored, err := feed.AddItem(AssistantFeedItem{
		Key:        "trip:weekend",
		Kind:       AssistantFeedKindTripSuggestion,
		Title:      "Trip ideas",
		Summary:    "London trip next weekend",
		SourceType: "journal",
		Importance: 50,
	})
	if err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}

	updated, ok, err := feed.ApplyAction("feed:"+stored.ID+":done", time.Now().UTC())
	if err != nil {
		t.Fatalf("ApplyAction() error = %v", err)
	}
	if !ok {
		t.Fatal("expected ApplyAction() to handle feed action")
	}
	if updated.Status != AssistantFeedStatusDone {
		t.Fatalf("expected done status, got %q", updated.Status)
	}
	if len(feed.VisibleItems(time.Now().UTC())) != 0 {
		t.Fatalf("expected completed item to be hidden from visible feed")
	}
}

func TestAssistantFeed_RolloverSnoozedItem(t *testing.T) {
	feed := NewAssistantFeedAt(filepath.Join(t.TempDir(), assistantFeedFileName))
	stored, err := feed.AddItem(AssistantFeedItem{
		Key:          "deadline:tomorrow",
		Kind:         AssistantFeedKindDeadlineAlert,
		Title:        "Submit forms",
		Status:       AssistantFeedStatusSnoozed,
		SnoozedUntil: time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC),
		Importance:   90,
	})
	if err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}

	items := feed.VisibleItems(time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC))
	if len(items) != 1 {
		t.Fatalf("expected snoozed item to reappear, got %d items", len(items))
	}
	if items[0].Status != AssistantFeedStatusNew {
		t.Fatalf("expected snoozed item to roll back to new, got %q", items[0].Status)
	}
	if items[0].ID != stored.ID {
		t.Fatalf("expected item identity to remain stable, got %q and %q", items[0].ID, stored.ID)
	}
}

func TestRenderAssistantFeedItems(t *testing.T) {
	item := assistantFeedItemViewFromModel(AssistantFeedItem{
		ID:         "feed-123",
		Kind:       AssistantFeedKindResearchBrief,
		Status:     AssistantFeedStatusSeen,
		Title:      "Research brief",
		Summary:    "Find prep for next week",
		Reason:     "user has a deadline",
		SourceType: "calendar",
		SourceID:   "event-1",
		Links: []AssistantFeedLink{{
			Label:   "open brief",
			URL:     "https://example.com/brief",
			Preview: "example preview",
		}},
	}, time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC))

	html := renderAssistantFeedItems([]AssistantFeedItemView{item}, time.Now().UTC())
	if !strings.Contains(html, "Research brief") {
		t.Fatalf("expected title in rendered feed html, got %q", html)
	}
	if !strings.Contains(html, "open brief") {
		t.Fatalf("expected link label in rendered feed html, got %q", html)
	}
	if !strings.Contains(html, "ready") {
		t.Fatalf("expected status label in rendered feed html, got %q", html)
	}
}
