package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherDetectsCreateModifyDelete(t *testing.T) {
	dir := t.TempDir()
	watcher, err := NewWatcher(dir)
	if err != nil {
		t.Fatalf("NewWatcher returned error: %v", err)
	}

	events, err := watcher.Poll()
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %v", events)
	}

	notePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(notePath, []byte("first"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	events, err = watcher.Poll()
	if err != nil {
		t.Fatalf("Poll returned error after create: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventCreate || events[0].Path != notePath {
		t.Fatalf("expected create event for %s, got %v", notePath, events)
	}

	if err := os.WriteFile(notePath, []byte("second"), 0o600); err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	modTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(notePath, modTime, modTime); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	events, err = watcher.Poll()
	if err != nil {
		t.Fatalf("Poll returned error after modify: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventModify || events[0].Path != notePath {
		t.Fatalf("expected modify event for %s, got %v", notePath, events)
	}

	if err := os.Remove(notePath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	events, err = watcher.Poll()
	if err != nil {
		t.Fatalf("Poll returned error after delete: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventDelete || events[0].Path != notePath {
		t.Fatalf("expected delete event for %s, got %v", notePath, events)
	}
}
