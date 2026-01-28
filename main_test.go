package main

import (
	"bytes"
	"os"
	"reflect"
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

func TestParseCaptureArgsWithContent(t *testing.T) {
	options, err := parseCaptureArgs([]string{"hello", "world", "--title", "greeting", "--tag", "foo", "--tag", "bar", "--project", "alpha", "--repo", "jot"})
	if err != nil {
		t.Fatalf("parseCaptureArgs returned error: %v", err)
	}

	if options.Editor {
		t.Fatalf("expected editor false")
	}
	if options.Content != "hello world" {
		t.Fatalf("expected content %q, got %q", "hello world", options.Content)
	}
	if options.Title != "greeting" {
		t.Fatalf("expected title %q, got %q", "greeting", options.Title)
	}
	if len(options.Tags) != 2 || options.Tags[0] != "foo" || options.Tags[1] != "bar" {
		t.Fatalf("expected tags %v, got %v", []string{"foo", "bar"}, options.Tags)
	}
	if options.Project != "alpha" {
		t.Fatalf("expected project %q, got %q", "alpha", options.Project)
	}
	if options.Repo != "jot" {
		t.Fatalf("expected repo %q, got %q", "jot", options.Repo)
	}
}

func TestParseCaptureArgsWithEditor(t *testing.T) {
	options, err := parseCaptureArgs([]string{"--title", "greeting"})
	if err != nil {
		t.Fatalf("parseCaptureArgs returned error: %v", err)
	}
	if !options.Editor {
		t.Fatalf("expected editor true")
	}
	if options.Title != "greeting" {
		t.Fatalf("expected title %q, got %q", "greeting", options.Title)
	}
}

func TestSplitEditorCommand(t *testing.T) {
	cases := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{input: "code --wait", want: []string{"code", "--wait"}},
		{input: "\"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code\" --wait", want: []string{"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code", "--wait"}},
		{input: "'/path with spaces/editor' -f", want: []string{"/path with spaces/editor", "-f"}},
		{input: "C:\\\\Tools\\\\vim.exe -f", want: []string{"C:\\Tools\\vim.exe", "-f"}},
		{input: "\"C:\\\\Program Files\\\\Editor\\\\editor.exe\" --wait", want: []string{"C:\\Program Files\\Editor\\editor.exe", "--wait"}},
		{input: "\"unterminated", wantErr: true},
	}

	for _, tc := range cases {
		got, err := splitEditorCommand(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("splitEditorCommand(%q) returned error: %v", tc.input, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("splitEditorCommand(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestJotCaptureStoresMetadata(t *testing.T) {
	home := withTempHome(t)

	fixedNow := func() time.Time {
		return time.Date(2024, 3, 10, 9, 30, 0, 0, time.FixedZone("Z", 0))
	}

	if err := jotCapture(&bytes.Buffer{}, []string{"note", "--title", "title", "--tag", "foo"}, fixedNow, launchEditor); err != nil {
		t.Fatalf("jotCapture returned error: %v", err)
	}

	_, journalPath := journalPaths(home)
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}

	expectedEntry := "[2024-03-10 09:30] title — note (tags: foo)\n"
	if string(data) != expectedEntry {
		t.Fatalf("expected entry %q, got %q", expectedEntry, string(data))
	}
}

func TestJotCaptureUsesEditor(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("EDITOR", "test-editor")

	launcherCalled := false
	launcher := func(editor, path string) error {
		launcherCalled = true
		if editor != "test-editor" {
			t.Fatalf("expected editor %q, got %q", "test-editor", editor)
		}
		return os.WriteFile(path, []byte("from editor"), 0o600)
	}

	fixedNow := func() time.Time {
		return time.Date(2024, 3, 11, 8, 0, 0, 0, time.FixedZone("Z", 0))
	}

	if err := jotCapture(&bytes.Buffer{}, []string{"--title", "note"}, fixedNow, launcher); err != nil {
		t.Fatalf("jotCapture returned error: %v", err)
	}
	if !launcherCalled {
		t.Fatalf("expected launcher to be called")
	}

	_, journalPath := journalPaths(home)
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}

	expectedEntry := "[2024-03-11 08:00] note — from editor\n"
	if string(data) != expectedEntry {
		t.Fatalf("expected entry %q, got %q", expectedEntry, string(data))
	}
}
