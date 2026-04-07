package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildProcessEventSignals(t *testing.T) {
	now := time.Now().UTC()
	events := []LocalProcessEvent{
		{
			ProcessName: "example-service",
			Level:       "error",
			Type:        "crash",
			Message:     "unexpected termination",
			Detail:      "segfault in module",
			Source:      "local-agents",
			CommandLine: "example-service --watch",
			Timestamp:   now.Add(-5 * time.Minute),
		},
	}

	result := BuildProcessEventSignals(events, now)
	if len(result.Observations) != 1 {
		t.Fatalf("expected observation, got %d", len(result.Observations))
	}
	if len(result.Inferences) != 1 {
		t.Fatalf("expected inference, got %d", len(result.Inferences))
	}
	if len(result.Feed) != 1 {
		t.Fatalf("expected feed item, got %d", len(result.Feed))
	}
	if !strings.Contains(strings.ToLower(result.Feed[0].Title), "example-service") {
		t.Fatalf("unexpected feed title: %q", result.Feed[0].Title)
	}
	if !strings.Contains(result.Feed[0].Summary, "unexpected termination") {
		t.Fatalf("unexpected feed summary: %q", result.Feed[0].Summary)
	}
}

func TestBuildProcessEventSignals_IgnoresNoise(t *testing.T) {
	now := time.Now().UTC()
	events := []LocalProcessEvent{
		{
			ProcessName: "System Idle Process",
			Level:       "info",
			Message:     "keepalive",
			Timestamp:   now.Add(-2 * time.Minute),
		},
		{
			ProcessName: "editor",
			Level:       "info",
			Message:     "launched",
			Timestamp:   now.Add(-1 * time.Minute),
		},
	}
	result := BuildProcessEventSignals(events, now)
	if len(result.Observations) != 0 {
		t.Fatalf("expected no observation, got %d", len(result.Observations))
	}
	if len(result.Inferences) != 0 {
		t.Fatalf("expected no inference, got %d", len(result.Inferences))
	}
	if len(result.Feed) != 0 {
		t.Fatalf("expected no feed item, got %d", len(result.Feed))
	}
}

func TestLoadProcessEvents(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/events.jsonl"
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	lines := []string{
		`{"ts":"2026-03-24T09:00:00Z","process":"viewer","level":"info","message":"started"}`,
		`{"ts":"2026-03-24T09:00:05Z","process":"viewer","level":"warn","message":"slow response"}`,
	}
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("write line: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	events, err := LoadProcessEvents(path)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ProcessName != "viewer" || events[1].Level != "warn" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
