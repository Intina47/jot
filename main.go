package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const version = "1.5.3"

func main() {
	_ = version

	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "help" || isHelpFlag(args[0])) {
		if err := writeHelp(os.Stdout, ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "help" {
		if len(args) > 2 {
			if err := writeHelp(os.Stderr, ""); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		topic := ""
		if len(args) == 2 {
			topic = args[1]
		}
		if err := writeHelp(os.Stdout, topic); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(args) == 0 || (len(args) == 1 && args[0] == "init") {
		if err := jotInit(os.Stdin, os.Stdout, time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "init" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "init"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if err := writeHelp(os.Stderr, "init"); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}

	if len(args) >= 1 && args[0] == "new" {
		if err := jotNew(os.Stdout, time.Now, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "list" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "list"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if len(args) == 2 && (args[1] == "templates" || args[1] == "--templates" || args[1] == "-t") {
			if err := jotTemplates(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		full := false
		if len(args) == 2 && (args[1] == "--full" || args[1] == "-f") {
			full = true
		} else if len(args) != 1 {
			if err := writeHelp(os.Stderr, "list"); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		if err := jotList(os.Stdout, full); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "patterns" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "patterns"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if len(args) != 1 {
			if err := writeHelp(os.Stderr, "patterns"); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "patterns are coming. keep noticing.")
		return
	}

	if len(args) >= 1 && args[0] == "templates" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "templates"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if len(args) != 1 {
			if err := writeHelp(os.Stderr, "templates"); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		if err := jotTemplates(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "open" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "open"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if len(args) > 2 {
			if err := writeHelp(os.Stderr, "open"); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		target := ""
		if len(args) == 2 {
			target = strings.TrimSpace(args[1])
		}
		if err := jotOpen(os.Stdout, target); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "integrate" {
		if err := jotIntegrate(os.Stdout, args[1:], runtime.GOOS, os.Executable, runCommand); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "__viewer" {
		if err := jotServeViewer(os.Stdout, args[1:], time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "capture" {
		if err := jotCapture(os.Stdout, args[1:], time.Now, launchEditor); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := writeHelp(os.Stderr, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}

type helpStyler struct {
	color bool
}

func (s helpStyler) wrap(code, text string) string {
	if !s.color {
		return text
	}
	return code + text + "\x1b[0m"
}

func (s helpStyler) title(text string) string {
	return s.wrap("\x1b[1;96m", text)
}

func (s helpStyler) section(text string) string {
	return s.wrap("\x1b[1;36m", text)
}

func (s helpStyler) command(text string) string {
	return s.wrap("\x1b[1;32m", text)
}

func (s helpStyler) example(text string) string {
	return s.wrap("\x1b[33m", text)
}

func (s helpStyler) dim(text string) string {
	return s.wrap("\x1b[90m", text)
}

func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help"
}

func writeHelp(w io.Writer, topic string) error {
	text, err := renderHelp(topic, isTTY(w))
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, text)
	return err
}

func renderHelp(topic string, color bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "", "help":
		return renderMainHelp(color), nil
	case "init":
		return renderInitHelp(color), nil
	case "capture":
		return renderCaptureHelp(color), nil
	case "integrate":
		return renderIntegrateHelp(color), nil
	case "list":
		return renderListHelp(color), nil
	case "new":
		return renderNewHelp(color), nil
	case "open":
		return renderOpenHelp(color), nil
	case "templates":
		return renderTemplatesHelp(color), nil
	case "patterns":
		return renderPatternsHelp(color), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", topic)
	}
}

func renderMainHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, fmt.Sprintf("jot %s", version), "Fast capture, local notes, and journal browsing that stays close to the terminal.")
	writeUsageSection(&b, style, []string{
		"jot",
		"jot <command> [options]",
		"jot help [command]",
	}, []string{
		"`jot` and `jot init` start the quick prompt flow.",
	})
	writeCommandSection(&b, style, []helpCommand{
		{name: "init", description: "Open the quick prompt and append one journal entry."},
		{name: "capture", description: "Capture a structured note with title, tags, project, and repo context."},
		{name: "integrate", description: "Install or remove desktop integrations such as Explorer's `Open with jot`."},
		{name: "list", description: "Browse journal entries and note files from the current directory."},
		{name: "new", description: "Create a new note from a template in the current directory."},
		{name: "open", description: "Print a jot entry by id, or pick and open a local file."},
		{name: "templates", description: "List every built-in and custom template available to `jot new`."},
		{name: "patterns", description: "Show the current placeholder for future pattern views."},
		{name: "help", description: "Show this command guide or drill into one command."},
	})
	writeExamplesSection(&b, style, []string{
		"jot",
		`jot capture "Ship the help refresh" --title release --tag cli`,
		"jot integrate windows",
		"jot list --full",
		"jot open dg0ftbuoqqdc-62",
		`jot new --template meeting -n "Team Sync"`,
		"jot templates",
		"jot help capture",
	})
	return b.String()
}

func renderInitHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot init", "Open the quick prompt and write one plain journal entry.")
	writeUsageSection(&b, style, []string{
		"jot",
		"jot init",
	}, []string{
		"If stdin is interactive, jot prompts with `what's on your mind?`.",
		"If stdin is piped, jot reads a single line and stores it as a prompt entry.",
	})
	writeExamplesSection(&b, style, []string{
		"jot",
		`echo "remember the release cut" | jot init`,
	})
	return b.String()
}

func renderCaptureHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot capture", "Capture a richer journal entry without leaving the terminal.")
	writeUsageSection(&b, style, []string{
		"jot capture [content] [--title TITLE] [--tag TAG] [--project PROJECT] [--repo REPO]",
	}, []string{
		"If `content` is omitted, jot opens your editor and stores the result on save-and-exit.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--title TITLE", description: "Set a title for the captured note."},
		{name: "--tag TAG", description: "Attach a tag. Repeat the flag to add more than one."},
		{name: "--project PROJECT", description: "Attach project context to the entry."},
		{name: "--repo REPO", description: "Attach repository context to the entry."},
	})
	writeExamplesSection(&b, style, []string{
		`jot capture "Ship the help refresh" --title release --tag cli --project jot`,
		`jot capture --title "standup notes" --tag team`,
	})
	return b.String()
}

func renderIntegrateHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot integrate", "Install or remove desktop integrations for jot.")
	writeUsageSection(&b, style, []string{
		"jot integrate windows",
		"jot integrate windows --remove",
	}, []string{
		"`jot integrate windows` adds an `Open with jot` entry to the Windows Explorer context menu for files.",
		"`jot integrate windows --remove` removes that Explorer integration for the current user.",
	})
	writeExamplesSection(&b, style, []string{
		"jot integrate windows",
		"jot integrate windows --remove",
	})
	return b.String()
}

func renderListHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot list", "Browse journal entries together with template-created notes in the current directory.")
	writeUsageSection(&b, style, []string{
		"jot list",
		"jot list --full",
		"jot list templates",
	}, []string{
		"`jot list` shows a compact terminal preview.",
		"`jot list --full` disables truncation in the terminal view.",
		"`jot list templates` is a shortcut for `jot templates`.",
		"When a preview is truncated, jot prints a `jot open <id>` hint instead of showing ids on every line.",
	})
	writeExamplesSection(&b, style, []string{
		"jot list",
		"jot list --full",
		"jot open dg0ftbuoqqdc-62",
	})
	return b.String()
}

func renderNewHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot new", "Create a note file from a built-in or custom template in the current directory.")
	writeUsageSection(&b, style, []string{
		"jot new [--template NAME] [--name TEXT]",
	}, []string{
		"The default template is `daily`.",
		"The generated filename starts with today's date.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--template NAME", description: "Choose which template to render. Defaults to `daily`."},
		{name: "--name TEXT, -n TEXT", description: "Append a slugified note name to the filename."},
	})
	writeExamplesSection(&b, style, []string{
		"jot new",
		`jot new --template meeting -n "Team Sync"`,
	})
	return b.String()
}

func renderOpenHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot open", "Print a single journal entry by id, or pick and open a local file.")
	writeUsageSection(&b, style, []string{
		"jot open",
		"jot open <id>",
		"jot open <path-to-file>",
	}, []string{
		"`jot open` with no argument shows a native file picker.",
		"Use this when `jot list` shows a `jot open <id>` hint for a truncated preview.",
		"Ids stay available for explicit lookup without cluttering the normal list view.",
		"If a local `.pdf`, `.md`, `.markdown`, `.json`, or `.xml` file is selected, jot opens it in a jot-owned viewer window when available.",
		"If no dedicated viewer window host is found, jot falls back to the normal browser.",
		"Other existing files are opened with the system default app.",
	})
	writeExamplesSection(&b, style, []string{
		"jot open",
		"jot open dg0ftbuoqqdc-62",
		"jot open note:2026-03-19-daily.md",
		`jot open ".\docs\paper.pdf"`,
		`jot open ".\docs\plan.md"`,
		`jot open ".\data\sample.json"`,
		`jot open ".\notes\todo.txt"`,
	})
	return b.String()
}

func renderTemplatesHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot templates", "List the templates that `jot new` can render right now.")
	writeUsageSection(&b, style, []string{
		"jot templates",
	}, []string{
		"Built-in templates are merged with any custom templates from your jot config directory.",
	})
	writeExamplesSection(&b, style, []string{
		"jot templates",
		"jot new --template daily",
	})
	return b.String()
}

func renderPatternsHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot patterns", "Show the current placeholder for upcoming pattern features.")
	writeUsageSection(&b, style, []string{
		"jot patterns",
	}, []string{
		"Today this command returns a fixed message. The help entry exists so the command is still discoverable from the CLI.",
	})
	writeExamplesSection(&b, style, []string{
		"jot patterns",
	})
	return b.String()
}

type helpCommand struct {
	name        string
	description string
}

type helpFlag struct {
	name        string
	description string
}

func writeHelpHeader(b *strings.Builder, style helpStyler, title, description string) {
	b.WriteString(style.title(title))
	b.WriteString("\n")
	if description != "" {
		b.WriteString(description)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeUsageSection(b *strings.Builder, style helpStyler, usage []string, notes []string) {
	b.WriteString(style.section("Usage"))
	b.WriteString("\n")
	for _, line := range usage {
		b.WriteString("  ")
		b.WriteString(style.command(line))
		b.WriteString("\n")
	}
	if len(notes) > 0 {
		b.WriteString("\n")
		for _, note := range notes {
			b.WriteString("  ")
			b.WriteString(style.dim(note))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

func writeCommandSection(b *strings.Builder, style helpStyler, commands []helpCommand) {
	b.WriteString(style.section("Commands"))
	b.WriteString("\n")
	for _, command := range commands {
		b.WriteString("  ")
		b.WriteString(style.command(command.name))
		b.WriteString("\n")
		b.WriteString("      ")
		b.WriteString(command.description)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeFlagSection(b *strings.Builder, style helpStyler, flags []helpFlag) {
	b.WriteString(style.section("Options"))
	b.WriteString("\n")
	for _, flag := range flags {
		b.WriteString("  ")
		b.WriteString(style.command(flag.name))
		b.WriteString("\n")
		b.WriteString("      ")
		b.WriteString(flag.description)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeExamplesSection(b *strings.Builder, style helpStyler, examples []string) {
	b.WriteString(style.section("Examples"))
	b.WriteString("\n")
	for _, example := range examples {
		b.WriteString("  ")
		b.WriteString(style.example(example))
		b.WriteString("\n")
	}
}

type commandRunner func(name string, args ...string) error

func jotIntegrate(w io.Writer, args []string, goos string, executablePath func() (string, error), run commandRunner) error {
	if len(args) == 0 || (len(args) == 1 && isHelpFlag(args[0])) {
		return writeHelp(w, "integrate")
	}
	if args[0] != "windows" {
		return fmt.Errorf("unknown integration target %q", args[0])
	}
	return jotIntegrateWindows(w, args[1:], goos, executablePath, run)
}

func jotIntegrateWindows(w io.Writer, args []string, goos string, executablePath func() (string, error), run commandRunner) error {
	if goos != "windows" {
		return errors.New("windows integration can only be installed from Windows")
	}

	set := flag.NewFlagSet("integrate windows", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	remove := false
	set.BoolVar(&remove, "remove", false, "remove integration")
	set.BoolVar(&remove, "r", false, "remove integration")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeHelp(w, "integrate")
		}
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", set.Args())
	}

	exePath, err := executablePath()
	if err != nil {
		return err
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	if remove {
		if err := removeWindowsContextMenu(exePath, run); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, "removed Explorer \"Open with jot\" integration for the current user")
		return err
	}

	if err := installWindowsContextMenu(exePath, run); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "installed Explorer \"Open with jot\" integration for the current user")
	return err
}

func windowsContextMenuKey() string {
	return `HKCU\Software\Classes\*\shell\Open with jot`
}

func installWindowsContextMenu(exePath string, run commandRunner) error {
	key := windowsContextMenuKey()
	command := fmt.Sprintf(`"%s" open "%%1"`, exePath)

	if err := run("reg", "add", key, "/ve", "/d", "Open with jot", "/f"); err != nil {
		return err
	}
	if err := run("reg", "add", key, "/v", "Icon", "/d", exePath, "/f"); err != nil {
		return err
	}
	return run("reg", "add", key+`\command`, "/ve", "/d", command, "/f")
}

func removeWindowsContextMenu(exePath string, run commandRunner) error {
	return run("reg", "delete", windowsContextMenuKey(), "/f")
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}

func jotInit(r io.Reader, w io.Writer, now func() time.Time) error {
	prompt := "jot › "
	if isTTY(w) {
		prompt = "\x1b[32m" + prompt + "\x1b[0m"
	}
	fmt.Fprint(w, prompt+"what’s on your mind? ")

	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	entry := strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(entry) == "" {
		return nil
	}

	journalPath, err := ensureJournalJSONL()
	if err != nil {
		return err
	}

	currentTime := now()
	journalEntry := journalEntry{
		ID:        newEntryID(currentTime, 0),
		CreatedAt: currentTime,
		Content:   entry,
		Source:    "prompt",
	}
	return appendJournalEntry(journalPath, journalEntry)
}

func jotList(w io.Writer, full bool) error {
	items, err := jotListItems()
	if err != nil {
		return err
	}

	if !isTTY(w) {
		return writeListItemsPlain(w, items)
	}

	return writeListItemsTTY(w, items, full)
}

func jotOpen(w io.Writer, target string) error {
	return jotOpenWithHandlers(w, target, openURLInViewerWindow, openPathWithDefaultApp, pickFileInteractively)
}

func jotOpenWithHandlers(w io.Writer, target string, openURL func(string) error, openPath func(string) error, pickFile func() (string, error)) error {
	target = strings.TrimSpace(target)
	if target == "" {
		var err error
		target, err = pickFile()
		if err != nil {
			return err
		}
		if strings.TrimSpace(target) == "" {
			return nil
		}
	}

	items, err := jotListItems()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.id == target {
			return writeListItemsPlain(w, []listItem{item})
		}
	}

	foundPath, err := openLocalPath(target, openURL, openPath)
	if err != nil {
		return err
	}
	if foundPath {
		return nil
	}
	return fmt.Errorf("no entry found with id %s", target)
}

func openLocalPath(target string, openURL func(string) error, openPath func(string) error) (bool, error) {
	return openLocalPathWithViewerLauncher(target, openURL, openPath, launchLocalFileInViewer)
}

func openLocalPathWithViewerLauncher(target string, openURL func(string) error, openPath func(string) error, launchViewer func(string, func(string) error) error) (bool, error) {
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	absPath, err := filepath.Abs(target)
	if err != nil {
		return true, err
	}
	if info.IsDir() {
		return true, openPath(absPath)
	}
	if viewerDocumentTypeForPath(absPath) != viewerDocumentTypeUnknown {
		return true, launchViewer(absPath, openURL)
	}
	return true, openPath(absPath)
}

func launchLocalFileInViewer(path string, openURL func(string) error) error {
	return launchLocalFileInViewerWithProcess(path, openURL, os.Executable, startViewerProcess)
}

type viewerProcessStarter func(executablePath string, filePath string) (string, error)

func launchLocalFileInViewerWithProcess(path string, openURL func(string) error, executablePath func() (string, error), start viewerProcessStarter) error {
	exePath, err := executablePath()
	if err != nil {
		return err
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}
	viewerURL, err := start(exePath, path)
	if err != nil {
		return err
	}
	return openURL(viewerURL)
}

func startViewerProcess(executablePath string, filePath string) (string, error) {
	cmd := exec.Command(executablePath, "__viewer", filePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", err
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func jotServeViewer(w io.Writer, args []string, now func() time.Time) error {
	if len(args) != 1 {
		return errors.New("usage: jot __viewer <path-to-file>")
	}
	path := strings.TrimSpace(args[0])
	if path == "" {
		return errors.New("file path must be provided")
	}
	return serveFileViewer(w, path, 15*time.Minute, now)
}

type viewerDocumentType string

const (
	viewerDocumentTypeUnknown  viewerDocumentType = ""
	viewerDocumentTypePDF      viewerDocumentType = "pdf"
	viewerDocumentTypeMarkdown viewerDocumentType = "markdown"
	viewerDocumentTypeJSON     viewerDocumentType = "json"
	viewerDocumentTypeXML      viewerDocumentType = "xml"
)

type viewerDocument struct {
	path     string
	fileName string
	docType  viewerDocumentType
	content  string
}

func viewerDocumentTypeForPath(path string) viewerDocumentType {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return viewerDocumentTypePDF
	case ".md", ".markdown":
		return viewerDocumentTypeMarkdown
	case ".json":
		return viewerDocumentTypeJSON
	case ".xml":
		return viewerDocumentTypeXML
	default:
		return viewerDocumentTypeUnknown
	}
}

func loadViewerDocument(path string) (viewerDocument, error) {
	docType := viewerDocumentTypeForPath(path)
	if docType == viewerDocumentTypeUnknown {
		return viewerDocument{}, fmt.Errorf("%s is not a supported jot viewer file", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return viewerDocument{}, err
	}
	if info.IsDir() {
		return viewerDocument{}, fmt.Errorf("%s is a directory, expected a file", path)
	}

	doc := viewerDocument{
		path:     path,
		fileName: filepath.Base(path),
		docType:  docType,
	}
	if docType == viewerDocumentTypePDF {
		return doc, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return viewerDocument{}, err
	}
	doc.content = normalizeViewerDocumentContent(docType, content)
	return doc, nil
}

func normalizeViewerDocumentContent(docType viewerDocumentType, content []byte) string {
	text := string(content)
	switch docType {
	case viewerDocumentTypeJSON:
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, content, "", "  "); err == nil {
			return pretty.String()
		}
	case viewerDocumentTypeXML:
		return strings.TrimSpace(text)
	}
	return text
}

func serveFileViewer(w io.Writer, path string, idleTimeout time.Duration, now func() time.Time) error {
	doc, err := loadViewerDocument(path)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	var mu sync.Mutex
	lastAccess := now()
	touch := func() {
		mu.Lock()
		lastAccess = now()
		mu.Unlock()
	}

	server := &http.Server{
		Handler: newFileViewerHandler(doc, touch),
	}

	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	addr := listener.Addr().(*net.TCPAddr)
	viewerURL := fmt.Sprintf("http://127.0.0.1:%d/", addr.Port)
	if _, err := fmt.Fprintln(w, viewerURL); err != nil {
		_ = server.Close()
		<-serverErr
		return err
	}
	if file, ok := w.(*os.File); ok {
		_ = file.Sync()
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case err := <-serverErr:
			return err
		case <-ticker.C:
			mu.Lock()
			idle := now().Sub(lastAccess)
			mu.Unlock()
			if idle < idleTimeout {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := server.Shutdown(ctx)
			cancel()
			serveErr := <-serverErr
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return serveErr
		}
	}
}

func newFileViewerHandler(doc viewerDocument, touch func()) http.Handler {
	const documentPath = "/document.pdf"
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		touch()
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, renderViewerPage(doc, documentPath))
	})
	mux.HandleFunc(documentPath, func(w http.ResponseWriter, r *http.Request) {
		touch()
		if doc.docType != viewerDocumentTypePDF {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", doc.fileName))
		http.ServeFile(w, r, doc.path)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		touch()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func renderViewerPage(doc viewerDocument, documentPath string) string {
	safeTitle := template.HTMLEscapeString(doc.fileName)
	safeDocumentPath := template.HTMLEscapeString(documentPath)
	bodyClass := "viewer-body viewer-body-text"
	contentHTML := renderViewerContent(doc, safeDocumentPath)
	if doc.docType == viewerDocumentTypePDF {
		bodyClass = "viewer-body viewer-body-pdf"
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>jot viewer - %s</title>
  <style>
    :root {
      color-scheme: light;
      font-family: "Segoe UI", sans-serif;
      background: #f4f1e8;
      color: #201a12;
    }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      grid-template-rows: auto 1fr;
      background:
        radial-gradient(circle at top left, rgba(210, 168, 109, 0.26), transparent 32%%),
        linear-gradient(180deg, #f9f4e8 0%%, #f2ecdf 100%%);
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 1rem;
      padding: 0.9rem 1.2rem;
      border-bottom: 1px solid rgba(32, 26, 18, 0.12);
      background: rgba(255, 250, 240, 0.86);
      backdrop-filter: blur(8px);
    }
    .brand {
      font-size: 0.75rem;
      letter-spacing: 0.14em;
      text-transform: uppercase;
      color: #8b5e34;
      font-weight: 700;
    }
    .file {
      margin-top: 0.18rem;
      font-size: 1rem;
      font-weight: 600;
      color: #201a12;
      word-break: break-word;
    }
    .hint {
      font-size: 0.85rem;
      color: #6c5b46;
      white-space: nowrap;
    }
    main {
      padding: 1rem;
    }
    .viewer-body-pdf .viewer-surface {
      padding: 0;
      overflow: hidden;
    }
    .viewer-surface {
      border: 1px solid rgba(32, 26, 18, 0.12);
      border-radius: 18px;
      background: rgba(255, 252, 246, 0.94);
      box-shadow: 0 24px 60px rgba(81, 60, 30, 0.12);
      overflow: auto;
    }
    iframe {
      display: block;
      width: 100%%;
      height: calc(100vh - 6rem);
      border: 0;
      background: white;
    }
    .text-frame {
      max-width: 60rem;
      margin: 0 auto;
      padding: 1.5rem;
      line-height: 1.7;
    }
    .text-frame h1, .text-frame h2, .text-frame h3, .text-frame h4, .text-frame h5, .text-frame h6 {
      font-family: Georgia, "Times New Roman", serif;
      line-height: 1.2;
      margin: 1.6rem 0 0.8rem;
      color: #201a12;
    }
    .text-frame h1 {
      margin-top: 0;
      font-size: 2rem;
    }
    .text-frame h2 {
      font-size: 1.55rem;
    }
    .text-frame p {
      margin: 0.85rem 0;
    }
    .text-frame ul {
      margin: 0.9rem 0 0.9rem 1.4rem;
      padding: 0;
    }
    .text-frame li {
      margin: 0.35rem 0;
    }
    .text-frame blockquote {
      margin: 1rem 0;
      padding: 0.4rem 1rem;
      border-left: 4px solid #c08a49;
      color: #5a4d3f;
      background: rgba(240, 221, 193, 0.32);
    }
    .text-frame code {
      padding: 0.1rem 0.35rem;
      border-radius: 6px;
      background: rgba(51, 38, 24, 0.08);
      font-family: Consolas, "SFMono-Regular", monospace;
      font-size: 0.95em;
    }
    .text-frame pre {
      margin: 1rem 0;
      padding: 1rem;
      border-radius: 14px;
      overflow: auto;
      background: #1d1d1b;
      color: #f8f4ea;
      box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.06);
    }
    .text-frame pre code {
      padding: 0;
      background: transparent;
      color: inherit;
    }
    .structured-block {
      white-space: pre-wrap;
      word-break: break-word;
    }
    @media (max-width: 720px) {
      header {
        align-items: flex-start;
        flex-direction: column;
      }
      .hint {
        white-space: normal;
      }
      iframe {
        height: calc(100vh - 8rem);
      }
    }
  </style>
</head>
<body class="%s">
  <header>
    <div>
      <div class="brand">jot viewer</div>
      <div class="file">%s</div>
    </div>
    <div class="hint">%s</div>
  </header>
  <main>
    <div class="viewer-surface">
      %s
    </div>
  </main>
</body>
</html>
`, safeTitle, bodyClass, safeTitle, template.HTMLEscapeString(viewerDocumentHint(doc.docType)), contentHTML)
}

func renderViewerContent(doc viewerDocument, safeDocumentPath string) string {
	switch doc.docType {
	case viewerDocumentTypePDF:
		return fmt.Sprintf(`<iframe src="%s" title="%s"></iframe>`, safeDocumentPath, template.HTMLEscapeString(doc.fileName))
	case viewerDocumentTypeMarkdown:
		return `<article class="text-frame markdown-frame">` + renderMarkdownHTML(doc.content) + `</article>`
	case viewerDocumentTypeJSON, viewerDocumentTypeXML:
		return `<div class="text-frame"><pre class="structured-block"><code>` + template.HTMLEscapeString(doc.content) + `</code></pre></div>`
	default:
		return `<div class="text-frame"><p>Preview not available.</p></div>`
	}
}

func viewerDocumentHint(docType viewerDocumentType) string {
	switch docType {
	case viewerDocumentTypePDF:
		return "Local PDF session"
	case viewerDocumentTypeMarkdown:
		return "Markdown preview"
	case viewerDocumentTypeJSON:
		return "JSON preview"
	case viewerDocumentTypeXML:
		return "XML preview"
	default:
		return "Local file preview"
	}
}

func renderMarkdownHTML(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var b strings.Builder
	inList := false
	inCodeBlock := false

	closeList := func() {
		if !inList {
			return
		}
		b.WriteString("</ul>")
		inList = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			closeList()
			if inCodeBlock {
				b.WriteString("</code></pre>")
				inCodeBlock = false
				continue
			}
			b.WriteString(`<pre><code>`)
			inCodeBlock = true
			continue
		}
		if inCodeBlock {
			b.WriteString(template.HTMLEscapeString(line))
			b.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			closeList()
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString("<li>")
			b.WriteString(renderMarkdownInline(strings.TrimSpace(trimmed[2:])))
			b.WriteString("</li>")
			continue
		}
		closeList()
		if level := markdownHeadingLevel(trimmed); level > 0 {
			text := strings.TrimSpace(trimmed[level+1:])
			b.WriteString(fmt.Sprintf("<h%d>%s</h%d>", level, renderMarkdownInline(text), level))
			continue
		}
		if strings.HasPrefix(trimmed, "> ") {
			b.WriteString("<blockquote><p>")
			b.WriteString(renderMarkdownInline(strings.TrimSpace(trimmed[2:])))
			b.WriteString("</p></blockquote>")
			continue
		}
		b.WriteString("<p>")
		b.WriteString(renderMarkdownInline(trimmed))
		b.WriteString("</p>")
	}
	closeList()
	if inCodeBlock {
		b.WriteString("</code></pre>")
	}
	return b.String()
}

func markdownHeadingLevel(line string) int {
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

func renderMarkdownInline(text string) string {
	var b strings.Builder
	for {
		start := strings.Index(text, "`")
		if start < 0 {
			b.WriteString(template.HTMLEscapeString(text))
			return b.String()
		}
		end := strings.Index(text[start+1:], "`")
		if end < 0 {
			b.WriteString(template.HTMLEscapeString(text))
			return b.String()
		}
		b.WriteString(template.HTMLEscapeString(text[:start]))
		code := text[start+1 : start+1+end]
		b.WriteString("<code>")
		b.WriteString(template.HTMLEscapeString(code))
		b.WriteString("</code>")
		text = text[start+1+end+1:]
	}
}

func openURLInBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Run()
}

type commandSpec struct {
	name string
	args []string
}

func openURLInViewerWindow(targetURL string) error {
	spec, ok := viewerWindowCommand(targetURL, runtime.GOOS, os.Getenv, exec.LookPath, pathExists)
	if !ok {
		return openURLInBrowser(targetURL)
	}
	return startDetachedCommand(spec.name, spec.args...)
}

func viewerWindowCommand(targetURL string, goos string, getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) (commandSpec, bool) {
	for _, candidate := range viewerBrowserCandidates(goos, getenv) {
		for _, path := range candidate.paths {
			if !exists(path) {
				continue
			}
			return commandSpec{
				name: path,
				args: []string{"--app=" + targetURL},
			}, true
		}
		for _, name := range candidate.names {
			resolvedPath, err := lookPath(name)
			if err != nil {
				continue
			}
			return commandSpec{
				name: resolvedPath,
				args: []string{"--app=" + targetURL},
			}, true
		}
	}
	return commandSpec{}, false
}

type viewerBrowserCandidate struct {
	paths []string
	names []string
}

func viewerBrowserCandidates(goos string, getenv func(string) string) []viewerBrowserCandidate {
	switch goos {
	case "windows":
		programFiles := getenv("ProgramFiles")
		programFilesX86 := getenv("ProgramFiles(x86)")
		localAppData := getenv("LocalAppData")
		return []viewerBrowserCandidate{
			{
				paths: collectNonEmptyStrings(
					filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),
				),
				names: []string{"msedge.exe", "msedge"},
			},
			{
				paths: collectNonEmptyStrings(
					filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
				),
				names: []string{"chrome.exe", "chrome"},
			},
			{
				paths: collectNonEmptyStrings(
					filepath.Join(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
					filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
					filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
				),
				names: []string{"brave.exe", "brave"},
			},
		}
	case "darwin":
		return []viewerBrowserCandidate{
			{
				paths: []string{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
				names: []string{"Microsoft Edge"},
			},
			{
				paths: []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
				names: []string{"Google Chrome", "google-chrome"},
			},
			{
				paths: []string{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"},
				names: []string{"Brave Browser"},
			},
			{
				paths: []string{"/Applications/Chromium.app/Contents/MacOS/Chromium"},
				names: []string{"Chromium", "chromium"},
			},
		}
	default:
		return []viewerBrowserCandidate{
			{
				names: []string{"microsoft-edge", "microsoft-edge-stable"},
			},
			{
				names: []string{"google-chrome", "google-chrome-stable"},
			},
			{
				names: []string{"chromium", "chromium-browser"},
			},
			{
				names: []string{"brave-browser", "brave"},
			},
		}
	}
}

func collectNonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func startDetachedCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func openPathWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "shell32.dll,ShellExec_RunDLL", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Run()
}

func pickFileInteractively() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return runPickerCommand("powershell", "-NoProfile", "-STA", "-Command", `Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.OpenFileDialog; $dialog.CheckFileExists = $true; $dialog.Multiselect = $false; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; Write-Output $dialog.FileName }`)
	case "darwin":
		return runPickerCommand("osascript", "-e", `POSIX path of (choose file)`)
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			return runPickerCommand("zenity", "--file-selection")
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			return runPickerCommand("kdialog", "--getopenfilename")
		}
		return "", errors.New("no file picker available; install zenity or kdialog, or pass a path directly")
	}
}

func runPickerCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

type listItem struct {
	timestamp time.Time
	lines     []string
	order     int
	source    string
	id        string
}

type journalEntry struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Content   string     `json:"content,omitempty"`
	Title     string     `json:"title,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Project   string     `json:"project,omitempty"`
	Repo      string     `json:"repo,omitempty"`
	Source    string     `json:"source,omitempty"`
}

func collectJournalEntries(r io.Reader, source string) ([]listItem, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var items []listItem
	order := 0
	var current *listItem
	for scanner.Scan() {
		line := scanner.Text()
		ts := parseTimestamp(line)
		if !ts.IsZero() {
			item := listItem{
				timestamp: ts,
				lines:     []string{line},
				order:     order,
				source:    source,
			}
			items = append(items, item)
			current = &items[len(items)-1]
			order++
			continue
		}
		if current == nil {
			item := listItem{
				timestamp: time.Time{},
				lines:     []string{line},
				order:     order,
				source:    source,
			}
			items = append(items, item)
			current = &items[len(items)-1]
			order++
			continue
		}
		current.lines = append(current.lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func loadJournalEntries(path string) ([]journalEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var entries []journalEntry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		if entry.ID == "" {
			entry.ID = newEntryID(entry.CreatedAt, 0)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func appendJournalEntry(path string, entry journalEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if entry.ID == "" {
		entry.ID = newEntryID(entry.CreatedAt, 0)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(entry)
}

func entryToListItem(entry journalEntry, source string, order int) listItem {
	body := formatEntryBody(entry)
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	lines[0] = fmt.Sprintf("[%s] %s", entry.CreatedAt.Format("2006-01-02 15:04"), lines[0])
	return listItem{
		timestamp: entry.CreatedAt,
		lines:     lines,
		order:     order,
		source:    source,
		id:        entry.ID,
	}
}

func formatEntryBody(entry journalEntry) string {
	content := strings.TrimRight(entry.Content, "\r\n")
	title := strings.TrimSpace(entry.Title)

	var builder strings.Builder
	if title != "" {
		builder.WriteString(title)
		if content != "" {
			builder.WriteString(" â€” ")
			builder.WriteString(content)
		}
	} else if content != "" {
		builder.WriteString(content)
	}

	metadata := []string{}
	if len(entry.Tags) > 0 {
		metadata = append(metadata, "tags: "+strings.Join(entry.Tags, ", "))
	}
	if strings.TrimSpace(entry.Project) != "" {
		metadata = append(metadata, "project: "+strings.TrimSpace(entry.Project))
	}
	if strings.TrimSpace(entry.Repo) != "" {
		metadata = append(metadata, "repo: "+strings.TrimSpace(entry.Repo))
	}
	if len(metadata) > 0 {
		builder.WriteString(" (")
		builder.WriteString(strings.Join(metadata, "; "))
		builder.WriteString(")")
	}

	return builder.String()
}

func journalEntryFromListItem(item listItem, seq int) journalEntry {
	content := contentFromListItem(item)
	createdAt := item.timestamp
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return journalEntry{
		ID:        newEntryID(createdAt, seq),
		CreatedAt: createdAt,
		Content:   content,
		Source:    "import",
	}
}

func contentFromListItem(item listItem) string {
	if len(item.lines) == 0 {
		return ""
	}
	first := item.lines[0]
	contentFirst := first
	if strings.HasPrefix(first, "[") {
		if end := strings.IndexByte(first, ']'); end > 0 {
			contentFirst = strings.TrimSpace(first[end+1:])
		}
	}
	lines := []string{contentFirst}
	if len(item.lines) > 1 {
		lines = append(lines, item.lines[1:]...)
	}
	return strings.Join(lines, "\n")
}

func newEntryID(t time.Time, seq int) string {
	if t.IsZero() {
		t = time.Now()
	}
	base := strconv.FormatInt(t.UnixNano(), 36)
	if seq > 0 {
		return fmt.Sprintf("%s-%d", base, seq)
	}
	return base
}

func collectTemplateNotes(dir string) ([]listItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var items []listItem
	order := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isTemplateNoteName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		lines := []string{fmt.Sprintf("[%s] %s", info.ModTime().Format("2006-01-02 15:04"), name)}
		for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
			lines = append(lines, line)
		}
		items = append(items, listItem{
			timestamp: info.ModTime(),
			lines:     lines,
			order:     order,
			source:    path,
			id:        fmt.Sprintf("note:%s", name),
		})
		order++
	}
	return items, nil
}

func sortListItems(items []listItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].timestamp.Equal(items[j].timestamp) {
			return items[i].order < items[j].order
		}
		return items[i].timestamp.Before(items[j].timestamp)
	})
}

func jotListItems() ([]listItem, error) {
	journalPath, err := ensureJournalJSONL()
	if err != nil {
		return nil, err
	}

	entries, err := loadJournalEntries(journalPath)
	if err != nil {
		return nil, err
	}

	var items []listItem
	order := 0
	for _, entry := range entries {
		items = append(items, entryToListItem(entry, journalPath, order))
		order++
	}

	noteItems, err := collectTemplateNotes(mustGetwd())
	if err != nil {
		return nil, err
	}
	for i := range noteItems {
		noteItems[i].order = order
		order++
	}
	items = append(items, noteItems...)
	sortListItems(items)
	return items, nil
}

func writeListItemsPlain(w io.Writer, items []listItem) error {
	for _, item := range items {
		for _, line := range item.lines {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeListItemsTTY(w io.Writer, items []listItem, full bool) error {
	var lines []string
	const previewLines = 3
	for _, item := range items {
		limit := previewLines
		if full {
			limit = 0
		}
		itemLines := previewListLines(item, limit)
		itemLines = annotateListItemLines(item, itemLines)
		lines = append(lines, itemLines...)
	}
	lastIdx := len(lines) - 1
	for lastIdx >= 0 && strings.TrimSpace(lines[lastIdx]) == "" {
		lastIdx--
	}

	prevDate := ""
	sep := "\x1b[90m" + "----------------" + "\x1b[0m"
	for i, line := range lines {
		if strings.HasPrefix(line, "[") {
			if end := strings.IndexByte(line, ']'); end > 0 {
				ts := line[:end+1]
				rest := line[end+1:]
				datePart := strings.SplitN(ts[1:len(ts)-1], " ", 2)[0]
				if prevDate != "" && datePart != prevDate {
					if _, err := fmt.Fprintln(w, sep); err != nil {
						return err
					}
				}
				prevDate = datePart
				if i == lastIdx {
					rest = "\x1b[36m" + rest + "\x1b[0m"
				}
				line = "\x1b[90m" + ts + "\x1b[0m" + rest
			}
		} else if i == lastIdx {
			line = "\x1b[36m" + line + "\x1b[0m"
		}

		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	return nil
}

func previewListLines(item listItem, limit int) []string {
	if limit <= 0 || len(item.lines) <= limit {
		return item.lines
	}
	lines := append([]string{}, item.lines[:limit]...)
	lines = append(lines, fmt.Sprintf("\x1b[92m… (jot open %s)\x1b[0m", item.id))
	return lines
}

func annotateListItemLines(item listItem, lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	return append([]string{}, lines...)
}

func parseTimestamp(line string) time.Time {
	if !strings.HasPrefix(line, "[") {
		return time.Time{}
	}
	end := strings.IndexByte(line, ']')
	if end <= 1 {
		return time.Time{}
	}
	ts := strings.TrimSpace(line[1:end])
	parsed, err := time.Parse("2006-01-02 15:04", ts)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func isTemplateNoteName(name string) bool {
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		return false
	}
	if len(name) < len("2006-01-02-.md") {
		return false
	}
	if name[4] != '-' || name[7] != '-' {
		return false
	}
	datePart := name[:10]
	if _, err := time.Parse("2006-01-02", datePart); err != nil {
		return false
	}
	return true
}

func jotNew(w io.Writer, now func() time.Time, args []string) error {
	set := flag.NewFlagSet("new", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var templateName string
	var noteName string
	set.StringVar(&templateName, "template", "daily", "template to use")
	set.StringVar(&noteName, "name", "", "note name")
	set.StringVar(&noteName, "n", "", "note name")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeHelp(w, "new")
		}
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", set.Args())
	}

	templates, err := loadTemplates()
	if err != nil {
		return err
	}
	content, ok := templates[templateName]
	if !ok {
		return fmt.Errorf("template %q not found", templateName)
	}

	currentTime := now()
	repo := repoName()
	rendered := renderTemplate(content, currentTime, repo)
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}

	filename := templateName
	if noteName != "" {
		slug := slugifyName(noteName)
		if slug == "" {
			return fmt.Errorf("note name must contain letters or numbers")
		}
		filename = fmt.Sprintf("%s-%s", templateName, slug)
	}
	filename = fmt.Sprintf("%s-%s.md", currentTime.Format("2006-01-02"), filename)
	path := filepath.Join(mustGetwd(), filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("note already exists: %s", path)
		}
		return err
	}
	if _, err := io.WriteString(file, rendered); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, path)
	return err
}

func jotTemplates(w io.Writer) error {
	templates, err := loadTemplates()
	if err != nil {
		return err
	}

	var names []string
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, err := fmt.Fprintln(w, name); err != nil {
			return err
		}
	}
	return nil
}

func ensureJournalJSONL() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	journalDir, journalTxtPath, journalJSONLPath := journalPaths(home)

	// Create the directory and file lazily so jot stays zero-config.
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return "", err
	}

	if err := migrateJournalIfNeeded(journalTxtPath, journalJSONLPath); err != nil {
		return "", err
	}

	file, err := os.OpenFile(journalJSONLPath, os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	return journalJSONLPath, nil
}

func journalPaths(home string) (string, string, string) {
	journalDir := filepath.Join(home, ".jot")
	journalTxtPath := filepath.Join(journalDir, "journal.txt")
	journalJSONLPath := filepath.Join(journalDir, "journal.jsonl")
	return journalDir, journalTxtPath, journalJSONLPath
}

func migrateJournalIfNeeded(txtPath, jsonlPath string) error {
	if _, err := os.Stat(jsonlPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if _, err := os.Stat(txtPath); err != nil {
		if os.IsNotExist(err) {
			return createEmptyJournal(jsonlPath)
		}
		return err
	}

	file, err := os.Open(txtPath)
	if err != nil {
		return err
	}
	defer file.Close()

	items, err := collectJournalEntries(file, txtPath)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return createEmptyJournal(jsonlPath)
	}

	out, err := os.OpenFile(jsonlPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer out.Close()

	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	for i, item := range items {
		entry := journalEntryFromListItem(item, i)
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}

func createEmptyJournal(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return file.Close()
}

func templateDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err == nil && configDir != "" {
		return filepath.Join(configDir, "jot", "templates"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jot", "templates"), nil
}

func loadTemplates() (map[string]string, error) {
	templates := builtinTemplates()
	custom, err := loadCustomTemplates()
	if err != nil {
		return nil, err
	}
	for name, content := range custom {
		templates[name] = content
	}
	return templates, nil
}

func builtinTemplates() map[string]string {
	return map[string]string{
		"daily": strings.Join([]string{
			"# Daily Log — {{date}}",
			"",
			"## Focus",
			"- ",
			"",
			"## Notes",
			"- ",
			"",
			"## Closing",
			"- What moved?",
		}, "\n"),
		"meeting": strings.Join([]string{
			"# Meeting — {{date}} {{time}}",
			"",
			"## Attendees",
			"- ",
			"",
			"## Agenda",
			"- ",
			"",
			"## Notes",
			"- ",
			"",
			"## Next Steps",
			"- ",
		}, "\n"),
		"rfc": strings.Join([]string{
			"# RFC — {{repo}} — {{date}}",
			"",
			"## Problem",
			"- ",
			"",
			"## Proposal",
			"- ",
			"",
			"## Alternatives",
			"- ",
			"",
			"## Risks",
			"- ",
		}, "\n"),
	}
}

func loadCustomTemplates() (map[string]string, error) {
	dir, err := templateDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	custom := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.TrimSpace(name) == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		custom[name] = string(data)
	}
	return custom, nil
}

func renderTemplate(content string, now time.Time, repo string) string {
	replacements := strings.NewReplacer(
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04"),
		"{{datetime}}", now.Format("2006-01-02 15:04"),
		"{{repo}}", repo,
	)
	return replacements.Replace(content)
}

func slugifyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune(' ')
		}
	}
	parts := strings.Fields(builder.String())
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.Join(parts, "-"))
}

func repoName() string {
	wd := mustGetwd()
	for {
		if info, err := os.Stat(filepath.Join(wd, ".git")); err == nil && info != nil {
			return filepath.Base(wd)
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return ""
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func isTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

type captureOptions struct {
	Content string
	Title   string
	Tags    []string
	Project string
	Repo    string
	Editor  bool
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func parseCaptureArgs(args []string) (captureOptions, error) {
	var options captureOptions
	var tags stringSliceFlag

	flags := flag.NewFlagSet("capture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Title, "title", "", "optional title")
	flags.Var(&tags, "tag", "tag (repeatable)")
	flags.StringVar(&options.Project, "project", "", "project context")
	flags.StringVar(&options.Repo, "repo", "", "repo context")

	var flagArgs []string
	var contentArgs []string
	consumeContent := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if consumeContent {
			contentArgs = append(contentArgs, arg)
			continue
		}
		if arg == "--" {
			consumeContent = true
			continue
		}
		if arg == "-h" || arg == "--help" {
			return options, flag.ErrHelp
		}
		if strings.HasPrefix(arg, "-") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--title", "--tag", "--project", "--repo":
				flagArgs = append(flagArgs, name)
				if hasValue {
					flagArgs = append(flagArgs, value)
				} else {
					if i+1 >= len(args) {
						return options, fmt.Errorf("missing value for %s", name)
					}
					i++
					flagArgs = append(flagArgs, args[i])
				}
				continue
			default:
				return options, fmt.Errorf("unknown flag: %s", arg)
			}
		}
		contentArgs = append(contentArgs, arg)
	}

	if err := flags.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options, flag.ErrHelp
		}
		return options, err
	}

	options.Tags = []string(tags)
	if len(contentArgs) > 0 {
		options.Content = strings.Join(contentArgs, " ")
	} else {
		options.Editor = true
	}
	return options, nil
}

func jotCapture(w io.Writer, args []string, now func() time.Time, launch editorLauncher) error {
	options, err := parseCaptureArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeHelp(w, "capture")
		}
		return err
	}

	if options.Editor {
		content, err := captureFromEditor(launch)
		if err != nil {
			return err
		}
		options.Content = content
	}

	content := strings.TrimSpace(options.Content)
	if content == "" && strings.TrimSpace(options.Title) == "" {
		return nil
	}

	journalPath, err := ensureJournalJSONL()
	if err != nil {
		return err
	}

	source := "capture"
	if options.Editor {
		source = "editor"
	}
	currentTime := now()
	journalEntry := journalEntry{
		ID:        newEntryID(currentTime, 0),
		CreatedAt: currentTime,
		UpdatedAt: nil,
		Content:   content,
		Title:     strings.TrimSpace(options.Title),
		Tags:      options.Tags,
		Project:   strings.TrimSpace(options.Project),
		Repo:      strings.TrimSpace(options.Repo),
		Source:    source,
	}
	return appendJournalEntry(journalPath, journalEntry)
}

type editorLauncher func(editor, path string) error

func captureFromEditor(launch editorLauncher) (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	file, err := os.CreateTemp("", "jot-capture-*.txt")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	defer os.Remove(path)

	if err := launch(editor, path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(content), "\r\n"), nil
}

func launchEditor(editor, path string) error {
	args, err := splitEditorCommand(editor)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("editor command is empty")
	}

	cmd := exec.Command(args[0], append(args[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func splitEditorCommand(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	runes := []rune(strings.TrimSpace(command))
	inSingle := false
	inDouble := false

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		case '\\':
			if !inSingle {
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if inDouble || (next != 0 && (unicode.IsSpace(next) || next == '"' || next == '\'' || next == '\\')) {
					if next != 0 {
						current.WriteRune(next)
						i++
						continue
					}
				}
			}
		default:
			if unicode.IsSpace(r) && !inSingle && !inDouble {
				flush()
				continue
			}
		}
		current.WriteRune(r)
	}

	if inSingle || inDouble {
		return nil, errors.New("unterminated quote in editor command")
	}
	flush()
	return args, nil
}
