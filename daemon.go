package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const assistantDaemonFileName = "assistant_daemon.json"

type AssistantDaemonState struct {
	PID           int       `json:"pid,omitempty"`
	Running       bool      `json:"running,omitempty"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	LastHeartbeat time.Time `json:"lastHeartbeat,omitempty"`
	LastWorkAt    time.Time `json:"lastWorkAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	FeedPath      string    `json:"feedPath,omitempty"`
}

type daemonLaunchResult struct {
	PID int
}

type daemonLoopSnapshot struct {
	Config     AssistantConfig
	Now        time.Time
	Iteration  int
	PID        int
	StatePath  string
	FeedPath   string
	MemoryPath string
}

type daemonPaths struct {
	dir       string
	statePath string
	logPath   string
}

var (
	daemonLaunchWorker      = defaultDaemonLaunchWorker
	daemonKillProcess       = defaultDaemonKillProcess
	daemonProactiveWorkHook = defaultDaemonProactiveWorkHook
)

func (s AssistantDaemonState) isFresh(now time.Time) bool {
	if s.LastHeartbeat.IsZero() || now.IsZero() {
		return false
	}
	return now.UTC().Sub(s.LastHeartbeat.UTC()) <= 2*time.Minute
}

func jotDaemon(stdin io.Reader, stdout io.Writer, args []string, now func() time.Time) error {
	_ = stdin
	if now == nil {
		now = time.Now
	}
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch action {
	case "", "status":
		return jotDaemonStatus(stdout, now)
	case "start":
		return jotDaemonStart(stdout, now)
	case "run":
		once := len(args) > 1 && strings.TrimSpace(args[1]) == "--once"
		return jotDaemonRun(stdout, now, once)
	case "worker":
		return jotDaemonRun(stdout, now, false)
	case "stop":
		return jotDaemonStop(stdout)
	default:
		return fmt.Errorf("usage: jot daemon <start|run|status|stop>")
	}
}

func jotDaemonStart(stdout io.Writer, now func() time.Time) error {
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	state, exists, _ := readDaemonState(paths.statePath)
	if exists && state.Running && state.PID > 0 && state.isFresh(now()) {
		_, _ = fmt.Fprintf(stdout, "daemon already running (pid %d)\n", state.PID)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	launch, err := daemonLaunchWorker(exe, []string{"daemon", "worker"}, paths.logPath)
	if err != nil {
		return err
	}
	state = AssistantDaemonState{
		PID:           launch.PID,
		Running:       true,
		StartedAt:     now().UTC(),
		LastHeartbeat: now().UTC(),
		FeedPath:      mustLoadAssistantConfig().FeedPath,
	}
	if err := writeSecureJSON(paths.statePath, state); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "daemon started (pid %d)\n", launch.PID)
	return nil
}

func jotDaemonRun(stdout io.Writer, now func() time.Time, once bool) error {
	cfg := mustLoadAssistantConfig()
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if once {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		ran := false
		err := daemonBackgroundLoop(ctx, paths.statePath, os.Getpid(), time.Minute, now, func(loopCtx context.Context, snapshot daemonLoopSnapshot) error {
			snapshot.Config = cfg
			if !ran {
				ran = true
				defer cancel()
			}
			return daemonProactiveWorkHook(loopCtx, snapshot)
		})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, "daemon worker running")
	}
	return daemonBackgroundLoop(ctx, paths.statePath, os.Getpid(), time.Minute, now, func(loopCtx context.Context, snapshot daemonLoopSnapshot) error {
		snapshot.Config = cfg
		return daemonProactiveWorkHook(loopCtx, snapshot)
	})
}

func jotDaemonStatus(stdout io.Writer, now func() time.Time) error {
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	state, exists, err := readDaemonState(paths.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || !exists {
			_, _ = fmt.Fprintln(stdout, "daemon not running")
			return nil
		}
		return err
	}
	running := state.Running && state.PID > 0 && state.isFresh(now())
	if !running {
		_, _ = fmt.Fprintln(stdout, "daemon not running")
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "daemon running (pid %d)\n", state.PID)
	if !state.LastHeartbeat.IsZero() {
		_, _ = fmt.Fprintf(stdout, "last heartbeat: %s\n", state.LastHeartbeat.Format(time.RFC3339))
	}
	if !state.LastWorkAt.IsZero() {
		_, _ = fmt.Fprintf(stdout, "last proactive work: %s\n", state.LastWorkAt.Format(time.RFC3339))
	}
	if strings.TrimSpace(state.LastError) != "" {
		_, _ = fmt.Fprintf(stdout, "last error: %s\n", state.LastError)
	}
	return nil
}

func jotDaemonStop(stdout io.Writer) error {
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	state, exists, err := readDaemonState(paths.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || !exists {
			_, _ = fmt.Fprintln(stdout, "daemon not running")
			return nil
		}
		return err
	}
	if state.PID > 0 {
		_ = daemonKillProcess(state.PID)
	}
	_ = os.Remove(paths.statePath)
	_, _ = fmt.Fprintln(stdout, "daemon stopped")
	return nil
}

func renderDaemonHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot daemon", "Run the local background loop that prepares proactive assistant work.")
	writeUsageSection(&b, style, []string{
		"jot daemon status",
		"jot daemon start",
		"jot daemon stop",
		"jot daemon run",
	}, []string{
		"`jot daemon start` launches the background worker.",
		"`jot daemon run` stays in the foreground and is useful for development.",
		"`jot daemon status` shows heartbeat and last proactive work time.",
	})
	return b.String()
}

func defaultDaemonProactiveWorkHook(ctx context.Context, snapshot daemonLoopSnapshot) error {
	cfg := snapshot.Config
	if strings.TrimSpace(cfg.MemoryPath) == "" || strings.TrimSpace(cfg.FeedPath) == "" {
		cfg = mustLoadAssistantConfig()
	}
	feed, err := LoadAssistantFeed(cfg)
	if err != nil {
		return err
	}
	memory, err := LoadAssistantMemory(cfg)
	if err != nil {
		return err
	}
	web := NewWebCapability()
	watchers := []func(context.Context, daemonLoopSnapshot) ([]AssistantFeedItem, error){
		daemonWatchGmail,
		daemonWatchCalendar,
		daemonWatchJournal,
		daemonWatchLocalMachine,
		daemonWatchTerminalProcesses,
	}
	for _, watcher := range watchers {
		items, err := watcher(ctx, snapshot)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := feed.AddItem(item); err != nil {
				return err
			}
		}
	}
	if events, err := LoadProcessEvents(cfg.ProcessEventsPath); err == nil && len(events) > 0 {
		signals := BuildProcessEventSignals(events, snapshot.Now)
		for _, observation := range signals.Observations {
			if _, err := memory.AddObservation(observation); err != nil {
				return err
			}
		}
		for _, inference := range signals.Inferences {
			if _, err := memory.AddInference(inference); err != nil {
				return err
			}
		}
		for _, item := range signals.Feed {
			if _, err := feed.AddItem(item); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	for _, fact := range memory.BestFacts() {
		item, ok := assistantProactiveFeedItemForFact(fact, snapshot.Now, web.Client)
		if !ok {
			continue
		}
		if _, err := feed.AddItem(item); err != nil {
			return err
		}
	}
	return nil
}

func assistantProactiveFeedItemForFact(fact MemoryFact, now time.Time, client *http.Client) (AssistantFeedItem, bool) {
	if fact.ID == "" {
		return AssistantFeedItem{}, false
	}
	bucket := fact.Bucket
	switch bucket {
	case MemoryBucketScheduled:
		if fact.EffectiveStart.IsZero() || fact.EffectiveStart.Before(now.Add(-24*time.Hour)) || fact.EffectiveStart.After(now.Add(14*24*time.Hour)) {
			return AssistantFeedItem{}, false
		}
	case MemoryBucketActive:
		if !fact.EffectiveEnd.IsZero() && fact.EffectiveEnd.Before(now) {
			return AssistantFeedItem{}, false
		}
	default:
		return AssistantFeedItem{}, false
	}
	title := assistantProactiveTitleForFact(fact, now)
	body := assistantMemoryRecallLine(fact)
	if title == "" || body == "" {
		return AssistantFeedItem{}, false
	}
	item := AssistantFeedItem{
		ID:         "memory-" + fact.ID,
		Key:        "memory-" + fact.ID,
		Kind:       AssistantFeedKindResearchBrief,
		Status:     AssistantFeedStatusNew,
		Title:      title,
		Summary:    body,
		Body:       body,
		Reason:     assistantProactiveNoteForFact(fact, now),
		SourceType: "memory",
		MemoryRefs: []string{fact.ID},
		Confidence: fact.Confidence,
		Importance: fact.Importance,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if fact.Bucket == MemoryBucketScheduled {
		item.DueAt = fact.EffectiveStart
	}
	if !fact.EffectiveEnd.IsZero() {
		item.ExpiresAt = fact.EffectiveEnd
	}
	if query := assistantProactiveResearchQuery(fact); query != "" {
		if results, err := assistantWebSearch("https://www.bing.com", client, "jot-assistant/1.0", query, 2); err == nil {
			for _, result := range results {
				item.Links = append(item.Links, AssistantFeedLink{
					Label:   result.Title,
					URL:     result.URL,
					Preview: assistantDefaultString(result.Snippet, assistantDefaultString(result.Source, "web")),
				})
			}
		}
	}
	return item, true
}

func assistantProactiveTitleForFact(fact MemoryFact, now time.Time) string {
	label := assistantDefaultString(strings.TrimSpace(fact.Summary), strings.TrimSpace(fact.Value))
	if label == "" {
		label = strings.TrimSpace(fact.Key)
	}
	switch fact.Bucket {
	case MemoryBucketScheduled:
		return "Upcoming: " + label
	case MemoryBucketActive:
		return "In progress: " + label
	default:
		return label
	}
}

func assistantProactiveNoteForFact(fact MemoryFact, now time.Time) string {
	if fact.Bucket == MemoryBucketScheduled && !fact.EffectiveStart.IsZero() {
		return "starts " + fact.EffectiveStart.Local().Format("Mon Jan 2")
	}
	if fact.Bucket == MemoryBucketActive && !fact.EffectiveEnd.IsZero() {
		return "active until " + fact.EffectiveEnd.Local().Format("Mon Jan 2")
	}
	if !fact.ObservedAt.IsZero() {
		return "reinforced " + fact.ObservedAt.Local().Format("Mon Jan 2")
	}
	return ""
}

func assistantProactiveResearchQuery(fact MemoryFact) string {
	base := strings.TrimSpace(fact.Value)
	if base == "" {
		base = strings.TrimSpace(fact.Summary)
	}
	if base == "" {
		return ""
	}
	switch fact.Kind {
	case MemoryKindSituation:
		return base + " checklist schedule preparation"
	case MemoryKindProject:
		return base + " setup guide best practices"
	default:
		return ""
	}
}

func assistantDaemonStatePath() (string, error) {
	paths, err := daemonRuntimePaths()
	if err != nil {
		return "", err
	}
	return paths.statePath, nil
}

func assistantDaemonLogPath() (string, error) {
	paths, err := daemonRuntimePaths()
	if err != nil {
		return "", err
	}
	return paths.logPath, nil
}

func loadAssistantDaemonState() (AssistantDaemonState, error) {
	path, err := assistantDaemonStatePath()
	if err != nil {
		return AssistantDaemonState{}, err
	}
	state, _, err := readDaemonState(path)
	if err != nil {
		return AssistantDaemonState{}, err
	}
	return state, nil
}

func saveAssistantDaemonState(state AssistantDaemonState) error {
	path, err := assistantDaemonStatePath()
	if err != nil {
		return err
	}
	return writeSecureJSON(path, state)
}

func daemonRuntimePaths() (daemonPaths, error) {
	dir, err := assistantConfigDir()
	if err != nil {
		return daemonPaths{}, err
	}
	base := filepath.Join(dir, "daemon")
	return daemonPaths{
		dir:       base,
		statePath: filepath.Join(base, assistantDaemonFileName),
		logPath:   filepath.Join(base, "daemon.log"),
	}, nil
}

func readDaemonState(path string) (AssistantDaemonState, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return AssistantDaemonState{}, false, os.ErrNotExist
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AssistantDaemonState{}, false, os.ErrNotExist
		}
		return AssistantDaemonState{}, false, err
	}
	var state AssistantDaemonState
	if err := loadJSONFile(path, &state); err != nil {
		return AssistantDaemonState{}, false, err
	}
	return state, true, nil
}

func daemonBackgroundLoop(ctx context.Context, statePath string, pid int, interval time.Duration, now func() time.Time, hook func(context.Context, daemonLoopSnapshot) error) error {
	cfg := mustLoadAssistantConfig()
	if now == nil {
		now = time.Now
	}
	if interval <= 0 {
		interval = time.Minute
	}
	iteration := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current := now().UTC()
		snapshot := daemonLoopSnapshot{
			Config:     cfg,
			Now:        current,
			Iteration:  iteration,
			PID:        pid,
			StatePath:  statePath,
			FeedPath:   cfg.FeedPath,
			MemoryPath: cfg.MemoryPath,
		}
		state := AssistantDaemonState{
			PID:           pid,
			Running:       true,
			StartedAt:     current,
			LastHeartbeat: current,
			FeedPath:      cfg.FeedPath,
		}
		if prior, exists, err := readDaemonState(statePath); err == nil && exists {
			state.StartedAt = prior.StartedAt
			if state.StartedAt.IsZero() {
				state.StartedAt = current
			}
			state.LastWorkAt = prior.LastWorkAt
			state.LastError = prior.LastError
		}
		if hook != nil {
			if err := hook(ctx, snapshot); err != nil {
				state.LastError = err.Error()
			} else {
				state.LastError = ""
				state.LastWorkAt = current
			}
		}
		if err := writeSecureJSON(statePath, state); err != nil {
			return err
		}
		iteration++
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func defaultDaemonLaunchWorker(exe string, args []string, logPath string) (daemonLaunchResult, error) {
	cmd := exec.Command(exe, args...)
	if strings.TrimSpace(logPath) != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
			if file, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); openErr == nil {
				cmd.Stdout = file
				cmd.Stderr = file
			}
		}
	}
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	}
	if err := cmd.Start(); err != nil {
		return daemonLaunchResult{}, err
	}
	return daemonLaunchResult{PID: cmd.Process.Pid}, nil
}

func defaultDaemonKillProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func daemonProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil || process == nil {
		return false
	}
	if runtime.GOOS == "windows" {
		err = process.Signal(syscall.Signal(0))
		return err == nil
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func mustLoadAssistantConfig() AssistantConfig {
	cfg, err := LoadAssistantConfig(AssistantConfigOverrides{})
	if err != nil {
		return AssistantConfig{}
	}
	return cfg
}
