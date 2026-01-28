package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func withTempHome(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestJotInitIgnoresEmptyInput(t *testing.T) {
	home := withTempHome(t)

	var out bytes.Buffer
	if err := jotInit(strings.NewReader("   \n"), &out, time.Now); err != nil {
		t.Fatalf("jotInit returned error: %v", err)
	}

	journalDir, journalPath := journalPaths(home)
	if _, err := os.Stat(journalDir); !os.IsNotExist(err) {
		t.Fatalf("expected no journal dir, got err=%v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("expected no journal file, got err=%v", err)
	}
}

func TestEnsureJournalCreatesDirAndFile(t *testing.T) {
	home := withTempHome(t)

	journalPath, err := ensureJournal()
	if err != nil {
		t.Fatalf("ensureJournal returned error: %v", err)
	}

	journalDir, expectedPath := journalPaths(home)
	if journalPath != expectedPath {
		t.Fatalf("expected journal path %q, got %q", expectedPath, journalPath)
	}

	dirInfo, err := os.Stat(journalDir)
	if err != nil {
		t.Fatalf("journal dir missing: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("journal dir is not a directory")
	}

	fileInfo, err := os.Stat(journalPath)
	if err != nil {
		t.Fatalf("journal file missing: %v", err)
	}
	if fileInfo.IsDir() {
		t.Fatalf("journal path is a directory, expected file")
	}

	if runtime.GOOS != "windows" {
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("expected dir permissions 0700, got %v", dirInfo.Mode().Perm())
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("expected file permissions 0600, got %v", fileInfo.Mode().Perm())
		}
	}
}

func TestJotListStreamsFile(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := "[2024-01-01 10:00] first\n[2024-01-01 11:00] second\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotList(&out); err != nil {
		t.Fatalf("jotList returned error: %v", err)
	}
	if out.String() != content {
		t.Fatalf("expected output %q, got %q", content, out.String())
	}
}

func TestJotInitAppendsWithTimestamp(t *testing.T) {
	home := withTempHome(t)

	fixedNow := func() time.Time {
		return time.Date(2024, 2, 3, 4, 5, 0, 0, time.FixedZone("Z", 0))
	}

	var out bytes.Buffer
	if err := jotInit(strings.NewReader("hello\n"), &out, fixedNow); err != nil {
		t.Fatalf("jotInit returned error: %v", err)
	}

	expectedPrompt := "jot › what’s on your mind? "
	if out.String() != expectedPrompt {
		t.Fatalf("expected prompt %q, got %q", expectedPrompt, out.String())
	}

	_, journalPath := journalPaths(home)
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}

	expectedEntry := "[2024-02-03 04:05] hello\n"
	if string(data) != expectedEntry {
		t.Fatalf("expected entry %q, got %q", expectedEntry, string(data))
	}
}

func TestSearchNotesRanksPartialMatches(t *testing.T) {
	notes := []note{
		{Line: 1, Text: "quiet note about resilience"},
		{Line: 2, Text: "loneliness isn't social, it's unseen"},
		{Line: 3, Text: "random thought"},
	}

	matches := searchNotes(notes, "lone")
	if len(matches) == 0 {
		t.Fatalf("expected matches, got none")
	}
	if matches[0].Note.Line != 2 {
		t.Fatalf("expected best match line 2, got line %d", matches[0].Note.Line)
	}
}

func TestJotSwitchSearchAndOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script editor not supported on windows")
	}

	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := strings.Join([]string{
		"[2024-01-01 10:00] first idea #alpha",
		"[2024-01-02 09:00] lonely note #beta",
	}, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tempDir := t.TempDir()
	editorPath := filepath.Join(tempDir, "vim")
	outPath := filepath.Join(tempDir, "args.txt")
	script := "#!/bin/sh\nprintf '%s' \"$@\" > " + outPath + "\n"
	if err := os.WriteFile(editorPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write editor failed: %v", err)
	}
	t.Setenv("JOT_EDITOR", editorPath)

	input := "lon\n1\n"
	var out bytes.Buffer
	if err := jotSwitch(strings.NewReader(input), &out); err != nil {
		t.Fatalf("jotSwitch returned error: %v", err)
	}

	if !strings.Contains(out.String(), "1) [2024-01-02 09:00] lonely note #beta") {
		t.Fatalf("expected search results to include lonely note, got %q", out.String())
	}

	args, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	expectedArgs := "+2" + journalPath
	if string(args) != expectedArgs {
		t.Fatalf("expected editor args %q, got %q", expectedArgs, string(args))
	}
}
