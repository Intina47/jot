package main

import (
	"bytes"
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

func TestJotSearchFiltersByTag(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := strings.Join([]string{
		"[2024-01-01 10:00] incident in prod #prod project:myrepo",
		"[2024-01-02 11:00] incident in dev #dev project:other",
	}, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	opts := searchOptions{tags: []string{"prod"}}
	if err := jotSearch(&out, "incident", opts); err != nil {
		t.Fatalf("jotSearch returned error: %v", err)
	}

	expected := "[2024-01-01 10:00] incident in prod #prod project:myrepo\n"
	if out.String() != expected {
		t.Fatalf("expected %q, got %q", expected, out.String())
	}
}

func TestJotSearchFiltersByDate(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := strings.Join([]string{
		"[2024-01-01 10:00] incident in prod #prod",
		"[2024-02-01 10:00] incident follow-up #prod",
	}, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	since := dateOnly(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))
	until := dateOnly(time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC))
	opts := searchOptions{since: &since, until: &until}
	if err := jotSearch(&out, "incident", opts); err != nil {
		t.Fatalf("jotSearch returned error: %v", err)
	}

	expected := "[2024-02-01 10:00] incident follow-up #prod\n"
	if out.String() != expected {
		t.Fatalf("expected %q, got %q", expected, out.String())
	}
}

func TestJotSearchFiltersByProject(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := strings.Join([]string{
		"[2024-03-01 09:00] incident notes project:myrepo #prod",
		"[2024-03-02 09:00] incident notes repo:other #prod",
	}, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	opts := searchOptions{project: "myrepo"}
	if err := jotSearch(&out, "incident", opts); err != nil {
		t.Fatalf("jotSearch returned error: %v", err)
	}

	expected := "[2024-03-01 09:00] incident notes project:myrepo #prod\n"
	if out.String() != expected {
		t.Fatalf("expected %q, got %q", expected, out.String())
	}
}
