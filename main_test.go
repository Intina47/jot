package main

import (
	"bytes"
	"encoding/json"
	"os"
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

func TestParseLinkURLGitHubPull(t *testing.T) {
	info, err := parseLinkURL("https://github.com/org/repo/pull/123")
	if err != nil {
		t.Fatalf("parseLinkURL returned error: %v", err)
	}

	if info.Host != "github.com" {
		t.Fatalf("expected host github.com, got %q", info.Host)
	}
	if info.Owner != "org" || info.Repo != "repo" {
		t.Fatalf("expected owner/org repo, got %q/%q", info.Owner, info.Repo)
	}
	if info.Kind != "pull" || info.Number != 123 {
		t.Fatalf("expected pull 123, got kind=%q number=%d", info.Kind, info.Number)
	}
}

func TestParseLinkURLRejectsScheme(t *testing.T) {
	if _, err := parseLinkURL("ftp://example.com/resource"); err == nil {
		t.Fatalf("expected error for unsupported scheme")
	}
}

func TestJotLinkStoresMetadata(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	entry := "[2024-02-03 04:05] hello\n"
	if err := os.WriteFile(journalPath, []byte(entry), 0o600); err != nil {
		t.Fatalf("write journal failed: %v", err)
	}

	fixedNow := func() time.Time {
		return time.Date(2024, 2, 4, 5, 6, 7, 0, time.FixedZone("Z", 0))
	}

	if err := jotLink("https://github.com/org/repo/pull/123", fixedNow); err != nil {
		t.Fatalf("jotLink returned error: %v", err)
	}

	data, err := os.ReadFile(linksPath(home))
	if err != nil {
		t.Fatalf("read links failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 link entry, got %d", len(lines))
	}

	var entryData LinkMetadata
	if err := json.Unmarshal([]byte(lines[0]), &entryData); err != nil {
		t.Fatalf("unmarshal link entry failed: %v", err)
	}

	if entryData.NoteTimestamp != "2024-02-03 04:05" {
		t.Fatalf("expected note timestamp, got %q", entryData.NoteTimestamp)
	}
	if entryData.URL != "https://github.com/org/repo/pull/123" {
		t.Fatalf("expected url, got %q", entryData.URL)
	}
	if entryData.Kind != "pull" || entryData.Number != 123 {
		t.Fatalf("expected pull metadata, got kind=%q number=%d", entryData.Kind, entryData.Number)
	}
	if entryData.AddedAt != fixedNow().UTC().Format(time.RFC3339) {
		t.Fatalf("expected added_at %q, got %q", fixedNow().UTC().Format(time.RFC3339), entryData.AddedAt)
	}
}
