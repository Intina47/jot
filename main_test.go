package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
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
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	return dir
}

func writeTestPNG(t *testing.T, path string, width int, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	widthDenom := width
	if widthDenom < 1 {
		widthDenom = 1
	}
	heightDenom := height
	if heightDenom < 1 {
		heightDenom = 1
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / widthDenom),
				G: uint8((y * 255) / heightDenom),
				B: 180,
				A: 255,
			})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png failed: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png failed: %v", err)
	}
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
		"convert",
		"list",
		"open",
		"task",
		"write",
		"jot convert logo.png ico",
		"jot task",
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

func TestParseViewerServeArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantPath     string
		wantSelfOpen bool
		wantErr      string
	}{
		{name: "direct viewer launch", args: []string{"note.md"}, wantPath: "note.md", wantSelfOpen: true},
		{name: "parent opens url", args: []string{"--no-self-open", "note.md"}, wantPath: "note.md", wantSelfOpen: false},
		{name: "missing path", args: nil, wantErr: "usage: jot __viewer <path>"},
		{name: "blank path", args: []string{"   "}, wantErr: "path must be provided"},
	}

	for _, tt := range tests {
		gotPath, gotSelfOpen, err := parseViewerServeArgs(tt.args)
		if tt.wantErr != "" {
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("%s: expected error %q, got %v", tt.name, tt.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if gotPath != tt.wantPath {
			t.Fatalf("%s: expected path %q, got %q", tt.name, tt.wantPath, gotPath)
		}
		if gotSelfOpen != tt.wantSelfOpen {
			t.Fatalf("%s: expected selfOpen %v, got %v", tt.name, tt.wantSelfOpen, gotSelfOpen)
		}
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

func TestJotIntegrateHelpWritesCommandGuide(t *testing.T) {
	var out bytes.Buffer
	err := jotIntegrate(&out, nil, runtime.GOOS, os.Executable, runCommand)
	if err != nil {
		t.Fatalf("jotIntegrate returned error: %v", err)
	}
	help := out.String()
	for _, snippet := range []string{
		"jot integrate",
		"jot integrate windows",
		"jot integrate windows --remove",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotConvertHelpWritesCommandGuide(t *testing.T) {
	var out bytes.Buffer
	err := jotConvert(&out, []string{"--help"})
	if err != nil {
		t.Fatalf("jotConvert returned error: %v", err)
	}
	help := out.String()
	for _, snippet := range []string{
		"jot convert",
		"<png|jpg|jpeg|gif|ico|svg>",
		"--out PATH",
		"multi-size favicon-style icon",
		"Raster-to-`.svg` output wraps the source image",
		"Supported raster inputs today",
		"jot convert screenshot.png jpg",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotTaskHelpWritesCommandGuide(t *testing.T) {
	var out bytes.Buffer
	err := jotTask(strings.NewReader(""), &out, []string{"--help"}, mustGetwd)
	if err != nil {
		t.Fatalf("jotTask returned error: %v", err)
	}
	help := out.String()
	for _, snippet := range []string{
		"jot task",
		"jot task convert",
		"jot convert logo.png ico",
		"Discover and run terminal-first tasks",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestParseConvertArgs(t *testing.T) {
	options, err := parseConvertArgs([]string{"logo.png", "ico", "--out", "favicon.ico", "--overwrite"})
	if err != nil {
		t.Fatalf("parseConvertArgs returned error: %v", err)
	}
	if options.SourcePath != "logo.png" || options.TargetFormat != "ico" {
		t.Fatalf("unexpected convert args: %#v", options)
	}
	if options.OutputPath != "favicon.ico" || !options.Overwrite {
		t.Fatalf("unexpected convert flags: %#v", options)
	}
}

func TestConvertImageFileCreatesICO(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "logo.png")
	writeTestPNG(t, sourcePath, 64, 48)

	result, err := convertImageFile(convertOptions{
		SourcePath:   sourcePath,
		TargetFormat: "ico",
	})
	if err != nil {
		t.Fatalf("convertImageFile returned error: %v", err)
	}
	if filepath.Ext(result.OutputPath) != ".ico" {
		t.Fatalf("expected .ico output, got %q", result.OutputPath)
	}
	data, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("read ico failed: %v", err)
	}
	if len(data) < 6 {
		t.Fatalf("expected ico bytes, got %d", len(data))
	}
	if got := binary.LittleEndian.Uint16(data[2:4]); got != 1 {
		t.Fatalf("expected ico type 1, got %d", got)
	}
	if got := binary.LittleEndian.Uint16(data[4:6]); got != 6 {
		t.Fatalf("expected 6 embedded icon sizes, got %d", got)
	}
}

func TestConvertImageFileCreatesEmbeddedSVG(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "logo.png")
	writeTestPNG(t, sourcePath, 32, 20)

	result, err := convertImageFile(convertOptions{
		SourcePath:   sourcePath,
		TargetFormat: "svg",
	})
	if err != nil {
		t.Fatalf("convertImageFile returned error: %v", err)
	}
	if result.Warning == "" {
		t.Fatalf("expected svg conversion warning")
	}
	data, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("read svg failed: %v", err)
	}
	html := string(data)
	for _, snippet := range []string{
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
		"<svg",
		`viewBox="0 0 32 20"`,
		`preserveAspectRatio="xMidYMid meet"`,
		`<title>logo.png</title>`,
		"data:image/png;base64,",
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected embedded svg wrapper to contain %q, got %q", snippet, html)
		}
	}
	if !strings.Contains(html, "logo.png") {
		t.Fatalf("expected embedded svg wrapper, got %q", html)
	}
	encoded := strings.TrimSuffix(strings.SplitN(strings.SplitN(html, "base64,", 2)[1], "\"", 2)[0], "")
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		t.Fatalf("expected decodable embedded image data: %v", err)
	}
}

func TestConvertImageFileCreatesPNG(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "logo.jpg")
	writeTestPNG(t, sourcePath, 40, 24)

	result, err := convertImageFile(convertOptions{
		SourcePath:   sourcePath,
		TargetFormat: "png",
	})
	if err != nil {
		t.Fatalf("convertImageFile returned error: %v", err)
	}
	if filepath.Ext(result.OutputPath) != ".png" {
		t.Fatalf("expected .png output, got %q", result.OutputPath)
	}
	data, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("read png failed: %v", err)
	}
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("expected png signature, got invalid output")
	}
}

func TestConvertImageFileCreatesJPGAndWarnsWhenFlatteningAlpha(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "logo.png")
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			alpha := uint8(255)
			if x > 10 {
				alpha = 128
			}
			img.Set(x, y, color.RGBA{R: 220, G: 80, B: 80, A: alpha})
		}
	}
	file, err := os.Create(sourcePath)
	if err != nil {
		t.Fatalf("create png failed: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png failed: %v", err)
	}

	result, err := convertImageFile(convertOptions{
		SourcePath:   sourcePath,
		TargetFormat: "jpg",
	})
	if err != nil {
		t.Fatalf("convertImageFile returned error: %v", err)
	}
	if filepath.Ext(result.OutputPath) != ".jpg" {
		t.Fatalf("expected .jpg output, got %q", result.OutputPath)
	}
	if result.Warning == "" {
		t.Fatalf("expected jpg conversion warning when flattening alpha")
	}
	data, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("read jpg failed: %v", err)
	}
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 || data[len(data)-2] != 0xff || data[len(data)-1] != 0xd9 {
		t.Fatalf("expected jpeg markers, got invalid output")
	}
}

func TestJotTaskConvertFlowCreatesOutputAndPrintsTip(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "logo.png")
	writeTestPNG(t, sourcePath, 48, 48)

	var out bytes.Buffer
	err := jotTask(strings.NewReader("1\n1\nico\n"), &out, nil, func() string {
		return workdir
	})
	if err != nil {
		t.Fatalf("jotTask returned error: %v", err)
	}

	outputPath := filepath.Join(workdir, "logo.ico")
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected ico output, got err=%v", err)
	}
	text := out.String()
	for _, snippet := range []string{
		"jot task",
		"Select task [1]:",
		"Convert Image",
		"IMAGES IN THIS FOLDER",
		"OUTPUT FORMAT",
		"Select format [ico]:",
		"logo.ico",
		"next time: jot convert logo.png ico",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected task flow output to contain %q, got %q", snippet, text)
		}
	}
	if strings.Contains(text, "Available tasks:") {
		t.Fatalf("expected task flow output, got %q", text)
	}
}

func TestJotOpenWithHandlersReturnsEntryForMatchingID(t *testing.T) {
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
	err = jotOpenWithHandlers(&out, "a1", func(targetURL string) error {
		called = true
		return nil
	}, func(path string) error {
		t.Fatalf("default opener should not be called for jot ids")
		return nil
	}, func() (string, error) {
		t.Fatalf("picker should not be called for jot ids")
		return "", nil
	})
	if err != nil {
		t.Fatalf("jotOpenWithHandlers returned error: %v", err)
	}
	if called {
		t.Fatalf("expected browser opener not to be called for jot ids")
	}
	expected := "[2024-01-01 10:00] first\n"
	if out.String() != expected {
		t.Fatalf("expected output %q, got %q", expected, out.String())
	}
}

func TestOpenLocalPathOpensExistingPDFPath(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	pdfPath := filepath.Join(workdir, "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var gotURL string
	opened, err := openLocalPathWithViewerLauncher(pdfPath, func(targetURL string) error {
		gotURL = targetURL
		return nil
	}, func(path string) error {
		t.Fatalf("default opener should not be called for pdf paths")
		return nil
	}, func(path string, openURL func(string) error) error {
		return openURL("http://127.0.0.1:4321/")
	})
	if err != nil {
		t.Fatalf("openLocalPath returned error: %v", err)
	}
	if !opened {
		t.Fatalf("expected pdf path to be handled")
	}
	if !strings.HasPrefix(gotURL, "http://127.0.0.1:") {
		t.Fatalf("expected localhost browser url, got %q", gotURL)
	}
	if !strings.HasSuffix(gotURL, "/") {
		t.Fatalf("expected viewer url to end with /, got %q", gotURL)
	}
}

func TestOpenLocalPathOpensMarkdownInViewer(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	mdPath := filepath.Join(workdir, "plan.md")
	if err := os.WriteFile(mdPath, []byte("# Plan\n\n- ship viewer\n"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var gotURL string
	opened, err := openLocalPathWithViewerLauncher(mdPath, func(targetURL string) error {
		gotURL = targetURL
		return nil
	}, func(path string) error {
		t.Fatalf("default opener should not be called for markdown paths")
		return nil
	}, func(path string, openURL func(string) error) error {
		return openURL("http://127.0.0.1:4567/")
	})
	if err != nil {
		t.Fatalf("openLocalPath returned error: %v", err)
	}
	if !opened {
		t.Fatalf("expected markdown path to be handled")
	}
	if gotURL != "http://127.0.0.1:4567/" {
		t.Fatalf("expected viewer url %q, got %q", "http://127.0.0.1:4567/", gotURL)
	}
}

func TestOpenLocalPathOpensDirectoryInViewer(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()

	var launchedPath string
	var gotURL string
	opened, err := openLocalPathWithViewerLauncher(workdir, func(targetURL string) error {
		gotURL = targetURL
		return nil
	}, func(path string) error {
		t.Fatalf("default opener should not be called for directory paths")
		return nil
	}, func(path string, openURL func(string) error) error {
		launchedPath = path
		return openURL("http://127.0.0.1:5050/")
	})
	if err != nil {
		t.Fatalf("openLocalPath returned error: %v", err)
	}
	if !opened {
		t.Fatalf("expected directory path to be handled")
	}
	wantPath, err := filepath.Abs(workdir)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if launchedPath != wantPath {
		t.Fatalf("expected viewer launch path %q, got %q", wantPath, launchedPath)
	}
	if gotURL != "http://127.0.0.1:5050/" {
		t.Fatalf("expected viewer url %q, got %q", "http://127.0.0.1:5050/", gotURL)
	}
}

func TestOpenLocalPathOpensUnsupportedFileWithDefaultApp(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "notes.bin")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var openedPath string
	opened, err := openLocalPath(filePath, func(targetURL string) error {
		t.Fatalf("browser opener should not be called")
		return nil
	}, func(path string) error {
		openedPath = path
		return nil
	})
	if err != nil {
		t.Fatalf("openLocalPath returned error: %v", err)
	}
	if !opened {
		t.Fatalf("expected existing path to be recognized")
	}
	wantPath, err := filepath.Abs(filePath)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if openedPath != wantPath {
		t.Fatalf("expected default opener path %q, got %q", wantPath, openedPath)
	}
}

func TestLaunchLocalFileInViewerWithProcessOpensViewerURL(t *testing.T) {
	var gotURL string
	err := launchLocalFileInViewerWithProcess(`C:\Docs\BRTC FAQs_DOC-212001.pdf`, func(targetURL string) error {
		gotURL = targetURL
		return nil
	}, func() (string, error) {
		return `C:\Tools\jot.exe`, nil
	}, func(executablePath string, filePath string) (string, error) {
		if executablePath != `C:\Tools\jot.exe` {
			t.Fatalf("unexpected executable path: %q", executablePath)
		}
		if filePath != `C:\Docs\BRTC FAQs_DOC-212001.pdf` {
			t.Fatalf("unexpected file path: %q", filePath)
		}
		return "http://127.0.0.1:4321/", nil
	})
	if err != nil {
		t.Fatalf("launchLocalFileInViewerWithProcess returned error: %v", err)
	}
	if gotURL != "http://127.0.0.1:4321/" {
		t.Fatalf("expected viewer url %q, got %q", "http://127.0.0.1:4321/", gotURL)
	}
}

func TestPrepareViewerExecutableForLaunchCopiesWindowsBinary(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "jot.exe")
	content := []byte("viewer-binary")
	if err := os.WriteFile(sourcePath, content, 0o700); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	launchPath, cleanupPath, err := prepareViewerExecutableForLaunch(sourcePath, "windows", func() string {
		return workdir
	}, copyFile)
	if err != nil {
		t.Fatalf("prepareViewerExecutableForLaunch returned error: %v", err)
	}
	if launchPath == sourcePath {
		t.Fatalf("expected a temp executable path, got source path %q", launchPath)
	}
	if cleanupPath != launchPath {
		t.Fatalf("expected cleanup path %q, got %q", launchPath, cleanupPath)
	}
	copiedContent, err := os.ReadFile(launchPath)
	if err != nil {
		t.Fatalf("read copied executable failed: %v", err)
	}
	if !bytes.Equal(copiedContent, content) {
		t.Fatalf("expected copied content %q, got %q", string(content), string(copiedContent))
	}
}

func TestPrepareViewerExecutableForLaunchKeepsOriginalOffWindows(t *testing.T) {
	launchPath, cleanupPath, err := prepareViewerExecutableForLaunch("/tmp/jot", "linux", func() string {
		t.Fatalf("tempDir should not be called off windows")
		return ""
	}, func(sourcePath string, destinationPath string) error {
		t.Fatalf("copy should not be called off windows")
		return nil
	})
	if err != nil {
		t.Fatalf("prepareViewerExecutableForLaunch returned error: %v", err)
	}
	if launchPath != "/tmp/jot" {
		t.Fatalf("expected original path, got %q", launchPath)
	}
	if cleanupPath != "" {
		t.Fatalf("expected no cleanup path, got %q", cleanupPath)
	}
}

func TestPDFViewerHandlerServesViewerPageAndDocument(t *testing.T) {
	workdir := t.TempDir()
	pdfPath := filepath.Join(workdir, "BRTC FAQs_DOC-212001.pdf")
	content := []byte("%PDF-1.7")
	if err := os.WriteFile(pdfPath, content, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	touched := 0
	doc, err := loadViewerDocument(pdfPath)
	if err != nil {
		t.Fatalf("loadViewerDocument returned error: %v", err)
	}
	server := httptest.NewServer(newFileViewerHandler(doc, func() {
		touched++
	}))
	defer server.Close()

	viewerResp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("viewer request failed: %v", err)
	}
	defer viewerResp.Body.Close()
	if viewerResp.StatusCode != http.StatusOK {
		t.Fatalf("expected viewer status 200, got %d", viewerResp.StatusCode)
	}
	viewerBody, err := io.ReadAll(viewerResp.Body)
	if err != nil {
		t.Fatalf("read viewer body failed: %v", err)
	}
	if !strings.Contains(string(viewerBody), "jot · BRTC FAQs_DOC-212001.pdf") {
		t.Fatalf("expected viewer html, got %q", string(viewerBody))
	}
	if !strings.Contains(string(viewerBody), "BRTC FAQs_DOC-212001.pdf") {
		t.Fatalf("expected file name in viewer html, got %q", string(viewerBody))
	}
	if !strings.Contains(string(viewerBody), `/document.pdf`) {
		t.Fatalf("expected embedded pdf route, got %q", string(viewerBody))
	}
	if !strings.Contains(string(viewerBody), `/logo.png`) {
		t.Fatalf("expected embedded logo route, got %q", string(viewerBody))
	}

	pdfResp, err := http.Get(server.URL + "/document.pdf")
	if err != nil {
		t.Fatalf("pdf request failed: %v", err)
	}
	defer pdfResp.Body.Close()
	if pdfResp.StatusCode != http.StatusOK {
		t.Fatalf("expected pdf status 200, got %d", pdfResp.StatusCode)
	}
	pdfBody, err := io.ReadAll(pdfResp.Body)
	if err != nil {
		t.Fatalf("read pdf body failed: %v", err)
	}
	if !bytes.Equal(pdfBody, content) {
		t.Fatalf("expected served pdf content %q, got %q", string(content), string(pdfBody))
	}
	logoResp, err := http.Get(server.URL + "/logo.png")
	if err != nil {
		t.Fatalf("logo request failed: %v", err)
	}
	defer logoResp.Body.Close()
	if logoResp.StatusCode != http.StatusOK {
		t.Fatalf("expected logo status 200, got %d", logoResp.StatusCode)
	}
	if got := logoResp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected logo content type image/png, got %q", got)
	}
	logoBody, err := io.ReadAll(logoResp.Body)
	if err != nil {
		t.Fatalf("read logo body failed: %v", err)
	}
	if !bytes.Equal(logoBody, viewerLogoPNG) {
		t.Fatalf("expected served logo bytes to match embedded logo")
	}
	if touched < 2 {
		t.Fatalf("expected handler touch to run at least twice, got %d", touched)
	}
}

func TestFolderViewerHandlerServesFolderPageAndPreviews(t *testing.T) {
	workdir := t.TempDir()
	mdPath := filepath.Join(workdir, "a-plan.md")
	pdfPath := filepath.Join(workdir, "z-paper.pdf")
	txtPath := filepath.Join(workdir, "notes.txt")
	if err := os.WriteFile(mdPath, []byte("# Plan\n\n- ship viewer\n"), 0o600); err != nil {
		t.Fatalf("markdown write failed: %v", err)
	}
	pdfContent := []byte("%PDF-1.7")
	if err := os.WriteFile(pdfPath, pdfContent, 0o600); err != nil {
		t.Fatalf("pdf write failed: %v", err)
	}
	if err := os.WriteFile(txtPath, []byte("alpha\nbeta"), 0o600); err != nil {
		t.Fatalf("txt write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "ignore.bin"), []byte("ignore me"), 0o600); err != nil {
		t.Fatalf("bin write failed: %v", err)
	}

	files, err := scanFolderFiles(workdir)
	if err != nil {
		t.Fatalf("scanFolderFiles returned error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 supported files, got %d", len(files))
	}
	if files[0].Name != "a-plan.md" || files[1].Name != "notes.txt" || files[2].Name != "z-paper.pdf" {
		t.Fatalf("unexpected scanned files: %#v", files)
	}

	server := httptest.NewServer(newFolderViewerHandler(workdir, files, func() {}))
	defer server.Close()

	viewerResp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("folder viewer request failed: %v", err)
	}
	defer viewerResp.Body.Close()
	if viewerResp.StatusCode != http.StatusOK {
		t.Fatalf("expected folder viewer status 200, got %d", viewerResp.StatusCode)
	}
	viewerBody, err := io.ReadAll(viewerResp.Body)
	if err != nil {
		t.Fatalf("read folder viewer body failed: %v", err)
	}
	viewerHTML := string(viewerBody)
	if !strings.Contains(viewerHTML, "a-plan.md") || !strings.Contains(viewerHTML, "notes.txt") || !strings.Contains(viewerHTML, "z-paper.pdf") {
		t.Fatalf("expected folder file list in viewer html, got %q", viewerHTML)
	}
	if strings.Contains(viewerHTML, "ignore.bin") {
		t.Fatalf("expected unsupported files to be omitted, got %q", viewerHTML)
	}

	mdResp, err := http.Get(server.URL + "/file?i=0")
	if err != nil {
		t.Fatalf("folder markdown request failed: %v", err)
	}
	defer mdResp.Body.Close()
	if mdResp.StatusCode != http.StatusOK {
		t.Fatalf("expected markdown preview status 200, got %d", mdResp.StatusCode)
	}
	mdBody, err := io.ReadAll(mdResp.Body)
	if err != nil {
		t.Fatalf("read markdown preview body failed: %v", err)
	}
	if !strings.Contains(string(mdBody), "<h1") || !strings.Contains(string(mdBody), "Plan") {
		t.Fatalf("expected markdown preview html, got %q", string(mdBody))
	}

	txtResp, err := http.Get(server.URL + "/file?i=1")
	if err != nil {
		t.Fatalf("folder text request failed: %v", err)
	}
	defer txtResp.Body.Close()
	if txtResp.StatusCode != http.StatusOK {
		t.Fatalf("expected folder text status 200, got %d", txtResp.StatusCode)
	}
	txtBody, err := io.ReadAll(txtResp.Body)
	if err != nil {
		t.Fatalf("read folder text body failed: %v", err)
	}
	if !strings.Contains(string(txtBody), `class="code-frame"`) || !strings.Contains(string(txtBody), "alpha") {
		t.Fatalf("expected text preview html, got %q", string(txtBody))
	}

	pdfResp, err := http.Get(server.URL + "/pdf?i=2")
	if err != nil {
		t.Fatalf("folder pdf request failed: %v", err)
	}
	defer pdfResp.Body.Close()
	if pdfResp.StatusCode != http.StatusOK {
		t.Fatalf("expected folder pdf status 200, got %d", pdfResp.StatusCode)
	}
	pdfBody, err := io.ReadAll(pdfResp.Body)
	if err != nil {
		t.Fatalf("read folder pdf body failed: %v", err)
	}
	if !bytes.Equal(pdfBody, pdfContent) {
		t.Fatalf("expected served pdf content %q, got %q", string(pdfContent), string(pdfBody))
	}
}

func TestMarkdownViewerHandlerRendersHTMLPreview(t *testing.T) {
	workdir := t.TempDir()
	mdPath := filepath.Join(workdir, "plan.md")
	content := strings.Join([]string{
		"# Launch Plan",
		"",
		"**Sunday, March 1, 2026**",
		"",
		"Generated:",
		"8:00 AM",
		"",
		"*Status: escalating*",
		"",
		"1. first scenario",
		"2. second scenario",
		"",
		"- ship viewer",
		"- route markdown",
		"",
		"`jot open` and [source](https://example.com)",
		"",
	}, "\n")
	if err := os.WriteFile(mdPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	doc, err := loadViewerDocument(mdPath)
	if err != nil {
		t.Fatalf("loadViewerDocument returned error: %v", err)
	}
	server := httptest.NewServer(newFileViewerHandler(doc, func() {}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("viewer request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read viewer body failed: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `>Launch Plan</h1>`) {
		t.Fatalf("expected heading render, got %q", html)
	}
	if !strings.Contains(html, "<strong>Sunday, March 1, 2026</strong>") {
		t.Fatalf("expected bold render, got %q", html)
	}
	if !strings.Contains(html, "<p>Generated: 8:00 AM</p>") {
		t.Fatalf("expected wrapped paragraph lines to join with spaces, got %q", html)
	}
	if !strings.Contains(html, "<em>Status: escalating</em>") {
		t.Fatalf("expected italic render, got %q", html)
	}
	if !strings.Contains(html, "<ol><li>first scenario</li><li>second scenario</li></ol>") {
		t.Fatalf("expected ordered list render, got %q", html)
	}
	if !strings.Contains(html, "<li>ship viewer</li>") {
		t.Fatalf("expected list render, got %q", html)
	}
	if !strings.Contains(html, "<code>jot open</code>") {
		t.Fatalf("expected inline code render, got %q", html)
	}
	if !strings.Contains(html, `<a href="https://example.com" target="_blank" rel="noreferrer">source</a>`) {
		t.Fatalf("expected link render, got %q", html)
	}
}

func TestRenderMarkdownHTMLSupportsNestedDocumentBlocks(t *testing.T) {
	content := strings.Join([]string{
		"| Name | Status |",
		"| :--- | ---: |",
		"| jot | ready |",
		"",
		"- parent item",
		"  - child item",
		"",
		"- [ ] open task",
		"- [x] closed task",
		"",
		"Ship ~~legacy~~ viewer",
		"",
		"> outer quote",
		"> > inner quote",
		"",
		"Roses are red",
		"Violets are blue",
	}, "\n")

	html := renderMarkdownHTML(content)

	if !strings.Contains(html, `<div class="table-wrap"><table><thead><tr><th style="text-align:left">Name</th><th style="text-align:right">Status</th></tr></thead><tbody><tr><td style="text-align:left">jot</td><td style="text-align:right">ready</td></tr></tbody></table></div>`) {
		t.Fatalf("expected themed table render, got %q", html)
	}
	if !strings.Contains(html, "<ul><li>parent item<ul><li>child item</li></ul></li>") {
		t.Fatalf("expected nested list render, got %q", html)
	}
	if !strings.Contains(html, `class="task-list-item"`) || !strings.Contains(html, `class="task-checkbox" type="checkbox" disabled`) {
		t.Fatalf("expected task checkbox render, got %q", html)
	}
	if !strings.Contains(html, "<del>legacy</del>") {
		t.Fatalf("expected strikethrough render, got %q", html)
	}
	if !strings.Contains(html, "<blockquote><p>outer quote</p><blockquote><p>inner quote</p></blockquote></blockquote>") {
		t.Fatalf("expected nested blockquote render, got %q", html)
	}
	if !strings.Contains(html, "<p>Roses are red Violets are blue</p>") {
		t.Fatalf("expected non-hard-broken lines to join with spaces, got %q", html)
	}
}

func TestRenderMarkdownHTMLKeepsListChildrenInsideListItem(t *testing.T) {
	t.Run("continuation paragraph", func(t *testing.T) {
		content := strings.Join([]string{
			"- This list item has a continuation paragraph.",
			"",
			"  The continuation is indented to align with the list content. It should",
			"  render as part of the same `<li>`, not as a new paragraph outside the list.",
			"",
			"- This is the next item, back to normal.",
		}, "\n")

		html := renderMarkdownHTML(content)

		assertContainsInOrder(t, html,
			"<li>",
			"This list item has a continuation paragraph.",
			"The continuation is indented to align with the list content.",
			"</li>",
		)
	})

	t.Run("nested blockquote", func(t *testing.T) {
		content := strings.Join([]string{
			"- A list item that references a quote:",
			"",
			"  > This is a blockquote nested inside a list item continuation.",
			"  > It should render with the blockquote styling indented under the bullet.",
		}, "\n")

		html := renderMarkdownHTML(content)

		assertContainsInOrder(t, html,
			"<li>",
			"<blockquote>",
			"This is a blockquote nested inside a list item continuation.",
			"</blockquote>",
			"</li>",
		)
	})

	t.Run("fenced code block", func(t *testing.T) {
		content := strings.Join([]string{
			"- Build the project:",
			"",
			"  ```shell",
			"  go build -o jot ./...",
			"  ```",
		}, "\n")

		html := renderMarkdownHTML(content)

		assertContainsInOrder(t, html,
			"<li>",
			"<pre data-lang=\"shell\"><code>",
			"go build -o jot ./...",
			"</code></pre>",
			"</li>",
		)
	})
}

func TestRenderMarkdownHTMLHandlesInlineEdgeCasesFromSample(t *testing.T) {
	t.Run("combined emphasis", func(t *testing.T) {
		html := renderMarkdownHTML("***bold and italic combined***")
		if !strings.Contains(html, "<p><strong><em>bold and italic combined</em></strong></p>") {
			t.Fatalf("expected combined emphasis render, got %q", html)
		}
	})

	t.Run("escaped markers", func(t *testing.T) {
		html := renderMarkdownHTML("Escaped characters: \\*not italic\\*, \\`not code\\`, \\~\\~not strikethrough\\~\\~.")
		if !strings.Contains(html, `Escaped characters: *not italic*, `+"`not code`"+`, ~~not strikethrough~~.`) {
			t.Fatalf("expected escaped markers to render literally, got %q", html)
		}
		if strings.Contains(html, "<em>not italic</em>") || strings.Contains(html, "<code>not code</code>") || strings.Contains(html, "<del>not strikethrough</del>") {
			t.Fatalf("expected literal escaped markers, got %q", html)
		}
	})

	t.Run("double backtick code spans", func(t *testing.T) {
		html := renderMarkdownHTML("Use ``a|b`` and ``x`` in prose.")
		if !strings.Contains(html, "<p>Use <code>a|b</code> and <code>x</code> in prose.</p>") {
			t.Fatalf("expected double-backtick code spans, got %q", html)
		}
	})

	t.Run("internal hash link", func(t *testing.T) {
		html := renderMarkdownHTML("See [Mixed and Nested](#mixed-and-nested) for the full example.")
		if !strings.Contains(html, `<a href="#mixed-and-nested">Mixed and Nested</a>`) {
			t.Fatalf("expected internal hash link render, got %q", html)
		}
	})
}

func TestRenderMarkdownHTMLHandlesSampleTableEdgeCases(t *testing.T) {
	t.Run("escaped and literal pipes", func(t *testing.T) {
		content := strings.Join([]string{
			"| Expression | Meaning | Example |",
			"|------------|---------|---------|",
			"| `a|b`      | Pipe inside code span | `0b1010|0b0101` |",
			"| `a\\|b`    | Escaped pipe | `x\\|y` |",
			"| \\|        | Literal pipe | \\| |",
		}, "\n")

		html := renderMarkdownHTML(content)

		if !strings.Contains(html, `<div class="table-wrap"><table>`) {
			t.Fatalf("expected table wrapper, got %q", html)
		}
		if !strings.Contains(html, `<code>a|b</code>`) || !strings.Contains(html, `<code>0b1010|0b0101</code>`) {
			t.Fatalf("expected pipes inside code spans to stay intact, got %q", html)
		}
		if !strings.Contains(html, `<code>a\|b</code>`) || !strings.Contains(html, `<code>x\|y</code>`) {
			t.Fatalf("expected escaped pipes inside code spans to stay literal, got %q", html)
		}
		if !strings.Contains(html, `<td>|</td>`) {
			t.Fatalf("expected literal pipe cell render, got %q", html)
		}
	})

	t.Run("single column alignment", func(t *testing.T) {
		content := strings.Join([]string{
			"| Right oriented rendering |",
			"|--------------------------:|",
			"| 150.0                     |",
			"| or text                   |",
		}, "\n")

		html := renderMarkdownHTML(content)

		if !strings.Contains(html, `<th style="text-align:right">Right oriented rendering</th>`) {
			t.Fatalf("expected right-aligned header cell, got %q", html)
		}
		if !strings.Contains(html, `<td style="text-align:right">150.0</td>`) {
			t.Fatalf("expected right-aligned data cell, got %q", html)
		}
	})

	t.Run("single column centered alignment", func(t *testing.T) {
		content := strings.Join([]string{
			"| Centered rendering |",
			"|:------------------:|",
			"| 150.0              |",
			"| or text            |",
		}, "\n")

		html := renderMarkdownHTML(content)

		if !strings.Contains(html, `<th style="text-align:center">Centered rendering</th>`) {
			t.Fatalf("expected centered header cell, got %q", html)
		}
		if !strings.Contains(html, `<td style="text-align:center">150.0</td>`) {
			t.Fatalf("expected centered data cell, got %q", html)
		}
	})
}

func TestSampleMarkdownRendersSentinelFragments(t *testing.T) {
	content, err := os.ReadFile("sample.md")
	if err != nil {
		t.Fatalf("read sample.md failed: %v", err)
	}

	html := renderMarkdownHTML(string(content))

	sentinels := []string{
		`<h2 id="blockquotes">Blockquotes</h2>`,
		`<h2 id="lists">Lists</h2>`,
		`<h2 id="tables">Tables</h2>`,
		`<div class="table-wrap"><table>`,
		`class="task-checkbox"`,
		`<pre data-lang="go"><code>`,
		`<a href="#mixed-and-nested">Mixed and Nested</a>`,
	}
	for _, sentinel := range sentinels {
		if !strings.Contains(html, sentinel) {
			t.Fatalf("expected sample.md render to contain %q, got %q", sentinel, html)
		}
	}
}

func assertContainsInOrder(t *testing.T, text string, fragments ...string) {
	t.Helper()

	offset := 0
	for _, fragment := range fragments {
		idx := strings.Index(text[offset:], fragment)
		if idx < 0 {
			t.Fatalf("expected %q after offset %d in %q", fragment, offset, text)
		}
		offset += idx + len(fragment)
	}
}

func TestStructuredViewerFormatsJSONAndXML(t *testing.T) {
	workdir := t.TempDir()
	jsonPath := filepath.Join(workdir, "sample.json")
	xmlPath := filepath.Join(workdir, "sample.xml")
	if err := os.WriteFile(jsonPath, []byte(`{"project":"jot","viewer":true}`), 0o600); err != nil {
		t.Fatalf("json write failed: %v", err)
	}
	if err := os.WriteFile(xmlPath, []byte("<root><project>jot</project></root>"), 0o600); err != nil {
		t.Fatalf("xml write failed: %v", err)
	}

	jsonDoc, err := loadViewerDocument(jsonPath)
	if err != nil {
		t.Fatalf("loadViewerDocument json returned error: %v", err)
	}
	if !strings.Contains(jsonDoc.content, "\n  \"project\": \"jot\"") {
		t.Fatalf("expected pretty-printed json, got %q", jsonDoc.content)
	}
	jsonServer := httptest.NewServer(newFileViewerHandler(jsonDoc, func() {}))
	defer jsonServer.Close()

	jsonResp, err := http.Get(jsonServer.URL + "/")
	if err != nil {
		t.Fatalf("json viewer request failed: %v", err)
	}
	defer jsonResp.Body.Close()
	jsonBody, err := io.ReadAll(jsonResp.Body)
	if err != nil {
		t.Fatalf("read json viewer body failed: %v", err)
	}
	jsonHTML := string(jsonBody)
	if strings.Contains(jsonHTML, "%!(") {
		t.Fatalf("expected no fmt formatting artifact, got %q", jsonHTML)
	}
	if !strings.Contains(jsonHTML, `class="toc-panel"`) {
		t.Fatalf("expected json toc shell, got %q", jsonHTML)
	}
	if !strings.Contains(jsonHTML, `id="viewer-source"`) {
		t.Fatalf("expected json viewer source payload, got %q", jsonHTML)
	}
	if strings.Contains(jsonHTML, `if (!trigger) return;`) {
		t.Fatalf("expected no early toc abort in shared script, got %q", jsonHTML)
	}
	if !strings.Contains(jsonHTML, `function readViewerSource()`) {
		t.Fatalf("expected shared viewer source reader, got %q", jsonHTML)
	}
	if !strings.Contains(jsonHTML, `buildJSONTree(JSON.parse(jsonRaw), jsonRoot)`) {
		t.Fatalf("expected deferred json bootstrap, got %q", jsonHTML)
	}

	xmlDoc, err := loadViewerDocument(xmlPath)
	if err != nil {
		t.Fatalf("loadViewerDocument xml returned error: %v", err)
	}
	server := httptest.NewServer(newFileViewerHandler(xmlDoc, func() {}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("viewer request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read viewer body failed: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `id="viewer-source"`) {
		t.Fatalf("expected xml viewer source payload, got %q", html)
	}
	if !strings.Contains(html, "XML preview") {
		t.Fatalf("expected xml hint, got %q", html)
	}
	if !strings.Contains(html, `var xmlRaw = readViewerSource();`) {
		t.Fatalf("expected shared xml source reader, got %q", html)
	}
	if !strings.Contains(html, `buildXMLTree(xmlRoot)`) {
		t.Fatalf("expected deferred xml bootstrap, got %q", html)
	}
}

func TestViewerDocumentTypeForPathSupportsAdditionalFormats(t *testing.T) {
	cases := map[string]viewerDocumentType{
		"compose.yaml":  viewerDocumentTypeYAML,
		"compose.yml":   viewerDocumentTypeYAML,
		"Cargo.toml":    viewerDocumentTypeTOML,
		"report.csv":    viewerDocumentTypeCSV,
		".env":          viewerDocumentTypeEnv,
		".env.local":    viewerDocumentTypeEnv,
		"notes.txt":     viewerDocumentTypeText,
		"journal.jsonl": viewerDocumentTypeText,
	}
	for path, want := range cases {
		if got := viewerDocumentTypeForPath(path); got != want {
			t.Fatalf("viewerDocumentTypeForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestStructuredViewerFormatsYAMLAndTOML(t *testing.T) {
	workdir := t.TempDir()
	yamlPath := filepath.Join(workdir, "docker-compose.yaml")
	tomlPath := filepath.Join(workdir, "Cargo.toml")

	yamlContent := strings.Join([]string{
		"services:",
		"  web:",
		"    image: nginx",
		"    ports:",
		"      - \"80:80\"",
		"      - \"443:443\"",
	}, "\n")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("yaml write failed: %v", err)
	}
	tomlContent := strings.Join([]string{
		`title = "jot"`,
		`[server]`,
		`port = 8080`,
		`hosts = ["localhost", "127.0.0.1"]`,
		`[[plugins]]`,
		`name = "markdown"`,
	}, "\n")
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0o600); err != nil {
		t.Fatalf("toml write failed: %v", err)
	}

	yamlDoc, err := loadViewerDocument(yamlPath)
	if err != nil {
		t.Fatalf("loadViewerDocument yaml returned error: %v", err)
	}
	if yamlDoc.docType != viewerDocumentTypeYAML {
		t.Fatalf("expected yaml doc type, got %q", yamlDoc.docType)
	}
	if !strings.Contains(yamlDoc.structuredContent, `"services"`) || !strings.Contains(yamlDoc.structuredContent, `"ports"`) {
		t.Fatalf("expected yaml structured payload, got %q", yamlDoc.structuredContent)
	}
	yamlServer := httptest.NewServer(newFileViewerHandler(yamlDoc, func() {}))
	defer yamlServer.Close()
	yamlResp, err := http.Get(yamlServer.URL + "/")
	if err != nil {
		t.Fatalf("yaml viewer request failed: %v", err)
	}
	defer yamlResp.Body.Close()
	yamlBody, err := io.ReadAll(yamlResp.Body)
	if err != nil {
		t.Fatalf("read yaml viewer body failed: %v", err)
	}
	yamlHTML := string(yamlBody)
	if !strings.Contains(yamlHTML, `Top-level fields`) || !strings.Contains(yamlHTML, `id="json-root"`) {
		t.Fatalf("expected yaml structured viewer shell, got %q", yamlHTML)
	}

	tomlDoc, err := loadViewerDocument(tomlPath)
	if err != nil {
		t.Fatalf("loadViewerDocument toml returned error: %v", err)
	}
	if tomlDoc.docType != viewerDocumentTypeTOML {
		t.Fatalf("expected toml doc type, got %q", tomlDoc.docType)
	}
	if !strings.Contains(tomlDoc.structuredContent, `"server"`) || !strings.Contains(tomlDoc.structuredContent, `"plugins"`) {
		t.Fatalf("expected toml structured payload, got %q", tomlDoc.structuredContent)
	}
}

func TestCSVViewerHandlerRendersThemedTablePreview(t *testing.T) {
	workdir := t.TempDir()
	csvPath := filepath.Join(workdir, "report.csv")
	content := strings.Join([]string{
		`name,city,notes`,
		`alice,"New York, NY","ships weekly"`,
		`bob,London,"supports commas, too"`,
	}, "\n")
	if err := os.WriteFile(csvPath, []byte(content), 0o600); err != nil {
		t.Fatalf("csv write failed: %v", err)
	}

	doc, err := loadViewerDocument(csvPath)
	if err != nil {
		t.Fatalf("loadViewerDocument csv returned error: %v", err)
	}
	if doc.csvTable == nil {
		t.Fatalf("expected parsed csv table")
	}
	server := httptest.NewServer(newFileViewerHandler(doc, func() {}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("csv viewer request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read csv viewer body failed: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `csv-frame`) || !strings.Contains(html, `class="table-wrap"`) {
		t.Fatalf("expected csv themed table, got %q", html)
	}
	if !strings.Contains(html, "New York, NY") || !strings.Contains(html, "supports commas, too") {
		t.Fatalf("expected parsed csv cells, got %q", html)
	}
}

func TestEnvViewerHandlerMasksValues(t *testing.T) {
	workdir := t.TempDir()
	envPath := filepath.Join(workdir, ".env")
	content := strings.Join([]string{
		"# local secrets",
		"API_KEY=super-secret-token",
		`export DATABASE_URL="postgres://user:pass@example.com/db?sslmode=disable"`,
		"JWT=abc=123=xyz",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("env write failed: %v", err)
	}

	doc, err := loadViewerDocument(envPath)
	if err != nil {
		t.Fatalf("loadViewerDocument env returned error: %v", err)
	}
	server := httptest.NewServer(newFileViewerHandler(doc, func() {}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("env viewer request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read env viewer body failed: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `env-frame`) || !strings.Contains(html, `tok-env-key`) {
		t.Fatalf("expected env tokenized view, got %q", html)
	}
	if strings.Contains(html, "super-secret-token") || strings.Contains(html, "postgres://user:pass@example.com/db?sslmode=disable") {
		t.Fatalf("expected raw env values to stay masked, got %q", html)
	}
	if !strings.Contains(html, "API_KEY") || !strings.Contains(html, "DATABASE_URL") || !strings.Contains(html, "JWT") {
		t.Fatalf("expected env keys in html, got %q", html)
	}
	if !strings.Contains(html, "Values are masked before rendering.") {
		t.Fatalf("expected env masking note, got %q", html)
	}
}

func TestTextViewerHandlerPreservesRawLines(t *testing.T) {
	workdir := t.TempDir()
	txtPath := filepath.Join(workdir, "notes.txt")
	content := "alpha\n  beta\n<config>\n"
	if err := os.WriteFile(txtPath, []byte(content), 0o600); err != nil {
		t.Fatalf("txt write failed: %v", err)
	}

	doc, err := loadViewerDocument(txtPath)
	if err != nil {
		t.Fatalf("loadViewerDocument txt returned error: %v", err)
	}
	server := httptest.NewServer(newFileViewerHandler(doc, func() {}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("txt viewer request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read txt viewer body failed: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `class="code-frame"`) || !strings.Contains(html, `<td class="ln">1</td>`) {
		t.Fatalf("expected text code-frame with line numbers, got %q", html)
	}
	if !strings.Contains(html, "&lt;config&gt;") {
		t.Fatalf("expected text preview to escape html, got %q", html)
	}
}

func TestViewerWindowCommandPrefersKnownWindowsBrowserPath(t *testing.T) {
	targetURL := "http://127.0.0.1:4321/"
	spec, ok := viewerWindowCommand(targetURL, "windows", func(key string) string {
		switch key {
		case "ProgramFiles(x86)":
			return `C:\Program Files (x86)`
		case "ProgramFiles":
			return `C:\Program Files`
		case "LocalAppData":
			return `C:\Users\mamba\AppData\Local`
		default:
			return ""
		}
	}, func(name string) (string, error) {
		t.Fatalf("lookPath should not be needed when a known browser path exists; got %q", name)
		return "", nil
	}, func(path string) bool {
		return path == `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
	})
	if !ok {
		t.Fatalf("expected a viewer window command")
	}
	if spec.name != `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe` {
		t.Fatalf("unexpected browser path: %q", spec.name)
	}
	wantArgs := []string{"--app=" + targetURL}
	if !reflect.DeepEqual(spec.args, wantArgs) {
		t.Fatalf("expected args %v, got %v", wantArgs, spec.args)
	}
}

func TestViewerWindowCommandFallsBackToLookPath(t *testing.T) {
	targetURL := "http://127.0.0.1:9876/"
	spec, ok := viewerWindowCommand(targetURL, "linux", func(string) string {
		return ""
	}, func(name string) (string, error) {
		if name == "google-chrome" {
			return "/usr/bin/google-chrome", nil
		}
		return "", os.ErrNotExist
	}, func(path string) bool {
		return false
	})
	if !ok {
		t.Fatalf("expected a viewer window command from lookPath")
	}
	if spec.name != "/usr/bin/google-chrome" {
		t.Fatalf("unexpected browser path: %q", spec.name)
	}
	wantArgs := []string{"--app=" + targetURL}
	if !reflect.DeepEqual(spec.args, wantArgs) {
		t.Fatalf("expected args %v, got %v", wantArgs, spec.args)
	}
}

func TestJotIntegrateWindowsInstallsContextMenu(t *testing.T) {
	var calls [][]string
	var out bytes.Buffer
	err := jotIntegrateWindows(&out, nil, "windows", func() (string, error) {
		return `C:\Tools\jot.exe`, nil
	}, func(name string, args ...string) error {
		call := append([]string{name}, args...)
		calls = append(calls, call)
		return nil
	})
	if err != nil {
		t.Fatalf("jotIntegrateWindows returned error: %v", err)
	}
	if len(calls) != 5 {
		t.Fatalf("expected 5 registry calls, got %d", len(calls))
	}
	expectedCalls := [][]string{
		{"reg", "add", `HKCU\Software\Classes\*\shell\jot`, "/ve", "/d", "Open with jot", "/f"},
		{"reg", "add", `HKCU\Software\Classes\*\shell\jot`, "/v", "Icon", "/t", "REG_SZ", "/d", `C:\Tools\jot.exe,0`, "/f"},
		{"reg", "add", `HKCU\Software\Classes\*\shell\jot`, "/v", "MUIVerb", "/t", "REG_SZ", "/d", "Open with jot", "/f"},
		{"reg", "delete", `HKCU\Software\Classes\*\shell\jot`, "/v", "Extended", "/f"},
		{"reg", "add", `HKCU\Software\Classes\*\shell\jot\command`, "/ve", "/t", "REG_SZ", "/d", `"C:\Tools\jot.exe" __viewer "%1"`, "/f"},
	}
	if !reflect.DeepEqual(calls, expectedCalls) {
		t.Fatalf("expected registry calls %v, got %v", expectedCalls, calls)
	}
	if !strings.Contains(out.String(), `installed Explorer "Open with jot" integration`) {
		t.Fatalf("expected install message, got %q", out.String())
	}
}

func TestJotIntegrateWindowsRemovesContextMenu(t *testing.T) {
	var calls [][]string
	var out bytes.Buffer
	err := jotIntegrateWindows(&out, []string{"--remove"}, "windows", func() (string, error) {
		return `C:\Tools\jot.exe`, nil
	}, func(name string, args ...string) error {
		call := append([]string{name}, args...)
		calls = append(calls, call)
		return nil
	})
	if err != nil {
		t.Fatalf("jotIntegrateWindows returned error: %v", err)
	}
	expected := []string{"reg", "delete", `HKCU\Software\Classes\*\shell\jot`, "/f"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], expected) {
		t.Fatalf("expected remove call %v, got %v", expected, calls)
	}
	if !strings.Contains(out.String(), `removed Explorer "Open with jot" integration`) {
		t.Fatalf("expected remove message, got %q", out.String())
	}
}

func TestJotIntegrateWindowsRejectsNonWindows(t *testing.T) {
	err := jotIntegrateWindows(&bytes.Buffer{}, nil, "linux", func() (string, error) {
		return `C:\Tools\jot.exe`, nil
	}, func(name string, args ...string) error {
		t.Fatalf("runner should not be called")
		return nil
	})
	if err == nil {
		t.Fatalf("expected non-windows error")
	}
	if !strings.Contains(err.Error(), "only be installed from Windows") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJotOpenWithHandlersUsesPickerWhenTargetEmpty(t *testing.T) {
	withTempHome(t)
	workdir := t.TempDir()
	filePath := filepath.Join(workdir, "notes.bin")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var openedPath string
	err := jotOpenWithHandlers(&bytes.Buffer{}, "", func(targetURL string) error {
		t.Fatalf("browser opener should not be called")
		return nil
	}, func(path string) error {
		openedPath = path
		return nil
	}, func() (string, error) {
		return filePath, nil
	})
	if err != nil {
		t.Fatalf("jotOpenWithHandlers returned error: %v", err)
	}
	wantPath, err := filepath.Abs(filePath)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if openedPath != wantPath {
		t.Fatalf("expected picked path %q, got %q", wantPath, openedPath)
	}
}

func TestJotOpenWithHandlersReturnsNilWhenPickerCancelled(t *testing.T) {
	withTempHome(t)
	err := jotOpenWithHandlers(&bytes.Buffer{}, "", func(targetURL string) error {
		t.Fatalf("browser opener should not be called")
		return nil
	}, func(path string) error {
		t.Fatalf("default opener should not be called")
		return nil
	}, func() (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("expected nil error on picker cancel, got %v", err)
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
