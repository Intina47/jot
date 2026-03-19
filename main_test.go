package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

	journalDir, _, journalPath := journalPaths(home)
	if _, err := os.Stat(journalDir); !os.IsNotExist(err) {
		t.Fatalf("expected no journal dir, got err=%v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("expected no journal file, got err=%v", err)
	}
}

func TestEnsureJournalCreatesDirAndFile(t *testing.T) {
	home := withTempHome(t)

	journalPath, err := ensureJournalJSONL()
	if err != nil {
		t.Fatalf("ensureJournalJSONL returned error: %v", err)
	}

	journalDir, _, expectedPath := journalPaths(home)
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
	journalDir, _, journalPath := journalPaths(home)

	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	entries := []journalEntry{
		{
			ID:        "a1",
			CreatedAt: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			Content:   "first",
		},
		{
			ID:        "a2",
			CreatedAt: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
			Content:   "second",
		},
	}
	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			_ = file.Close()
			t.Fatalf("write failed: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotList(&out, false); err != nil {
		t.Fatalf("jotList returned error: %v", err)
	}
	expected := "[2024-01-01 10:00] first\n[2024-01-01 11:00] second\n"
	if out.String() != expected {
		t.Fatalf("expected output %q, got %q", expected, out.String())
	}
}

func TestAnnotateListItemLinesDoesNotShowIDs(t *testing.T) {
	item := listItem{
		id: "dg0aa9b7itc0-55",
		lines: []string{
			"[2026-01-28 14:15] Dear readers, here is what we want to do",
			"second line",
		},
	}

	got := annotateListItemLines(item, item.lines)
	want := []string{
		"[2026-01-28 14:15] Dear readers, here is what we want to do",
		"second line",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected lines %v, got %v", want, got)
	}
}

func TestPreviewListLinesKeepsOpenHintForTruncatedEntries(t *testing.T) {
	item := listItem{
		id: "dg0ftbuoqqdc-62",
		lines: []string{
			"[2026-01-28 14:15] Dear readers, here is what we want to do",
			"line two",
			"line three",
		},
	}

	got := previewListLines(item, 2)
	want := []string{
		"[2026-01-28 14:15] Dear readers, here is what we want to do",
		"line two",
		"\x1b[92m… (jot open dg0ftbuoqqdc-62)\x1b[0m",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected lines %v, got %v", want, got)
	}
}

func TestRenderHelpMainIncludesCommands(t *testing.T) {
	help, err := renderHelp("", false)
	if err != nil {
		t.Fatalf("renderHelp returned error: %v", err)
	}
	for _, snippet := range []string{
		"jot " + version,
		"jot help [command]",
		"capture",
		"list",
		"open",
		"jot list --full",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
	if strings.Contains(help, "\x1b[") {
		t.Fatalf("expected plain help output without ANSI escapes, got %q", help)
	}
}

func TestRenderHelpColorAddsANSI(t *testing.T) {
	help, err := renderHelp("capture", true)
	if err != nil {
		t.Fatalf("renderHelp returned error: %v", err)
	}
	if !strings.Contains(help, "\x1b[") {
		t.Fatalf("expected ANSI color escapes in help output, got %q", help)
	}
	if !strings.Contains(help, "jot capture") {
		t.Fatalf("expected capture help content, got %q", help)
	}
}

func TestJotNewHelpWritesCommandGuide(t *testing.T) {
	var out bytes.Buffer
	err := jotNew(&out, time.Now, []string{"--help"})
	if err != nil {
		t.Fatalf("jotNew returned error: %v", err)
	}
	help := out.String()
	for _, snippet := range []string{
		"jot new",
		"--template NAME",
		"--name TEXT, -n TEXT",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotCaptureHelpWritesCommandGuide(t *testing.T) {
	var out bytes.Buffer
	err := jotCapture(&out, []string{"--help"}, time.Now, launchEditor)
	if err != nil {
		t.Fatalf("jotCapture returned error: %v", err)
	}
	help := out.String()
	for _, snippet := range []string{
		"jot capture",
		"--title TITLE",
		"--project PROJECT",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotOpenWithBrowserOpenerReturnsEntryForMatchingID(t *testing.T) {
	home := withTempHome(t)
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

	journalDir, _, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	entry := journalEntry{
		ID:        "a1",
		CreatedAt: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Content:   "first",
	}
	if err := encoder.Encode(entry); err != nil {
		_ = file.Close()
		t.Fatalf("encode failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	called := false
	var out bytes.Buffer
	err = jotOpenWithBrowserOpener(&out, "a1", func(targetURL string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("jotOpenWithBrowserOpener returned error: %v", err)
	}
	if called {
		t.Fatalf("expected browser opener not to be called for jot ids")
	}
	expected := "[2024-01-01 10:00] first\n"
	if out.String() != expected {
		t.Fatalf("expected output %q, got %q", expected, out.String())
	}
}

func TestJotOpenWithBrowserOpenerOpensExistingPDFPath(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	pdfPath := filepath.Join(workdir, "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var gotURL string
	var out bytes.Buffer
	opened, err := openLocalPDFInBrowserWithLauncher(pdfPath, func(targetURL string) error {
		gotURL = targetURL
		return nil
	}, func(path string, openURL func(string) error) error {
		return openURL("http://127.0.0.1:4321/paper.pdf")
	})
	if err != nil {
		t.Fatalf("openLocalPDFInBrowserWithLauncher returned error: %v", err)
	}
	if !opened {
		t.Fatalf("expected pdf path to be handled")
	}
	wantURL := "http://127.0.0.1:4321/paper.pdf"
	if gotURL != wantURL {
		t.Fatalf("expected URL %q, got %q", wantURL, gotURL)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}

func TestJotOpenWithBrowserOpenerRejectsNonPDFPath(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	txtPath := filepath.Join(workdir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	opened, err := openLocalPDFInBrowserWithLauncher(txtPath, func(targetURL string) error {
		t.Fatalf("browser opener should not be called")
		return nil
	}, func(path string, openURL func(string) error) error {
		t.Fatalf("pdf launcher should not be called")
		return nil
	})
	if err == nil {
		t.Fatalf("expected error for non-pdf path")
	}
	if !opened {
		t.Fatalf("expected existing path to be recognized")
	}
	if !strings.Contains(err.Error(), "is not a PDF file") {
		t.Fatalf("expected non-pdf error, got %v", err)
	}
}

func TestLaunchLocalPDFInBrowserServesSpacedFilename(t *testing.T) {
	workdir := t.TempDir()
	pdfPath := filepath.Join(workdir, "BRTC FAQs_DOC-212001.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var browserURL string
	err := launchLocalPDFInBrowser(pdfPath, func(targetURL string) error {
		browserURL = targetURL
		resp, err := http.Get(targetURL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("launchLocalPDFInBrowser returned error: %v", err)
	}
	if !strings.Contains(browserURL, "BRTC%20FAQs_DOC-212001.pdf") {
		t.Fatalf("expected escaped browser url, got %q", browserURL)
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

	_, _, journalPath := journalPaths(home)
	entries, err := loadJournalEntries(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.ID != newEntryID(fixedNow(), 0) {
		t.Fatalf("expected id %q, got %q", newEntryID(fixedNow(), 0), entry.ID)
	}
	if !entry.CreatedAt.Equal(fixedNow()) {
		t.Fatalf("expected created_at %v, got %v", fixedNow(), entry.CreatedAt)
	}
	if entry.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", entry.Content)
	}
	if entry.Source != "prompt" {
		t.Fatalf("expected source %q, got %q", "prompt", entry.Source)
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

	_, _, journalPath := journalPaths(home)
	entries, err := loadJournalEntries(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.ID != newEntryID(fixedNow(), 0) {
		t.Fatalf("expected id %q, got %q", newEntryID(fixedNow(), 0), entry.ID)
	}
	if !entry.CreatedAt.Equal(fixedNow()) {
		t.Fatalf("expected created_at %v, got %v", fixedNow(), entry.CreatedAt)
	}
	if entry.Title != "title" {
		t.Fatalf("expected title %q, got %q", "title", entry.Title)
	}
	if entry.Content != "note" {
		t.Fatalf("expected content %q, got %q", "note", entry.Content)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "foo" {
		t.Fatalf("expected tags %v, got %v", []string{"foo"}, entry.Tags)
	}
	if entry.Source != "capture" {
		t.Fatalf("expected source %q, got %q", "capture", entry.Source)
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

	_, _, journalPath := journalPaths(home)
	entries, err := loadJournalEntries(journalPath)
	if err != nil {
		t.Fatalf("read journal failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.ID != newEntryID(fixedNow(), 0) {
		t.Fatalf("expected id %q, got %q", newEntryID(fixedNow(), 0), entry.ID)
	}
	if !entry.CreatedAt.Equal(fixedNow()) {
		t.Fatalf("expected created_at %v, got %v", fixedNow(), entry.CreatedAt)
	}
	if entry.Title != "note" {
		t.Fatalf("expected title %q, got %q", "note", entry.Title)
	}
	if entry.Content != "from editor" {
		t.Fatalf("expected content %q, got %q", "from editor", entry.Content)
	}
	if entry.Source != "editor" {
		t.Fatalf("expected source %q, got %q", "editor", entry.Source)
	}
}
