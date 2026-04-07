package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDaemonWatchTerminalProcesses(t *testing.T) {
	now := time.Now().UTC()
	orig := processSnapshotCollector
	processSnapshotCollector = func(ctx context.Context, options ProcessCaptureOptions) (ProcessSnapshot, error) {
		return ProcessSnapshot{
			CapturedAt: now,
			Processes: []ProcessInfo{
				{
					PID:         111,
					Command:     "bash",
					CommandLine: "bash -l",
					User:        "tester",
					TTY:         "pts/2",
					Elapsed:     2 * time.Hour,
				},
				{
					PID:         222,
					Command:     "vim",
					CommandLine: "vim daemon_process_watchers.go",
					User:        "tester",
					TTY:         "pts/2",
					Elapsed:     15 * time.Minute,
				},
			},
		}, nil
	}
	defer func() {
		processSnapshotCollector = orig
	}()

	items, err := daemonWatchTerminalProcesses(context.Background(), daemonLoopSnapshot{Now: now})
	if err != nil {
		t.Fatalf("daemonWatchTerminalProcesses error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]
	if item.SourceType != "process" {
		t.Fatalf("unexpected source type %q", item.SourceType)
	}
	if !strings.Contains(item.Summary, "bash") {
		t.Fatalf("missing expected process summary, got %q", item.Summary)
	}
}

func TestDaemonWatchTerminalProcessesEmpty(t *testing.T) {
	now := time.Now().UTC()
	orig := processSnapshotCollector
	processSnapshotCollector = func(ctx context.Context, options ProcessCaptureOptions) (ProcessSnapshot, error) {
		return ProcessSnapshot{CapturedAt: now}, nil
	}
	defer func() {
		processSnapshotCollector = orig
	}()

	items, err := daemonWatchTerminalProcesses(context.Background(), daemonLoopSnapshot{Now: now})
	if err != nil {
		t.Fatalf("daemonWatchTerminalProcesses error: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil items for empty snapshot, got %d", len(items))
	}
}
