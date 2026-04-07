package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func daemonWatchGmail(ctx context.Context, snapshot daemonLoopSnapshot) ([]AssistantFeedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cap, err := NewGmailCapability(snapshot.Config)
	if err != nil {
		return nil, nil
	}
	result, err := cap.Execute("gmail.search", map[string]any{
		"query": "is:inbox is:unread newer_than:3d",
		"max":   3,
	})
	if err != nil {
		return nil, nil
	}
	emails, _ := result.Data.([]NormalizedEmail)
	if len(emails) == 0 {
		return nil, nil
	}
	items := make([]AssistantFeedItem, 0, len(emails))
	for _, email := range emails {
		items = append(items, AssistantFeedItem{
			ID:         "gmail:" + email.ID,
			Key:        "gmail:" + email.ID,
			Kind:       AssistantFeedKindResearchBrief,
			Status:     AssistantFeedStatusNew,
			Title:      "Unread email from " + assistantSenderName(email.From),
			Summary:    email.Subject,
			Body:       email.Snippet,
			SourceType: "gmail",
			SourceID:   email.ID,
			Links: []AssistantFeedLink{{
				Label: strings.TrimSpace(email.Subject),
				URL:   "https://mail.google.com/mail/u/0/#search/rfc822msgid:" + email.ID,
				Preview: assistantDefaultString(email.Snippet, "unread message"),
			}},
			MemoryRefs: []string{},
			CreatedAt:  snapshot.Now,
			UpdatedAt:  snapshot.Now,
		})
	}
	return items, nil
}

func daemonWatchCalendar(ctx context.Context, snapshot daemonLoopSnapshot) ([]AssistantFeedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	cap := &CalendarCapability{}
	params := map[string]any{
		"start":       snapshot.Now.UTC().Format(time.RFC3339),
		"end":         snapshot.Now.Add(48 * time.Hour).UTC().Format(time.RFC3339),
		"max_results": 5,
	}
	result, err := cap.Execute("calendar.find_events", params)
	if err != nil {
		return nil, nil
	}
	data, _ := result.Data.(map[string]any)
	events, _ := data["events"].([]map[string]any)
	items := make([]AssistantFeedItem, 0, len(events))
	for _, event := range events {
		id := assistantStringValue(event["id"])
		summary := assistantStringValue(event["summary"])
		if summary == "" {
			summary = assistantStringValue(event["description"])
		}
		start := assistantStringValue(event["start"])
		item := AssistantFeedItem{
			ID:            "calendar:" + id,
			Key:           "calendar:" + id,
			Kind:          AssistantFeedKindDeadlineAlert,
			Status:        AssistantFeedStatusNew,
			Title:         "Upcoming event: " + assistantDefaultString(summary, "calendar item"),
			Summary:       summary,
			Body:          formatCalendarTime(start),
			SourceType:    "calendar",
			SourceID:      id,
			Links:         []AssistantFeedLink{{Label: summary, URL: assistantStringValue(event["htmlLink"]), Preview: assistantDefaultString(assistantStringValue(event["location"]), "upcoming event")}},
			DueAt:         assistantParseTime(start),
			MemoryRefs:    []string{},
			CreatedAt:     snapshot.Now,
			UpdatedAt:     snapshot.Now,
			Importance:    2,
		}
		items = append(items, item)
	}
	return items, nil
}

func daemonWatchJournal(ctx context.Context, snapshot daemonLoopSnapshot) ([]AssistantFeedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	journalDir, _, journalPath := journalPaths(home)
	if err != nil {
		return nil, nil
	}
	entries, err := loadJournalEntries(journalPath)
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	entry := entries[len(entries)-1]
	item := AssistantFeedItem{
		ID:         "journal:" + entry.ID,
		Key:        "journal:" + entry.ID,
		Kind:       AssistantFeedKindResearchBrief,
		Status:     AssistantFeedStatusNew,
		Title:      "Journal note: " + entry.Title,
		Summary:    entry.Content,
		Body:       strings.Join(entry.Tags, ", "),
		SourceType: "journal",
		SourceID:   journalPath,
		Links: []AssistantFeedLink{{
			Label: "open entry",
			URL:   journalDir,
			Preview: assistantDefaultString(strings.Join(entry.Tags, ", "), "journal entry"),
		}},
		CreatedAt: snapshot.Now,
		UpdatedAt: snapshot.Now,
	}
	return []AssistantFeedItem{item}, nil
}

func daemonWatchLocalMachine(ctx context.Context, snapshot daemonLoopSnapshot) ([]AssistantFeedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	downloads := filepath.Join(home, "Downloads")
	files, err := os.ReadDir(downloads)
	if err != nil {
		return nil, nil
	}
	var newest os.DirEntry
	var modTime time.Time
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(modTime) {
			modTime = info.ModTime()
			newest = f
		}
	}
	if newest == nil {
		return nil, nil
	}
	item := AssistantFeedItem{
		ID:         "local:newfile:" + newest.Name(),
		Key:        "local:newfile:" + newest.Name(),
		Kind:       AssistantFeedKindPrepPlan,
		Status:     AssistantFeedStatusNew,
		Title:      "Downloaded file: " + newest.Name(),
		Summary:    "Latest file in Downloads",
		Body:       "Modified " + assistantFormatTime(modTime),
		SourceType: "local",
		SourceID:   downloads,
		Links: []AssistantFeedLink{{
			Label: newest.Name(),
			URL:   downloads,
			Preview: assistantFormatTime(modTime),
		}},
		CreatedAt: snapshot.Now,
		UpdatedAt: snapshot.Now,
	}
	return []AssistantFeedItem{item}, nil
}

func assistantParseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func assistantFormatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("Mon Jan 2, 3:04pm")
}
func formatCalendarTime(value string) string {
	if value == "" {
		return ""
	}
	if t := assistantParseTime(value); !t.IsZero() {
		return "at " + assistantFormatTime(t)
	}
	return value
}
