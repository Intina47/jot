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

func TestLoadTemplatesIncludesCustom(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	customDir, err := templateDir()
	if err != nil {
		t.Fatalf("templateDir returned error: %v", err)
	}
	if err := os.MkdirAll(customDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "daily.md"), []byte("custom"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates returned error: %v", err)
	}
	if templates["daily"] != "custom" {
		t.Fatalf("expected custom template override, got %q", templates["daily"])
	}
}

func TestRenderTemplate(t *testing.T) {
	fixed := time.Date(2024, 2, 3, 4, 5, 0, 0, time.FixedZone("Z", 0))
	content := "{{date}} {{time}} {{datetime}} {{repo}}"
	result := renderTemplate(content, fixed, "jot")
	if result != "2024-02-03 04:05 2024-02-03 04:05 jot" {
		t.Fatalf("unexpected render result: %q", result)
	}
}

func TestJotNewDoesNotOverwriteExistingNote(t *testing.T) {
	workdir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	})
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	fixedNow := func() time.Time {
		return time.Date(2024, 2, 3, 4, 5, 0, 0, time.FixedZone("Z", 0))
	}
	filename := filepath.Join(workdir, "2024-02-03-daily.md")
	if err := os.WriteFile(filename, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	err = jotNew(&out, fixedNow, []string{"--template", "daily"})
	if err == nil {
		t.Fatalf("expected error when note exists")
	}
	if !strings.Contains(err.Error(), "note already exists") {
		t.Fatalf("expected already exists error, got %v", err)
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(content) != "existing" {
		t.Fatalf("expected existing note to remain unchanged, got %q", string(content))
	}
}

func TestJotNewWithNameCreatesNamedNote(t *testing.T) {
	workdir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	})
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	fixedNow := func() time.Time {
		return time.Date(2024, 2, 3, 4, 5, 0, 0, time.FixedZone("Z", 0))
	}

	var out bytes.Buffer
	if err := jotNew(&out, fixedNow, []string{"--template", "meeting", "-n", "Team Sync-Up"}); err != nil {
		t.Fatalf("jotNew returned error: %v", err)
	}

	expected := filepath.Join(workdir, "2024-02-03-meeting-team-sync-up.md")
	if strings.TrimSpace(out.String()) != expected {
		t.Fatalf("expected output %q, got %q", expected, strings.TrimSpace(out.String()))
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestSlugifyName(t *testing.T) {
	if slug := slugifyName(" Team Sync-Up "); slug != "team-sync-up" {
		t.Fatalf("unexpected slug: %q", slug)
	}
	if slug := slugifyName("###"); slug != "" {
		t.Fatalf("expected empty slug, got %q", slug)
	}
}
