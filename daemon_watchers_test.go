package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonWatchLocalMachineReturnsDownloadHint(t *testing.T) {
	dir := t.TempDir()
	downloads := filepath.Join(dir, "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatalf("mkdir Downloads: %v", err)
	}
	filename := filepath.Join(downloads, "notes.txt")
	if err := os.WriteFile(filename, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	items, err := daemonWatchLocalMachine(context.Background(), daemonLoopSnapshot{Now: time.Now()})
	if err != nil {
		t.Fatalf("daemonWatchLocalMachine error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]
	if item.Title == "" || item.Body == "" {
		t.Fatalf("unexpected item %+v", item)
	}
	if !strings.Contains(item.Title, "Downloaded file") {
		t.Fatalf("expected title mention download, got %q", item.Title)
	}
}

func TestDaemonWatchJournalReturnsLatestEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	journalDir, _, journalPath := journalPaths(dir)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir journal dir: %v", err)
	}
	entry := journalEntry{
		Title:   "Diary",
		Content: "reflect",
		Tags:    []string{"test"},
	}
	if err := appendJournalEntry(journalPath, entry); err != nil {
		t.Fatalf("append journal entry: %v", err)
	}

	items, err := daemonWatchJournal(context.Background(), daemonLoopSnapshot{Now: time.Now()})
	if err != nil {
		t.Fatalf("daemonWatchJournal error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 journal item, got %d", len(items))
	}
	if items[0].SourceType != "journal" {
		t.Fatalf("expected journal source, got %s", items[0].SourceType)
	}
	if !strings.Contains(items[0].Title, "Journal note") {
		t.Fatalf("unexpected title %q", items[0].Title)
	}
}

func TestDaemonWatchGmailRunsSafely(t *testing.T) {
	items, err := daemonWatchGmail(context.Background(), daemonLoopSnapshot{})
	if err != nil {
		t.Fatalf("daemonWatchGmail error: %v", err)
	}
	if items == nil {
		t.Fatalf("expected slice, got nil")
	}
}

func TestDaemonWatchCalendarSkipsWhenUnavailable(t *testing.T) {
	items, err := daemonWatchCalendar(context.Background(), daemonLoopSnapshot{})
	if err != nil {
		t.Fatalf("daemonWatchCalendar error: %v", err)
	}
	if items == nil {
		t.Fatalf("expected slice, got nil")
	}
}
