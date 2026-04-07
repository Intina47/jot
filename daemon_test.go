package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderDaemonHelpIncludesLifecycleCommands(t *testing.T) {
	help, err := renderHelp("daemon", false)
	if err != nil {
		t.Fatalf("renderHelp returned error: %v", err)
	}
	for _, snippet := range []string{
		"jot daemon",
		"jot daemon start",
		"jot daemon stop",
		"jot daemon status",
		"background loop",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotDaemonStartStatusStopSkeleton(t *testing.T) {
	withTempHome(t)

	fixedNow := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixedNow }

	originalLaunch := daemonLaunchWorker
	originalKill := daemonKillProcess
	originalHook := daemonProactiveWorkHook
	t.Cleanup(func() {
		daemonLaunchWorker = originalLaunch
		daemonKillProcess = originalKill
		daemonProactiveWorkHook = originalHook
	})

	launched := false
	killedPID := 0
	daemonLaunchWorker = func(exe string, args []string, logPath string) (daemonLaunchResult, error) {
		launched = true
		if strings.TrimSpace(exe) == "" {
			t.Fatalf("expected executable path")
		}
		if !strings.Contains(strings.Join(args, " "), "daemon worker") {
			t.Fatalf("expected worker args, got %v", args)
		}
		if !strings.HasSuffix(filepath.Clean(logPath), filepath.Join("daemon", "daemon.log")) {
			t.Fatalf("unexpected log path %q", logPath)
		}
		return daemonLaunchResult{PID: 4242}, nil
	}
	daemonKillProcess = func(pid int) error {
		killedPID = pid
		return nil
	}
	daemonProactiveWorkHook = func(context.Context, daemonLoopSnapshot) error {
		return nil
	}

	var out bytes.Buffer
	if err := jotDaemon(strings.NewReader(""), &out, []string{"start"}, now); err != nil {
		t.Fatalf("daemon start failed: %v", err)
	}
	if !launched {
		t.Fatalf("expected launcher to be called")
	}
	if !strings.Contains(out.String(), "daemon started") {
		t.Fatalf("expected start output to mention daemon started, got %q", out.String())
	}

	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatalf("daemonRuntimePaths failed: %v", err)
	}
	state, exists, err := readDaemonState(paths.statePath)
	if err != nil {
		t.Fatalf("readDaemonState failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected daemon state file to exist")
	}
	if state.PID != 4242 {
		t.Fatalf("expected pid 4242, got %d", state.PID)
	}
	if !state.isFresh(now()) {
		t.Fatalf("expected daemon state to be fresh")
	}

	out.Reset()
	if err := jotDaemon(strings.NewReader(""), &out, []string{"status"}, now); err != nil {
		t.Fatalf("daemon status failed: %v", err)
	}
	if !strings.Contains(out.String(), "daemon running") {
		t.Fatalf("expected status output to mention running, got %q", out.String())
	}

	out.Reset()
	if err := jotDaemon(strings.NewReader(""), &out, []string{"stop"}, now); err != nil {
		t.Fatalf("daemon stop failed: %v", err)
	}
	if killedPID != 4242 {
		t.Fatalf("expected pid 4242 to be killed, got %d", killedPID)
	}
	if _, err := os.Stat(paths.statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected daemon state file to be removed, got err=%v", err)
	}
	if !strings.Contains(out.String(), "daemon stopped") {
		t.Fatalf("expected stop output to mention daemon stopped, got %q", out.String())
	}
}

func TestDaemonBackgroundLoopInvokesHookAndPersistsHeartbeat(t *testing.T) {
	withTempHome(t)

	paths, err := daemonRuntimePaths()
	if err != nil {
		t.Fatalf("daemonRuntimePaths failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	hook := func(ctx context.Context, snapshot daemonLoopSnapshot) error {
		calls++
		if snapshot.Iteration != 0 {
			t.Fatalf("expected first snapshot iteration to be 0, got %d", snapshot.Iteration)
		}
		cancel()
		return nil
	}

	fixedNow := time.Date(2026, 4, 7, 12, 30, 0, 0, time.UTC)
	if err := daemonBackgroundLoop(ctx, paths.statePath, 9001, 10*time.Millisecond, func() time.Time { return fixedNow }, hook); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected hook to run once, got %d", calls)
	}

	state, exists, err := readDaemonState(paths.statePath)
	if err != nil {
		t.Fatalf("readDaemonState failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected daemon state file to exist")
	}
	if state.PID != 9001 {
		t.Fatalf("expected pid 9001, got %d", state.PID)
	}
	if state.LastHeartbeat.IsZero() {
		t.Fatalf("expected heartbeat to be written")
	}
}
