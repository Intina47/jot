package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConflictCopyPath(t *testing.T) {
	original := filepath.Join("notes", "entry.txt")
	at := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	got := conflictCopyPath(original, at, "device-01")
	expected := filepath.Join("notes", "entry.txt.conflict-20240506T070809Z-device-01")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestConflictCopyPathDefaultsActor(t *testing.T) {
	original := filepath.Join("notes", "entry.txt")
	at := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	got := conflictCopyPath(original, at, "")
	expected := filepath.Join("notes", "entry.txt.conflict-20240506T070809Z-unknown")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
