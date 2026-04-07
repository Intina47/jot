package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultProcessLimit = 4

type ProcessCaptureOptions struct {
	Limit int
	Now   time.Time
}

type ProcessSnapshot struct {
	CapturedAt time.Time
	Processes  []ProcessInfo
}

type ProcessInfo struct {
	PID         int
	Command     string
	CommandLine string
	User        string
	TTY         string
	Elapsed     time.Duration
}

var processSnapshotCollector = captureProcessSnapshotPlatform

// CaptureProcessSnapshot returns a snapshot of the most recent terminal-owned processes on this machine.
func CaptureProcessSnapshot(ctx context.Context, options ProcessCaptureOptions) (ProcessSnapshot, error) {
	if options.Limit <= 0 {
		options.Limit = defaultProcessLimit
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	return processSnapshotCollector(ctx, options)
}

func captureProcessSnapshotPlatform(ctx context.Context, options ProcessCaptureOptions) (ProcessSnapshot, error) {
	procSnapshot := ProcessSnapshot{CapturedAt: options.Now}
	switch runtime.GOOS {
	case "windows":
		return procSnapshot, nil
	default:
		processes, err := captureUnixProcesses(ctx, options.Limit)
		if err != nil {
			return ProcessSnapshot{}, err
		}
		if len(processes) > options.Limit {
			processes = processes[:options.Limit]
		}
		procSnapshot.Processes = processes
		return procSnapshot, nil
	}
}

func captureUnixProcesses(ctx context.Context, limit int) ([]ProcessInfo, error) {
	cmd := exec.CommandContext(ctx, "ps", "-eo", "pid=,uid=,etimes=,tty=,comm=,args=", "-ww", "--sort=-etimes")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var result []ProcessInfo
	lines := bytes.Split(output, []byte{'\n'})
	for _, rawLine := range lines {
		if len(result) >= limit {
			break
		}
		line := strings.TrimSpace(string(rawLine))
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		tty := parts[3]
		if tty == "" || tty == "?" {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		elapsedSeconds, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		username := parts[1]
		if u, err := user.LookupId(parts[1]); err == nil && u.Username != "" {
			username = u.Username
		}
		command := parts[4]
		args := ""
		if len(parts) > 5 {
			args = strings.Join(parts[5:], " ")
		}
		commandLine := command
		if args != "" {
			commandLine = command + " " + args
		}
		result = append(result, ProcessInfo{
			PID:         pid,
			Command:     command,
			CommandLine: strings.TrimSpace(commandLine),
			User:        username,
			TTY:         tty,
			Elapsed:     time.Duration(elapsedSeconds) * time.Second,
		})
	}
	return result, nil
}

func daemonWatchTerminalProcesses(ctx context.Context, snapshot daemonLoopSnapshot) ([]AssistantFeedItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	procSnapshot, err := CaptureProcessSnapshot(ctx, ProcessCaptureOptions{Limit: 4, Now: snapshot.Now})
	if err != nil || len(procSnapshot.Processes) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(procSnapshot.Processes))
	refs := make([]string, 0, len(procSnapshot.Processes))
	for _, proc := range procSnapshot.Processes {
		lines = append(lines, formatProcessLine(proc))
		refs = append(refs, fmt.Sprintf("pid:%d", proc.PID))
	}
	summary := strings.Join(lines, " | ")
	body := strings.Join(lines, "\n")
	if summary == "" {
		return nil, nil
	}
	hash := sha1.Sum([]byte(summary))
	id := fmt.Sprintf("local:process-context:%x", hash[:6])
	title := "Terminal activity snapshot"
	if len(procSnapshot.Processes) > 0 && procSnapshot.Processes[0].Command != "" {
		title = fmt.Sprintf("Terminal activity: %s", procSnapshot.Processes[0].Command)
	}
	item := AssistantFeedItem{
		ID:         id,
		Key:        id,
		Kind:       AssistantFeedKindResearchBrief,
		Status:     AssistantFeedStatusNew,
		Title:      title,
		Summary:    summary,
		Body:       body,
		Reason:     "Snapshot of interactive terminal processes",
		SourceType: "process",
		SourceID:   id,
		SourceRefs: refs,
		CreatedAt:  snapshot.Now,
		UpdatedAt:  snapshot.Now,
		Importance: 35,
	}
	return []AssistantFeedItem{item}, nil
}

func formatProcessLine(proc ProcessInfo) string {
	cmd := proc.CommandLine
	if cmd == "" {
		cmd = proc.Command
	}
	if cmd == "" {
		cmd = fmt.Sprintf("pid %d", proc.PID)
	}
	cmd = truncateString(cmd, 140)
	parts := make([]string, 0, 3)
	if proc.User != "" {
		parts = append(parts, proc.User)
	}
	if proc.TTY != "" {
		parts = append(parts, fmt.Sprintf("tty=%s", proc.TTY))
	}
	if proc.Elapsed > 0 {
		parts = append(parts, fmt.Sprintf("up %s", humanDuration(proc.Elapsed)))
	}
	if len(parts) == 0 {
		return cmd
	}
	return fmt.Sprintf("%s (%s)", cmd, strings.Join(parts, " | "))
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func humanDuration(d time.Duration) string {
	if d <= time.Second {
		return "1s"
	}
	mins := int(d.Minutes())
	if mins >= 60 {
		hours := mins / 60
		mins = mins % 60
		if mins > 0 {
			return fmt.Sprintf("%dh%dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if mins > 0 {
		secs := int(d.Seconds()) % 60
		if secs > 0 {
			return fmt.Sprintf("%dm%ds", mins, secs)
		}
		return fmt.Sprintf("%dm", mins)
	}
	secs := int(d.Seconds())
	return fmt.Sprintf("%ds", secs)
}
