package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
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

const version = "1.7.3"
const viewerTempExecutableEnv = "JOT_VIEWER_TEMP_EXE"

//go:embed assets/jot-logo.png
var viewerLogoPNG []byte

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
		defer cleanupViewerTempExecutable(runtime.GOOS, os.Getenv(viewerTempExecutableEnv))
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

	if len(args) >= 1 && args[0] == "convert" {
		if err := jotConvert(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "minify" {
		if err := jotMinify(os.Stdout, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				if err := writeHelp(os.Stdout, "minify"); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				return
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "encode" {
		if err := jotEncode(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "hash" {
		if err := jotHash(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "compress" {
		if err := jotCompress(os.Stdout, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				if err := writeHelp(os.Stdout, "compress"); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				return
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "timestamp" {
		if err := jotTimestamp(os.Stdout, args[1:], time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "uuid" {
		if err := jotUUID(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "resize" {
		if err := jotResize(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "diff" {
		if err := jotDiff(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "rename" {
		if err := jotRename(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "qr" {
		if err := jotQR(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "strip" {
		if err := jotStrip(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "palette" {
		if err := jotPalette(os.Stdout, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "task" {
		if err := jotTask(os.Stdin, os.Stdout, args[1:], mustGetwd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "assistant" {
		if err := jotAssistant(os.Stdin, os.Stdout, args[1:], time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "write" {
		if len(args) == 2 && isHelpFlag(args[1]) {
			if err := writeHelp(os.Stdout, "write"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if err := jotWrite(os.Stdout, args[1:]); err != nil {
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

// termUI provides a small, consistent set of styled terminal components for
// interactive jot commands. Every method degrades gracefully to plain text
// when color is false (i.e. when stdout is not a TTY).
type termUI struct {
	color bool
}

func newTermUI(w io.Writer) termUI {
	return termUI{color: isTTY(w)}
}

func (u termUI) esc(code, text string) string {
	if !u.color {
		return text
	}
	return code + text + "\x1b[0m"
}

// Primitive colour helpers.
func (u termUI) tcyan(text string) string    { return u.esc("\x1b[1;96m", text) }
func (u termUI) tgreen(text string) string   { return u.esc("\x1b[1;32m", text) }
func (u termUI) tyellow(text string) string  { return u.esc("\x1b[33m", text) }
func (u termUI) tdim(text string) string     { return u.esc("\x1b[90m", text) }
func (u termUI) tbold(text string) string    { return u.esc("\x1b[1m", text) }
func (u termUI) tmagenta(text string) string { return u.esc("\x1b[35m", text) }

// rule returns a subtle horizontal separator.
func (u termUI) rule() string {
	const width = 44
	return u.tdim(strings.Repeat("─", width))
}

// header prints a titled section opener with a rule beneath it.
func (u termUI) header(title string) string {
	return "\n" + u.tcyan(title) + "\n" + u.rule() + "\n"
}

// sectionLabel prints a small ALL-CAPS dim label used to group items.
func (u termUI) sectionLabel(title string) string {
	return "\n" + u.tdim(strings.ToUpper(title)) + "\n\n"
}

// success renders a green ✓ confirmation line.
func (u termUI) success(text string) string {
	return "  " + u.tgreen("✓") + "  " + u.tbold(text)
}

// warnLine renders a yellow ⚠ advisory line.
func (u termUI) warnLine(text string) string {
	return "  " + u.tyellow("⚠") + "  " + text
}

// tip renders a subtle → hint, typically shown after a success.
func (u termUI) tip(text string) string {
	return "  " + u.tdim("→  "+text)
}

// listItem renders a numbered menu row.
//
//	n     — the item number shown on the left
//	name  — the primary label (bold)
//	desc  — a short description (dim), or "" to omit
//	meta  — right-side metadata such as a file size (dim), or "" to omit
func (u termUI) listItem(n int, name, desc, meta string) string {
	num := fmt.Sprintf("%d", n)
	line := "  " + u.tdim(num) + "  " + u.tbold(name)
	if desc != "" {
		line += "   " + u.tdim(desc)
	}
	if meta != "" {
		line += "  " + u.tdim(meta)
	}
	return line
}

// styledPrompt builds a prompt string:  ›  label [hint]:
func (u termUI) styledPrompt(label, hint string) string {
	hintPart := ""
	if hint != "" {
		hintPart = " " + u.tdim("["+hint+"]")
	}
	return "  " + u.tgreen("›") + "  " + label + hintPart + ": "
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
	case "convert":
		return renderConvertHelp(color), nil
	case "minify":
		return renderMinifyHelp(color), nil
	case "encode":
		return renderEncodeHelp(color), nil
	case "hash":
		return renderHashHelp(color), nil
	case "compress":
		return renderCompressHelp(color), nil
	case "timestamp":
		return renderTimestampHelp(color), nil
	case "uuid":
		return renderUUIDHelp(color), nil
	case "resize":
		return renderResizeHelp(color), nil
	case "diff":
		return renderDiffHelp(color), nil
	case "rename":
		return renderRenameHelp(color), nil
	case "qr":
		return renderQRHelp(color), nil
	case "strip":
		return renderStripHelp(color), nil
	case "palette":
		return renderPaletteHelp(color), nil
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
	case "task":
		return renderTaskHelp(color), nil
	case "assistant":
		return renderAssistantHelp(color), nil
	case "write":
		return renderWriteHelp(color), nil
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
		{name: "open", description: "Print a jot entry by id, or pick and open a local file."},
		{name: "write", description: "Open a markdown file in jot's terminal editor with syntax highlighting."},
		{name: "capture", description: "Capture a structured note with title, tags, project, and repo context."},
		{name: "convert", description: "Convert a local image into `.ico` or `.svg` without leaving the terminal."},
		{name: "minify", description: "Minify or pretty-print local JSON from files, text, or stdin."},
		{name: "encode", description: "Base64 encode or decode local files, text, or stdin."},
		{name: "hash", description: "Compute or verify MD5, SHA1, SHA256, and SHA512 digests."},
		{name: "compress", description: "Create local zip, tar, or tar.gz archives from files and folders."},
		{name: "timestamp", description: "Convert Unix timestamps and human-readable dates in the terminal."},
		{name: "uuid", description: "Generate UUIDs, nanoids, and random strings."},
		{name: "resize", description: "Resize local images with fit, fill, or stretch modes."},
		{name: "diff", description: "Compare two local text files with a detailed terminal render."},
		{name: "rename", description: "Preview and apply safe local renames with patterns and templates."},
		{name: "qr", description: "Generate local QR codes as PNG, SVG, or ASCII."},
		{name: "strip", description: "Strip metadata from local image files by re-encoding them."},
		{name: "palette", description: "Extract a terminal-friendly color palette from a local image."},
		{name: "task", description: "Discover and run terminal-first tasks such as conversion, hashing, compression, resize, and diff."},
		{name: "assistant", description: "Run the CLI-native assistant for Gmail and future connected tools."},
		{name: "list", description: "Browse journal entries and note files from the current directory."},
		{name: "integrate", description: "Install or remove desktop integrations such as Explorer's `Open with jot`."},
		{name: "new", description: "Create a new note from a template in the current directory."},
		{name: "templates", description: "List every built-in and custom template available to `jot new`."},
		{name: "patterns", description: "Show the current placeholder for future pattern views."},
		{name: "help", description: "Show this command guide or drill into one command."},
	})
	writeExamplesSection(&b, style, []string{
		"jot",
		`jot capture "Ship the help refresh" --title release --tag cli`,
		"jot convert logo.png ico",
		"jot minify data.json",
		"jot hash package.zip",
		"jot resize logo.png 512x512",
		"jot diff before.txt after.txt",
		"jot task",
		"jot assistant --help",
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

func renderWriteHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot write", "Open a markdown file in jot's terminal editor.")
	writeUsageSection(&b, style, []string{
		"jot write <path-to-file>",
	}, []string{
		"Shows one full-screen markdown editor with syntax highlighting and line numbers.",
		"Works on any .md file, whether it already exists or not.",
		"Pair with `jot new` to create a file first.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "ctrl+s", description: "Save and exit."},
		{name: "ctrl+q", description: "Quit without saving."},
		{name: "ctrl+b", description: "Insert **bold** markers at the cursor."},
		{name: "ctrl+e", description: "Insert _italic_ markers at the cursor."},
		{name: "ctrl+k", description: "Insert `inline code` markers at the cursor."},
		{name: "ctrl+g", description: "Insert a fenced code block below the cursor."},
		{name: "ctrl+l", description: "Turn current line into a - list item."},
		{name: "ctrl+r", description: "Insert a --- horizontal rule below the cursor."},
		{name: `ctrl+\`, description: "Turn the current line into an # h1 heading."},
		{name: "ctrl+]", description: "Turn the current line into an ## h2 heading."},
		{name: "ctrl+^", description: "Turn the current line into an ### h3 heading."},
	})
	writeExamplesSection(&b, style, []string{
		"jot new --template rfc -n \"auth refactor\"",
		"jot write 2026-03-22-rfc-auth-refactor.md",
		"jot write README.md",
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

func renderConvertHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot convert", "Convert a local image into web-ready image formats right from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot convert <image-path> <png|jpg|jpeg|gif|ico|svg>",
		"jot convert <image-path> <format> --out <output-path>",
	}, []string{
		"`.png`, `.jpg`, and `.gif` output re-encode the source image into that target format and save it next to the source image by default.",
		"`.ico` output builds a multi-size favicon-style icon automatically.",
		"Raster-to-`.svg` output wraps the source image inside a standalone SVG file; jot does not trace vectors yet.",
		"Supported raster inputs today: `.png`, `.jpg`, `.jpeg`, and `.gif`.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--out PATH, -o PATH", description: "Write the converted file to a specific path instead of next to the source image."},
		{name: "--overwrite", description: "Replace an existing output file if one is already present."},
	})
	writeExamplesSection(&b, style, []string{
		"jot convert logo.png ico",
		"jot convert logo.png svg",
		"jot convert screenshot.png jpg",
		`jot convert ".\assets\brand.jpg" ico --out ".\public\favicon.ico"`,
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
		"jot open .",
		"jot open <id>",
		"jot open <path-to-file>",
	}, []string{
		"`jot open` with no argument shows a native file picker.",
		"Use this when `jot list` shows a `jot open <id>` hint for a truncated preview.",
		"Ids stay available for explicit lookup without cluttering the normal list view.",
		"If a local `.pdf`, `.md`, `.markdown`, `.json`, `.xml`, `.yaml`, `.yml`, `.toml`, `.csv`, `.env`, `.txt`, `.log`, or `.jsonl` file is selected, jot opens it in a jot-owned viewer window when available.",
		"If no dedicated viewer window host is found, jot falls back to the normal browser.",
		"Other existing files are opened with the system default app.",
		// Add to notes:
		"`jot open .` opens a folder browser for the current directory.",
	})
	writeExamplesSection(&b, style, []string{
		"jot open",
		"jot open .",
		"jot open dg0ftbuoqqdc-62",
		"jot open note:2026-03-19-daily.md",
		`jot open ".\docs\paper.pdf"`,
		`jot open ".\docs\plan.md"`,
		`jot open ".\data\sample.json"`,
		`jot open ".\infra\docker-compose.yaml"`,
		`jot open ".\data\report.csv"`,
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

func renderTaskHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot task", "Discover and run terminal-first tasks without leaving the current folder.")
	writeUsageSection(&b, style, []string{
		"jot task",
		"jot task convert",
		"jot task minify",
		"jot task encode",
		"jot task hash",
		"jot task compress",
		"jot task timestamp",
		"jot task uuid",
		"jot task resize",
		"jot task diff",
		"jot task rename",
		"jot task qr",
		"jot task strip",
		"jot task palette",
	}, []string{
		"`jot task` is the guided front door for jot's task layer.",
		"Available guided tasks today include image conversion, JSON minify, base64 encode/decode, hashing, compression, timestamp conversion, ID generation, resize, diff, rename, QR generation, metadata strip, and palette extraction.",
		"After a task runs, jot prints the equivalent direct command so the terminal shortcut becomes the habit.",
	})
	writeExamplesSection(&b, style, []string{
		"jot task",
		"jot task convert",
		"jot task minify",
		"jot task hash",
		"jot task resize",
		"jot task palette",
		"jot convert logo.png ico",
		"jot minify data.json --pretty",
		"jot uuid --count 3",
		"jot diff before.txt after.txt --viewer",
		"jot convert screenshot.png jpg",
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
	b.WriteString("\n")
	b.WriteString(style.title(title))
	b.WriteString("\n")
	const ruleWidth = 44
	b.WriteString(style.dim(strings.Repeat("─", ruleWidth)))
	b.WriteString("\n")
	if description != "" {
		b.WriteString(style.dim(description))
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

// helpColumnWidth is the fixed left-column width for commands and flags.
// It must be wider than the longest command/flag name so descriptions align.
const helpColumnWidth = 22

func writeCommandSection(b *strings.Builder, style helpStyler, commands []helpCommand) {
	b.WriteString(style.section("Commands"))
	b.WriteString("\n")
	for _, cmd := range commands {
		name := style.command(cmd.name)
		// Pad the raw name (without ANSI codes) to helpColumnWidth.
		pad := helpColumnWidth - len(cmd.name)
		if pad < 2 {
			pad = 2
		}
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(style.dim(cmd.description))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeFlagSection(b *strings.Builder, style helpStyler, flags []helpFlag) {
	b.WriteString(style.section("Options"))
	b.WriteString("\n")
	for _, f := range flags {
		name := style.command(f.name)
		pad := helpColumnWidth - len(f.name)
		if pad < 2 {
			pad = 2
		}
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(style.dim(f.description))
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
	b.WriteString("\n")
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
	return `HKCU\Software\Classes\*\shell\jot`
}

func installWindowsContextMenu(exePath string, run commandRunner) error {
	key := windowsContextMenuKey()
	// Use __viewer directly — opens the file without needing a parent process
	command := fmt.Sprintf(`"%s" __viewer "%%1"`, exePath)

	// Set the display label
	if err := run("reg", "add", key, "/ve", "/d", "Open with jot", "/f"); err != nil {
		return err
	}
	// Icon pulled from the exe itself
	if err := run("reg", "add", key, "/v", "Icon", "/t", "REG_SZ", "/d", exePath+",0", "/f"); err != nil {
		return err
	}
	// MUIVerb makes Windows 11 show it in the modern menu
	if err := run("reg", "add", key, "/v", "MUIVerb", "/t", "REG_SZ", "/d", "Open with jot", "/f"); err != nil {
		return err
	}
	// Remove Extended flag — without this it only shows in legacy menu with Shift+right-click
	// We do this by making sure the value does NOT exist (delete is fine to fail)
	_ = run("reg", "delete", key, "/v", "Extended", "/f")

	// The actual command
	return run("reg", "add", key+`\command`, "/ve", "/t", "REG_SZ", "/d", command, "/f")
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
	if target == "." || target == "/" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		target = wd
	}
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
		return true, launchViewer(absPath, openURL)
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
	launchPath, cleanupPath, err := prepareViewerExecutableForLaunch(executablePath, runtime.GOOS, os.TempDir, copyFile)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(launchPath, "__viewer", "--no-self-open", filePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	// Capture stderr so errors from the child are surfaced, not swallowed
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if cleanupPath != "" {
		cmd.Env = append(os.Environ(), viewerTempExecutableEnv+"="+cleanupPath)
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		// Child exited without printing a URL — wait for it and report why
		_ = cmd.Wait()
		msg := strings.TrimSpace(stderrBuf.String())
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", fmt.Errorf("viewer process exited unexpectedly: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func prepareViewerExecutableForLaunch(executablePath string, goos string, tempDir func() string, copy func(string, string) error) (string, string, error) {
	if goos != "windows" {
		return executablePath, "", nil
	}
	tempFile, err := os.CreateTemp(tempDir(), "jot-viewer-*.exe")
	if err != nil {
		return "", "", err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", "", err
	}
	if err := copy(executablePath, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return "", "", err
	}
	return tempPath, tempPath, nil
}

func copyFile(sourcePath string, destinationPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return err
	}

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		_ = destinationFile.Close()
		return err
	}
	return destinationFile.Close()
}

func cleanupViewerTempExecutable(goos string, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if goos != "windows" {
		_ = os.Remove(path)
		return
	}
	_ = scheduleViewerTempExecutableCleanup(path)
}

func scheduleViewerTempExecutableCleanup(path string) error {
	command := fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & del /f /q "%s"`, strings.ReplaceAll(path, `"`, `\"`))
	cmd := exec.Command("cmd", "/c", command)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func parseViewerServeArgs(args []string) (path string, selfOpen bool, err error) {
	selfOpen = true
	var pathArgs []string
	for _, arg := range args {
		if arg == "--no-self-open" {
			selfOpen = false
			continue
		}
		pathArgs = append(pathArgs, arg)
	}
	if len(pathArgs) != 1 {
		return "", false, errors.New("usage: jot __viewer <path>")
	}
	path = strings.TrimSpace(pathArgs[0])
	if path == "" {
		return "", false, errors.New("path must be provided")
	}
	return path, selfOpen, nil
}

func jotServeViewer(w io.Writer, args []string, now func() time.Time) error {
	path, selfOpen, err := parseViewerServeArgs(args)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return serveFolderViewer(w, path, 15*time.Minute, now, selfOpen)
	}
	return serveFileViewer(w, path, 15*time.Minute, now, selfOpen)
}

type folderFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	DocType string `json:"docType"`
}

func scanFolderFiles(dir string) ([]folderFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []folderFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		absPath := filepath.Join(dir, e.Name())
		dt := viewerDocumentTypeForPath(absPath)
		if dt == viewerDocumentTypeUnknown {
			continue
		}
		files = append(files, folderFile{
			Name:    e.Name(),
			Path:    absPath,
			DocType: string(dt),
		})
	}
	return files, nil
}

func serveFolderViewer(w io.Writer, dir string, idleTimeout time.Duration, now func() time.Time, selfOpen bool) error {
	files, err := scanFolderFiles(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported files found in %s", dir)
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
		Handler: newFolderViewerHandler(dir, files, touch),
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
	if selfOpen {
		_ = openURLInViewerWindow(viewerURL)
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

func newFolderViewerHandler(dir string, files []folderFile, touch func()) http.Handler {
	const logoPath = "/logo.png"
	mux := http.NewServeMux()

	// Main page — renders the folder browser shell
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		touch()
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, renderFolderPage(dir, files, logoPath))
	})

	// Logo
	mux.HandleFunc(logoPath, func(w http.ResponseWriter, r *http.Request) {
		touch()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(viewerLogoPNG)
	})

	// API: serve rendered HTML for a specific file by index
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		touch()
		idx, err := strconv.Atoi(r.URL.Query().Get("i"))
		if err != nil || idx < 0 || idx >= len(files) {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return
		}
		f := files[idx]
		doc, err := loadViewerDocument(f.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, renderFolderDocumentContent(doc))
	})

	// PDF bytes endpoint
	mux.HandleFunc("/pdf", func(w http.ResponseWriter, r *http.Request) {
		touch()
		idxStr := r.URL.Query().Get("i")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= len(files) {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return
		}
		f := files[idx]
		if f.DocType != string(viewerDocumentTypePDF) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", f.Name))
		http.ServeFile(w, r, f.Path)
	})

	return mux
}

type folderFileJS struct {
	Name    string `json:"name"`
	DocType string `json:"docType"`
}

func renderFolderPage(dir string, files []folderFile, logoPath string) string {
	dirName := filepath.Base(dir)
	safeDir := template.HTMLEscapeString(dirName)
	safeLogoPath := template.HTMLEscapeString(logoPath)

	// Build sidebar items JSON for the folder sidebar.
	var jsFiles []folderFileJS
	for _, f := range files {
		jsFiles = append(jsFiles, folderFileJS{Name: f.Name, DocType: f.DocType})
	}
	filesJSONBytes, _ := json.Marshal(jsFiles)
	filesJSON := string(filesJSONBytes)

	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>jot · %s</title>
  <link rel="icon" type="image/png" href="%s">
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    :root {
      font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", sans-serif;
      -webkit-font-smoothing: antialiased;
      background: #f7f6f3;
      color: #1a1a18;
      font-size: 14px;
    }
    body { min-height: 100vh; display: grid; grid-template-rows: 48px 1fr; }

    /* Header */
    header {
      display: flex; align-items: center; justify-content: space-between;
      padding: 0 14px; height: 48px;
      background: rgba(252,251,249,0.92);
      border-bottom: 0.5px solid rgba(0,0,0,0.08);
      backdrop-filter: blur(12px);
      position: sticky; top: 0; z-index: 20;
    }
    .brand { display: flex; align-items: center; gap: 10px; min-width: 0; }
    .brand-mark { width: 26px; height: 26px; border-radius: 7px; object-fit: cover; flex: none; }
    .brand-name { font-size: 11px; font-weight: 500; letter-spacing: 0.05em; text-transform: uppercase; color: rgba(26,26,24,0.38); }
    .brand-sep { width: 0.5px; height: 14px; background: rgba(0,0,0,0.12); }
    .dir-name { font-size: 13px; font-weight: 500; color: #1a1a18; }
    .file-count { font-size: 11px; font-weight: 500; padding: 2px 8px; border-radius: 5px; background: rgba(26,26,24,0.06); color: rgba(26,26,24,0.45); }

    /* Layout */
    .layout { display: grid; grid-template-columns: 220px 1fr; height: calc(100vh - 48px); overflow: hidden; }

    /* Sidebar */
    .sidebar {
      border-right: 0.5px solid rgba(0,0,0,0.08);
      background: rgba(250,249,246,0.97);
      display: flex; flex-direction: column;
      overflow: hidden;
    }
    .sidebar-header {
      padding: 10px 14px 8px;
      border-bottom: 0.5px solid rgba(0,0,0,0.06);
      font-size: 10px; font-weight: 600;
      letter-spacing: 0.08em; text-transform: uppercase;
      color: rgba(26,26,24,0.35);
      flex: none;
    }
    .sidebar-list { flex: 1; overflow-y: auto; padding: 6px 0; }
    .sidebar-item {
      display: flex; align-items: center; gap: 9px;
      padding: 7px 14px;
      cursor: pointer;
      border-left: 2px solid transparent;
      transition: background 0.1s, color 0.1s;
      min-width: 0;
    }
    .sidebar-item:hover { background: rgba(26,26,24,0.05); }
    .sidebar-item.active {
      background: rgba(26,26,24,0.06);
      border-left-color: #1a1a18;
    }
    .sidebar-item.active .item-name { color: #1a1a18; font-weight: 500; }
    .item-icon {
      font-size: 10px; font-weight: 600; padding: 2px 5px;
      border-radius: 4px; flex: none;
      font-family: "SF Mono", Consolas, monospace;
      letter-spacing: 0.02em;
    }
    .icon-md   { background: rgba(26,111,184,0.1); color: #1a6fb8; }
    .icon-json { background: rgba(184,92,26,0.1);  color: #b85c1a; }
    .icon-xml  { background: rgba(45,125,68,0.1);  color: #2d7d44; }
    .icon-pdf  { background: rgba(180,30,30,0.1);  color: #b41e1e; }
    .item-name {
      font-size: 12.5px; color: rgba(26,26,24,0.7);
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    }

    /* Main content area */
    .content-area { overflow: auto; position: relative; }
    .content-frame {
      width: 100%%; height: 100%%;
      border: none; display: block;
      background: #f7f6f3;
    }

    /* Loading state */
    .loading {
      display: flex; align-items: center; justify-content: center;
      height: 100%%; color: rgba(26,26,24,0.35); font-size: 13px; gap: 10px;
    }
    .loading-dot {
      width: 6px; height: 6px;
	  border-radius: 50%%;
      background: rgba(26,26,24,0.2);
      animation: pulse 1.2s ease-in-out infinite;
    }
    .loading-dot:nth-child(2) { animation-delay: 0.2s; }
    .loading-dot:nth-child(3) { animation-delay: 0.4s; }
    @keyframes pulse { 0%%,80%%,100%% { opacity: 0.3; } 40%% { opacity: 1; } }

    @media (max-width: 600px) {
      .layout { grid-template-columns: 160px 1fr; }
      .brand-name, .brand-sep { display: none; }
    }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <img class="brand-mark" src="%s" alt="jot">
      <span class="brand-name">jot</span>
      <div class="brand-sep"></div>
      <span class="dir-name">%s</span>
    </div>
    <span class="file-count" id="fileCount"></span>
  </header>
  <div class="layout">
    <div class="sidebar">
      <div class="sidebar-header">Files</div>
      <div class="sidebar-list" id="sidebarList"></div>
    </div>
    <div class="content-area" id="contentArea">
      <div class="loading">
        <div class="loading-dot"></div>
        <div class="loading-dot"></div>
        <div class="loading-dot"></div>
      </div>
    </div>
  </div>
<script>
(function() {
  var files = %s;
  var cur = -1;
  var sl = document.getElementById('sidebarList');
  var ca = document.getElementById('contentArea');
  document.getElementById('fileCount').textContent =
    files.length + ' file' + (files.length !== 1 ? 's' : '');

  var icons  = {markdown:'icon-md', json:'icon-json', xml:'icon-xml', yaml:'icon-json', toml:'icon-json', csv:'icon-json', env:'icon-json', text:'icon-md', pdf:'icon-pdf'};
  var labels = {markdown:'md', json:'json', xml:'xml', yaml:'yaml', toml:'toml', csv:'csv', env:'env', text:'txt', pdf:'pdf'};

  files.forEach(function(f, i) {
    var el = document.createElement('div');
    el.className = 'sidebar-item';
    el.id = 'item-' + i;
    el.innerHTML =
      '<span class="item-icon ' + (icons[f.docType]||'icon-md') + '">' +
      (labels[f.docType]||'?') + '</span>' +
      '<span class="item-name">' +
      f.name.replace(/&/g,'&amp;').replace(/</g,'&lt;') + '</span>';
    el.addEventListener('click', function() { load(i); });
    sl.appendChild(el);
  });

  function load(i) {
    if (i === cur) return;
    if (cur >= 0) {
      var p = document.getElementById('item-' + cur);
      if (p) p.classList.remove('active');
    }
    cur = i;
    var n = document.getElementById('item-' + i);
    if (n) { n.classList.add('active'); n.scrollIntoView({block:'nearest'}); }
    ca.innerHTML = '<iframe style="width:100%%;height:100%%;border:none;display:block;" src="/file?i=' + i + '"></iframe>';
  }

  if (files.length > 0) load(0);
})();
</script>
</body>
</html>
`, safeDir, safeLogoPath, safeLogoPath, safeDir, filesJSON)
}

func renderFolderDocumentContent(doc viewerDocument) string {
	// Returns a complete HTML page — loaded in an iframe, not injected as innerHTML.
	// This means scripts execute and CSS applies correctly without any extra wiring.
	const docLogoPath = "/logo.png"
	const docDocumentPath = "/document.pdf"
	return renderViewerPage(doc, docDocumentPath, docLogoPath)
}

type viewerDocumentType string

const (
	viewerDocumentTypeUnknown  viewerDocumentType = ""
	viewerDocumentTypePDF      viewerDocumentType = "pdf"
	viewerDocumentTypeMarkdown viewerDocumentType = "markdown"
	viewerDocumentTypeJSON     viewerDocumentType = "json"
	viewerDocumentTypeXML      viewerDocumentType = "xml"
	viewerDocumentTypeYAML     viewerDocumentType = "yaml"
	viewerDocumentTypeTOML     viewerDocumentType = "toml"
	viewerDocumentTypeCSV      viewerDocumentType = "csv"
	viewerDocumentTypeEnv      viewerDocumentType = "env"
	viewerDocumentTypeText     viewerDocumentType = "text"
)

type viewerDocument struct {
	path              string
	fileName          string
	docType           viewerDocumentType
	content           string
	structuredContent string
	csvTable          *viewerCSVTable
}

type viewerCSVTable struct {
	Headers   []string
	Rows      [][]string
	TotalRows int
	Truncated bool
}

func viewerDocumentTypeForPath(path string) viewerDocumentType {
	lowerName := strings.ToLower(filepath.Base(path))
	switch {
	case lowerName == ".env" || strings.HasPrefix(lowerName, ".env."):
		return viewerDocumentTypeEnv
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return viewerDocumentTypePDF
	case ".md", ".markdown":
		return viewerDocumentTypeMarkdown
	case ".json":
		return viewerDocumentTypeJSON
	case ".xml":
		return viewerDocumentTypeXML
	case ".yaml", ".yml":
		return viewerDocumentTypeYAML
	case ".toml":
		return viewerDocumentTypeTOML
	case ".csv":
		return viewerDocumentTypeCSV
	case ".txt", ".log", ".jsonl":
		return viewerDocumentTypeText
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
	switch docType {
	case viewerDocumentTypeJSON:
		doc.structuredContent = doc.content
	case viewerDocumentTypeYAML:
		payload, err := yamlToStructuredJSON(doc.content)
		if err == nil {
			doc.structuredContent = payload
		}
	case viewerDocumentTypeTOML:
		payload, err := tomlToStructuredJSON(doc.content)
		if err == nil {
			doc.structuredContent = payload
		}
	case viewerDocumentTypeCSV:
		table, err := buildCSVTable(doc.content, 500)
		if err == nil {
			doc.csvTable = table
		}
	}
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

func serveFileViewer(w io.Writer, path string, idleTimeout time.Duration, now func() time.Time, selfOpen bool) error {
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

	// Always print the URL (terminal flow reads this via pipe)
	if _, err := fmt.Fprintln(w, viewerURL); err != nil {
		_ = server.Close()
		<-serverErr
		return err
	}
	if file, ok := w.(*os.File); ok {
		_ = file.Sync()
	}

	// Also open the browser directly — this is what makes Explorer launch work.
	// When called from the terminal via startViewerProcess, the parent opens the
	// browser from the URL it reads. When called directly from Explorer via the
	// registry command, nobody reads stdout, so we open it ourselves here.
	// Opening twice is harmless — browsers deduplicate tabs to the same localhost URL.
	if selfOpen {
		_ = openURLInViewerWindow(viewerURL)
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
	const logoPath = "/logo.png"
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
		_, _ = io.WriteString(w, renderViewerPage(doc, documentPath, logoPath))
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
	mux.HandleFunc(logoPath, func(w http.ResponseWriter, r *http.Request) {
		touch()
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(viewerLogoPNG)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		touch()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func renderViewerPage(doc viewerDocument, documentPath string, logoPath string) string {
	safeTitle := template.HTMLEscapeString(doc.fileName)
	safeDocumentPath := template.HTMLEscapeString(documentPath)
	safeLogoPath := template.HTMLEscapeString(logoPath)
	bodyClass := "viewer-body viewer-body-text"
	contentHTML := renderViewerContent(doc, safeDocumentPath)
	if doc.docType == viewerDocumentTypePDF {
		bodyClass = "viewer-body viewer-body-pdf"
	}
	var tocShell string
	if viewerDocumentUsesStructuredTree(doc) {
		tocShell = `
	<div class="toc-trigger"></div>
	<div class="toc-panel">
	<div class="toc-header">
		<span>Top-level fields</span>
		<button class="toc-close">&#x2715;</button>
	</div>
	<button class="toc-expand-btn">Expand all</button>
	<nav class="toc-list"></nav>
	</div>`
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>jot · %s</title>
  <link rel="icon" type="image/png" href="%s">
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    :root {
      font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", sans-serif;
      -webkit-font-smoothing: antialiased;
      background: #f7f6f3;
      color: #1a1a18;
      font-size: 14px;
    }
    body {
      min-height: 100vh;
      display: grid;
      grid-template-rows: 48px 1fr;
    }
	body.toc-open main {
		padding-right: 240px;
	}
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0 14px;
      height: 48px;
      background: rgba(252, 251, 249, 0.92);
      border-bottom: 0.5px solid rgba(0, 0, 0, 0.08);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      position: sticky;
      top: 0;
      z-index: 10;
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 10px;
      min-width: 0;
    }
    .brand-mark {
      width: 26px;
      height: 26px;
      border-radius: 7px;
      object-fit: cover;
      flex: none;
    }
    .brand-name {
      font-size: 11px;
      font-weight: 500;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: rgba(26, 26, 24, 0.38);
    }
    .brand-sep {
      width: 0.5px;
      height: 14px;
      background: rgba(0, 0, 0, 0.12);
    }
    .file-name {
      font-size: 13px;
      font-weight: 500;
      color: #1a1a18;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 36ch;
    }
    .hint {
      font-size: 11px;
      font-weight: 500;
      padding: 2px 8px;
      border-radius: 5px;
      background: rgba(26, 26, 24, 0.06);
      color: rgba(26, 26, 24, 0.45);
      letter-spacing: 0.02em;
      white-space: nowrap;
      flex: none;
    }
    main {
      padding: 16px;
	  padding-right: 36px;
      overflow: auto;
	  transition: padding-right 0.22s cubic-bezier(0.4, 0, 0.2, 1);
    }
    .viewer-surface {
      background: rgba(252, 251, 249, 0.97);
      border: 0.5px solid rgba(0, 0, 0, 0.08);
      border-radius: 12px;
      overflow: auto;
    }
    .viewer-body-pdf .viewer-surface {
      border-radius: 12px;
      overflow: hidden;
      padding: 0;
    }
    iframe {
      display: block;
      width: 100%%;
      height: calc(100vh - 80px);
      border: 0;
      background: white;
    }
.text-frame {
  max-width: 680px;
  margin: 0 auto;
  padding: 40px 44px 60px;
  line-height: 1.8;
  color: rgba(26, 26, 24, 0.78);
  font-size: 15px;
  transition: padding-right 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}
body.toc-open .text-frame {
  padding-right: 250px;
}
.text-frame h1 {
  font-size: 26px;
  font-weight: 700;
  color: #1a1a18;
  letter-spacing: -0.03em;
  line-height: 1.2;
  margin: 0 0 6px;
}
.text-frame h2 {
  font-size: 18px;
  font-weight: 600;
  color: #1a1a18;
  letter-spacing: -0.02em;
  line-height: 1.3;
  margin: 2.2em 0 0.5em;
  padding-bottom: 6px;
  border-bottom: 0.5px solid rgba(0,0,0,0.08);
}
.text-frame h3 {
  font-size: 15px;
  font-weight: 600;
  color: #1a1a18;
  margin: 1.8em 0 0.4em;
}
.text-frame h4, .text-frame h5, .text-frame h6 {
  font-size: 14px;
  font-weight: 600;
  color: rgba(26,26,24,0.7);
  margin: 1.4em 0 0.4em;
}
.text-frame p { margin: 0 0 1em; }
.text-frame ul, .text-frame ol {
  margin: 0 0 1em 1.5em;
  padding: 0;
}
.text-frame li { margin: 0.4em 0; line-height: 1.7; }
.text-frame li > ul, .text-frame li > ol { margin-top: 0.3em; margin-bottom: 0.3em; }
.text-frame .task-list-item {
  list-style: none;
  margin-left: -1.5em;
}
.text-frame .task-list-label {
  display: inline-flex;
  align-items: flex-start;
  gap: 0.65em;
}
.text-frame .task-checkbox {
  margin: 0.28em 0 0;
  inline-size: 15px;
  block-size: 15px;
  accent-color: #1a6fb8;
}
.text-frame strong { font-weight: 600; color: #1a1a18; }
.text-frame em { font-style: italic; color: rgba(26, 26, 24, 0.65); }
.text-frame del { color: rgba(26, 26, 24, 0.45); }
.text-frame a {
  color: #1a6fb8;
  text-decoration-thickness: 1px;
  text-underline-offset: 0.22em;
  transition: color 0.12s;
}
.text-frame a:hover { color: #0f4f8a; }
.text-frame img {
  max-width: 100%%;
  border-radius: 8px;
  margin: 0.5em 0;
  display: block;
}
.text-frame blockquote {
  margin: 1.2em 0;
  padding: 10px 16px;
  border-left: 3px solid rgba(26, 26, 24, 0.12);
  color: rgba(26, 26, 24, 0.58);
  background: rgba(26, 26, 24, 0.025);
  border-radius: 0 8px 8px 0;
  font-style: italic;
}
.text-frame blockquote p { margin: 0; }
.text-frame blockquote blockquote {
  margin: 1em 0 0;
  background: rgba(26, 26, 24, 0.035);
}
.text-frame hr {
  margin: 2em 0;
  border: 0;
  border-top: 0.5px solid rgba(0, 0, 0, 0.1);
}
.text-frame .table-wrap {
  margin: 1.4em 0;
  overflow-x: auto;
  border: 0.5px solid rgba(0, 0, 0, 0.1);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.78);
}
.text-frame table {
  width: 100%%;
  border-collapse: collapse;
}
.text-frame th,
.text-frame td {
  padding: 10px 14px;
  border-bottom: 0.5px solid rgba(0, 0, 0, 0.08);
  vertical-align: top;
}
.text-frame th {
  font-weight: 600;
  color: #1a1a18;
  background: rgba(26, 26, 24, 0.04);
}
.text-frame tbody tr:last-child td {
  border-bottom: 0;
}
.csv-frame {
  padding-top: 18px;
}
.csv-frame .table-wrap {
  margin-top: 10px;
}
.viewer-meta {
  padding: 0 2px;
  font-size: 11px;
  letter-spacing: 0.02em;
  color: rgba(26, 26, 24, 0.46);
}
.text-frame code {
  padding: 2px 6px;
  border-radius: 5px;
  background: rgba(26, 26, 24, 0.07);
  font-family: "SF Mono", Consolas, "Fira Mono", monospace;
  font-size: 0.88em;
  color: #c7370a;
}
.text-frame pre {
  margin: 1.2em 0;
  padding: 18px 20px;
  border-radius: 10px;
  overflow-x: auto;
  background: #181816;
  border: 0.5px solid rgba(0, 0, 0, 0.15);
  line-height: 1.6;
}
.text-frame pre code {
  padding: 0;
  background: transparent;
  color: rgba(247, 246, 243, 0.88);
  font-size: 13px;
  border-radius: 0;
}
/* Language label on code blocks */
.text-frame pre[data-lang]::before {
  content: attr(data-lang);
  display: block;
  font-size: 10px;
  font-family: "SF Mono", Consolas, monospace;
  color: rgba(247, 246, 243, 0.3);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-bottom: 10px;
}
    .structured-block {
      white-space: pre-wrap;
      word-break: break-word;
    }
	  /* JSON / XML viewer */
		.code-frame {
		padding: 0;
		font-family: "SF Mono", Consolas, "Fira Mono", monospace;
		font-size: 13px;
		line-height: 1.65;
		overflow: auto;
		}
		.code-frame .line-table {
		width: 100%%;
		border-collapse: collapse;
		min-width: max-content;
		}
		.code-frame .ln {
		width: 1px;
		white-space: nowrap;
		padding: 0 18px 0 20px;
		text-align: right;
		color: rgba(26, 26, 24, 0.2);
		user-select: none;
		border-right: 0.5px solid rgba(0, 0, 0, 0.07);
		font-variant-numeric: tabular-nums;
		}
		.code-frame .lc {
		padding: 0 20px 0 16px;
		white-space: pre;
		}
		.code-frame .lc:hover,
		.code-frame tr:hover .ln {
		background: rgba(26, 26, 24, 0.03);
		color: rgba(26, 26, 24, 0.4);
		}
		.env-frame .viewer-meta {
		padding: 12px 20px 6px;
		}
		.env-frame .tok-env-export,
		.env-frame .tok-env-sep {
		color: rgba(26, 26, 24, 0.35);
		}
		.env-frame .tok-env-key {
		color: #1a6fb8;
		}
		.env-frame .tok-env-val {
		color: rgba(45, 125, 68, 0.62);
		transition: opacity 0.12s ease, color 0.12s ease;
		opacity: 0.55;
		}
		.env-frame tr:hover .tok-env-val {
		opacity: 0.95;
		color: rgba(45, 125, 68, 0.82);
		}
		.env-frame .tok-env-comment {
		color: rgba(26, 26, 24, 0.35);
		font-style: italic;
		}
		/* JSON token colors */
		.tok-key   { color: #1a6fb8; }
		.tok-str   { color: #2d7d44; }
		.tok-num   { color: #b85c1a; }
		.tok-bool  { color: #8b3ab8; }
		.tok-null  { color: #888; }
		.tok-punct { color: rgba(26, 26, 24, 0.35); }
		/* XML token colors */
		.tok-tag   { color: #1a6fb8; }
		.tok-attr  { color: #b85c1a; }
		.tok-val   { color: #2d7d44; }
		.tok-cmt   { color: rgba(26, 26, 24, 0.35); font-style: italic; }
	
			/* TOC */
		.toc-trigger {
		position: fixed;
		right: 0;
		top: 48px;
		bottom: 0;
		width: 20px;
		z-index: 30;
		cursor: pointer;
		}
		.toc-trigger::after {
		content: '';
		position: absolute;
		right: 6px;
		top: 50%%;
		transform: translateY(-50%%);
		width: 2px;
		height: 40px;
		background: rgba(26, 26, 24, 0.12);
		border-radius: 2px;
		transition: background 0.2s, height 0.2s;
		}
		.toc-trigger:hover::after {
		background: rgba(26, 26, 24, 0.3);
		height: 56px;
		}
		.toc-panel {
		position: fixed;
		right: 0;
		top: 48px;
		bottom: 0;
		width: 224px;
		background: rgba(252, 251, 249, 0.97);
		border-left: 0.5px solid rgba(0, 0, 0, 0.08);
		backdrop-filter: blur(16px);
		-webkit-backdrop-filter: blur(16px);
		transform: translateX(100%%);
		transition: transform 0.22s cubic-bezier(0.4, 0, 0.2, 1);
		z-index: 25;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		}
		.toc-panel.open { transform: translateX(0); }
		.toc-header {
		padding: 14px 16px 10px;
		border-bottom: 0.5px solid rgba(0, 0, 0, 0.07);
		display: flex;
		align-items: center;
		justify-content: space-between;
		}
		.toc-header span {
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: rgba(26, 26, 24, 0.35);
		}
		.toc-close {
		width: 20px;
		height: 20px;
		border-radius: 5px;
		border: none;
		background: transparent;
		cursor: pointer;
		color: rgba(26, 26, 24, 0.35);
		font-size: 13px;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background 0.15s;
		}
		.toc-close:hover { background: rgba(26, 26, 24, 0.07); color: rgba(26, 26, 24, 0.7); }
		.toc-list { flex: 1; overflow-y: auto; padding: 8px 0; }
	.toc-item {
	display: block;
	padding: 3px 16px;
	font-size: 12px;
	color: rgba(26, 26, 24, 0.45);
	text-decoration: none;
	cursor: pointer;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	border-left: 2px solid transparent;
	line-height: 1.65;
	transition: color 0.12s, background 0.12s;
	border-radius: 0 4px 4px 0;
	}
	.toc-item:hover { color: #1a1a18; background: rgba(26, 26, 24, 0.04); }
	.toc-item.active { color: #1a1a18; border-left-color: #1a1a18; font-weight: 500; background: rgba(26,26,24,0.04); }
	.toc-item.h1 { padding-left: 16px; font-size: 12px; color: rgba(26, 26, 24, 0.65); font-weight: 500; }
	.toc-item.h2 { padding-left: 24px; }
	.toc-item.h3 { padding-left: 34px; font-size: 11px; color: rgba(26,26,24,0.38); }
	.toc-item.h2.active { color: #1a6fb8; border-left-color: #1a6fb8; background: rgba(26, 111, 184, 0.04); }
	.toc-item.h3.active { color: rgba(26,26,24,0.65); border-left-color: rgba(26,26,24,0.25); }
    /* Tree viewer */
	.tree-view {
	padding: 14px 0;
	font-family: "SF Mono", Consolas, "Fira Mono", monospace;
	font-size: 12.5px;
	line-height: 1.7;
	}
	.tr { display: flex; align-items: baseline; padding: 1.5px 20px; border-left: 2px solid transparent; transition: background 0.1s; }
	.tr:hover { background: rgba(26, 26, 24, 0.04); }
	.tr.flash { background: rgba(26, 26, 24, 0.08); border-left-color: #1a1a18; transition: none; }
	.ti { display: inline-block; width: 18px; flex: none; }
	.tg { display: inline-flex; align-items: center; justify-content: center; width: 14px; height: 14px; border-radius: 3px; cursor: pointer; flex: none; color: rgba(26,26,24,0.28); font-size: 9px; margin-right: 4px; user-select: none; transition: background 0.1s; }
	.tg:hover { background: rgba(26,26,24,0.08); color: rgba(26,26,24,0.6); }
	.tg.leaf { cursor: default; }
	.tg.leaf:hover { background: transparent; }
	.tok-key   { color: #1a6fb8; }
	.tok-str   { color: #2d7d44; }
	.tok-num   { color: #b85c1a; }
	.tok-bool  { color: #8b3ab8; }
	.tok-null  { color: rgba(26,26,24,0.35); }
	.tok-punct { color: rgba(26,26,24,0.3); }
	.tok-hint  { font-size: 11px; color: rgba(26,26,24,0.3); font-style: italic; }
	.tok-tag   { color: #1a6fb8; }
	.tok-attr  { color: #b85c1a; }
	.tok-val   { color: #2d7d44; }
	.tok-cmt   { color: rgba(26,26,24,0.35); font-style: italic; }

    @media (max-width: 600px) {
      .brand-name, .brand-sep { display: none; }
      .text-frame { padding: 20px 18px; }
      main { padding: 10px; }
      iframe { height: calc(100vh - 68px); }
    }
  </style>
</head>
<body class="%s">
  <header>
    <div class="brand">
      <img class="brand-mark" src="%s" alt="jot">
      <span class="brand-name">jot</span>
      <div class="brand-sep"></div>
      <span class="file-name">%s</span>
    </div>
    <span class="hint">%s</span>
  </header>
  <main>
    <div class="viewer-surface">
      %s
    </div>
  </main>
  %s
<script>

function readViewerSource() {
var source = document.getElementById('viewer-source');
if (!source) return '';
return source.textContent;
}

(function() {

  var trigger = document.getElementById('tocTrigger');
  var panel = document.getElementById('tocPanel');
  var closeBtn = document.getElementById('tocClose');
  var items = document.querySelectorAll('.toc-item');

  if (trigger && panel) {
    trigger.addEventListener('mouseenter', function() { panel.classList.add('open'); document.body.classList.add('toc-open'); });
  }
  if (closeBtn && panel) {
    closeBtn.addEventListener('click', function() { panel.classList.remove('open'); document.body.classList.remove('toc-open'); });
  }

  items.forEach(function(item) {
    item.addEventListener('click', function() {
      var target = document.getElementById(item.dataset.target);
      if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
      items.forEach(function(i) { i.classList.remove('active'); });
      item.classList.add('active');
    });
  });

  var headings = Array.from(document.querySelectorAll('.markdown-frame h1, .markdown-frame h2, .markdown-frame h3'));
  if (headings.length > 0 && items.length > 0) {
    window.addEventListener('scroll', function() {
      var current = headings[0] && headings[0].id;
      headings.forEach(function(h) {
        if (h.getBoundingClientRect().top <= 64) current = h.id;
      });
      items.forEach(function(i) {
        i.classList.toggle('active', i.dataset.target === current);
      });
    });
  }
})();
</script>
<script>
(function() {
  var _nid = 0;
  var _nodes = {};

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  function typeOf(v) {
    if (v === null) return 'null';
    if (Array.isArray(v)) return 'array';
    return typeof v;
  }

  function buildJSONTree(data, container) {
    var keys = Object.keys(data);
    var tocItems = [];
    keys.forEach(function(k, i) {
      var id = 'jn' + (_nid++);
      tocItems.push({ id: id, key: k, type: typeOf(data[k]) });
      container.appendChild(buildJSONNode(k, data[k], 0, i === keys.length - 1, id));
    });
    mountTOC(tocItems, false);
  }
  window.buildJSONTree = buildJSONTree;

  function buildJSONNode(key, value, depth, isLast, forceId) {
    var id = forceId || ('jn' + (_nid++));
    var t = typeOf(value);
    var isComplex = t === 'object' || t === 'array';
    var open = depth < 2;
    _nodes[id] = { open: open, isComplex: isComplex };

    var wrap = document.createElement('div');
    wrap.className = 'tree-node';
    wrap.id = 'node-' + id;

    var row = document.createElement('div');
    row.className = 'tr';
    row.id = 'row-' + id;

    for (var i = 0; i < depth; i++) {
      var sp = document.createElement('span');
      sp.className = 'ti';
      row.appendChild(sp);
    }

    var tog = document.createElement('span');
    tog.className = isComplex && Object.keys(value || {}).length > 0 ? 'tg' : 'tg leaf';
    tog.id = 'tog-' + id;
    if (isComplex && Object.keys(value || {}).length > 0) {
      tog.textContent = open ? '▼' : '▶';
      if (open) tog.style.color = 'rgba(26,26,24,0.5)';
      tog.onclick = (function(nid) { return function() { toggleJSON(nid); }; })(id);
    }
    row.appendChild(tog);

    if (key !== null) {
      var kspan = document.createElement('span');
      kspan.className = 'tok-key';
      kspan.textContent = '"' + key + '"';
      row.appendChild(kspan);
      var colon = document.createElement('span');
      colon.className = 'tok-punct';
      colon.textContent = ': ';
      row.appendChild(colon);
    }

    var brackets = t === 'array' ? ['[',']'] : ['{','}'];
    var count = isComplex ? Object.keys(value || {}).length : 0;
    var comma = isLast ? '' : ',';

    if (!isComplex) {
      var vspan = document.createElement('span');
      if (t === 'string') { vspan.className = 'tok-str'; vspan.textContent = '"' + value + '"'; }
      else if (t === 'number') { vspan.className = 'tok-num'; vspan.textContent = value; }
      else if (t === 'boolean') { vspan.className = 'tok-bool'; vspan.textContent = value; }
      else { vspan.className = 'tok-null'; vspan.textContent = 'null'; }
      row.appendChild(vspan);
      if (comma) { var cp = document.createElement('span'); cp.className='tok-punct'; cp.textContent=comma; row.appendChild(cp); }
    } else {
      var ob = document.createElement('span');
      ob.className = 'tok-punct';
      ob.textContent = brackets[0];
      row.appendChild(ob);

      if (!open && count > 0) {
        var hint = document.createElement('span');
        hint.className = 'tok-hint';
        hint.id = 'hint-' + id;
        hint.textContent = ' ' + count + (t === 'array' ? ' items' : ' keys') + ' ';
        row.appendChild(hint);
        var cb2 = document.createElement('span');
        cb2.className = 'tok-punct';
        cb2.id = 'cb-' + id;
        cb2.textContent = brackets[1] + comma;
        row.appendChild(cb2);
      }
    }

    wrap.appendChild(row);

    if (isComplex && count > 0) {
      var children = document.createElement('div');
      children.id = 'ch-' + id;
      children.style.display = open ? '' : 'none';
      var childKeys = Object.keys(value);
      childKeys.forEach(function(ck, ci) {
        children.appendChild(buildJSONNode(t === 'array' ? null : ck, value[ck], depth + 1, ci === childKeys.length - 1, null));
      });
      // closing bracket
      var closingRow = document.createElement('div');
      closingRow.className = 'tr';
      for (var di = 0; di < depth; di++) {
        var dsp = document.createElement('span'); dsp.className='ti'; closingRow.appendChild(dsp);
      }
      var dummyTog = document.createElement('span'); dummyTog.className='tg leaf'; closingRow.appendChild(dummyTog);
      var closingBracket = document.createElement('span');
      closingBracket.className = 'tok-punct';
      closingBracket.id = 'closebr-' + id;
      closingBracket.textContent = brackets[1] + comma;
      closingRow.appendChild(closingBracket);
      children.appendChild(closingRow);

      if (open) {
        // add open bracket inline without hint
        ob.textContent = brackets[0];
      }

      wrap.appendChild(children);
    }

    return wrap;
  }

  function toggleJSON(id) {
    var node = _nodes[id];
    if (!node) return;
    node.open = !node.open;
    var ch = document.getElementById('ch-' + id);
    var tog = document.getElementById('tog-' + id);
    var hint = document.getElementById('hint-' + id);
    var cb = document.getElementById('cb-' + id);
    var closebr = document.getElementById('closebr-' + id);
    if (ch) ch.style.display = node.open ? '' : 'none';
    if (tog) { tog.textContent = node.open ? '▼' : '▶'; tog.style.color = node.open ? 'rgba(26,26,24,0.5)' : ''; }
    if (hint) hint.style.display = node.open ? 'none' : '';
    if (cb) cb.style.display = node.open ? 'none' : '';
    if (closebr) closebr.parentElement.style.display = node.open ? '' : 'none';
  }
  window.toggleJSON = toggleJSON;

  // XML tree
  function buildXMLTree(container) {
    var raw = container.getAttribute('data-xml');
    if (!raw) return;
    var parser = new DOMParser();
    var doc = parser.parseFromString(raw, 'text/xml');
    var root = doc.documentElement;
    var tocItems = [];
    Array.from(root.childNodes).forEach(function(child, i) {
      if (child.nodeType === 1) {
        var id = 'xn' + (_nid++);
        tocItems.push({ id: id, key: child.tagName, type: 'element' });
        container.appendChild(buildXMLNode(child, 0, i === root.childNodes.length - 1, id));
      }
    });
    mountTOC(tocItems, true);
  }
  window.buildXMLTree = buildXMLTree;

  function buildXMLNode(el, depth, isLast, forceId) {
    var id = forceId || ('xn' + (_nid++));
    var hasChildren = Array.from(el.childNodes).some(function(n) { return n.nodeType === 1; });
    var textContent = !hasChildren ? el.textContent.trim() : '';
    var open = depth < 2;
    _nodes[id] = { open: open, isComplex: hasChildren };

    var wrap = document.createElement('div');
    wrap.className = 'tree-node';
    wrap.id = 'node-' + id;

    var row = document.createElement('div');
    row.className = 'tr';
    row.id = 'row-' + id;

    for (var i = 0; i < depth; i++) {
      var sp = document.createElement('span'); sp.className='ti'; row.appendChild(sp);
    }

    var tog = document.createElement('span');
    tog.className = hasChildren ? 'tg' : 'tg leaf';
    tog.id = 'tog-' + id;
    if (hasChildren) {
      tog.textContent = open ? '▼' : '▶';
      if (open) tog.style.color = 'rgba(26,26,24,0.5)';
      tog.onclick = (function(nid) { return function() { toggleJSON(nid); }; })(id);
    }
    row.appendChild(tog);

    // opening tag
    var tagOpen = document.createElement('span');
    tagOpen.innerHTML = '<span class="tok-punct">&lt;</span><span class="tok-tag">' + esc(el.tagName) + '</span>';
    // attributes
    Array.from(el.attributes).forEach(function(attr) {
      tagOpen.innerHTML += ' <span class="tok-attr">' + esc(attr.name) + '</span><span class="tok-punct">=</span><span class="tok-val">"' + esc(attr.value) + '"</span>';
    });
    if (!hasChildren && !textContent) {
      tagOpen.innerHTML += '<span class="tok-punct"> /&gt;</span>';
    } else {
      tagOpen.innerHTML += '<span class="tok-punct">&gt;</span>';
    }
    row.appendChild(tagOpen);

    if (textContent) {
      var tv = document.createElement('span');
      tv.className = 'tok-str';
      tv.textContent = textContent;
      row.appendChild(tv);
      var closeTag = document.createElement('span');
      closeTag.innerHTML = '<span class="tok-punct">&lt;/</span><span class="tok-tag">' + esc(el.tagName) + '</span><span class="tok-punct">&gt;</span>';
      row.appendChild(closeTag);
    }

    if (!open && hasChildren) {
      var childEls = Array.from(el.childNodes).filter(function(n) { return n.nodeType === 1; });
      var hint = document.createElement('span');
      hint.className = 'tok-hint';
      hint.id = 'hint-' + id;
      hint.textContent = ' ' + childEls.length + ' children ';
      row.appendChild(hint);
      var cb = document.createElement('span');
      cb.className = 'tok-punct';
      cb.id = 'cb-' + id;
      cb.innerHTML = '&lt;/' + esc(el.tagName) + '&gt;';
      row.appendChild(cb);
    }

    wrap.appendChild(row);

    if (hasChildren) {
      var children = document.createElement('div');
      children.id = 'ch-' + id;
      children.style.display = open ? '' : 'none';
      var childEls2 = Array.from(el.childNodes).filter(function(n) { return n.nodeType === 1; });
      childEls2.forEach(function(child, ci) {
        children.appendChild(buildXMLNode(child, depth + 1, ci === childEls2.length - 1, null));
      });
      // closing tag row
      var closingRow = document.createElement('div');
      closingRow.className = 'tr';
      closingRow.id = 'closebr-' + id;
      for (var di = 0; di < depth; di++) {
        var dsp2 = document.createElement('span'); dsp2.className='ti'; closingRow.appendChild(dsp2);
      }
      var dt2 = document.createElement('span'); dt2.className='tg leaf'; closingRow.appendChild(dt2);
      var closingTag = document.createElement('span');
      closingTag.innerHTML = '<span class="tok-punct">&lt;/</span><span class="tok-tag">' + esc(el.tagName) + '</span><span class="tok-punct">&gt;</span>';
      closingRow.appendChild(closingTag);
      children.appendChild(closingRow);
      wrap.appendChild(children);
    }

    return wrap;
  }

  function mountTOC(items, isXML) {
    var trigger = document.querySelector('.toc-trigger');
    var panel = document.querySelector('.toc-panel');
    var closeBtn = document.querySelector('.toc-close');
    var tocList = document.querySelector('.toc-list');
    var expandBtn = document.querySelector('.toc-expand-btn');
    if (!trigger || !panel) return;

    trigger.addEventListener('mouseenter', function() { panel.classList.add('open'); });
    closeBtn.addEventListener('click', function() { panel.classList.remove('open'); });

    items.forEach(function(item) {
      var el = document.createElement('div');
      el.className = 'toc-item';
      var typeStr = isXML ? 'el' : (item.type === 'object' ? '{}' : item.type === 'array' ? '[]' : item.type[0]);
      el.innerHTML = '<span class="toc-type-tag">' + typeStr + '</span>' + esc(item.key);
      el.addEventListener('click', function() {
        var row = document.getElementById('row-' + item.id);
        if (row) {
          row.scrollIntoView({ behavior: 'smooth', block: 'start' });
          row.classList.add('flash');
          setTimeout(function() { row.classList.remove('flash'); }, 1200);
        }
        document.querySelectorAll('.toc-item').forEach(function(i) { i.classList.remove('active'); });
        el.classList.add('active');
        panel.classList.remove('open');
      });
      tocList.appendChild(el);
    });

    var allExpanded = false;
    if (expandBtn) {
      expandBtn.addEventListener('click', function() {
        allExpanded = !allExpanded;
        expandBtn.textContent = allExpanded ? 'Collapse all' : 'Expand all';
        Object.keys(_nodes).forEach(function(id) {
          var node = _nodes[id];
          if (!node.isComplex) return;
          node.open = allExpanded;
          var ch = document.getElementById('ch-' + id);
          var tog2 = document.getElementById('tog-' + id);
          var hint2 = document.getElementById('hint-' + id);
          var cb2 = document.getElementById('cb-' + id);
          var closebr2 = document.getElementById('closebr-' + id);
          if (ch) ch.style.display = allExpanded ? '' : 'none';
          if (tog2) { tog2.textContent = allExpanded ? '▼' : '▶'; tog2.style.color = allExpanded ? 'rgba(26,26,24,0.5)' : ''; }
          if (hint2) hint2.style.display = allExpanded ? 'none' : '';
          if (cb2) cb2.style.display = allExpanded ? 'none' : '';
          if (closebr2) closebr2.style.display = allExpanded ? '' : 'none';
        });
      });
    }
  }

  var jsonRoot = document.getElementById('json-root');
  if (jsonRoot) {
    var jsonRaw = readViewerSource();
    if (jsonRaw) {
      try {
        buildJSONTree(JSON.parse(jsonRaw), jsonRoot);
      } catch (err) {
        jsonRoot.innerHTML = '<div class="text-frame"><p>Could not render JSON: ' + err.message + '</p></div>';
      }
    }
  }

var xmlRoot = document.getElementById('xml-root');
if (xmlRoot) {
	var xmlRaw = readViewerSource();
	if (xmlRaw) {
	try {
		xmlRoot.setAttribute('data-xml', xmlRaw);
		buildXMLTree(xmlRoot);
    } catch (err) {
      xmlRoot.innerHTML = '<div class="text-frame"><p>Could not render XML: ' + err.message + '</p></div>';
    }
  }
}
})();
</script>
</body>
</html>
`, safeTitle, safeLogoPath, bodyClass, safeLogoPath, safeTitle, template.HTMLEscapeString(viewerDocumentHint(doc.docType)), contentHTML, tocShell)
}

func renderViewerContent(doc viewerDocument, safeDocumentPath string) string {
	switch doc.docType {
	case viewerDocumentTypePDF:
		return fmt.Sprintf(`<iframe src="%s" title="%s"></iframe>`, safeDocumentPath, template.HTMLEscapeString(doc.fileName))
	case viewerDocumentTypeMarkdown:
		toc := extractTOC(doc.content)
		article := `<article class="text-frame markdown-frame">` + renderMarkdownHTML(doc.content) + `</article>`
		return article + renderTOC(toc)
	case viewerDocumentTypeJSON:
		// Do NOT HTML-escape — script tag content is not HTML.
		// Only escape </script> to prevent premature tag closure.
		return renderStructuredViewerPayload(doc.content)
	case viewerDocumentTypeXML:
		safeContent := strings.ReplaceAll(doc.content, "</script>", `<\/script>`)
		return fmt.Sprintf(`<script type="application/xml" id="viewer-source">%s</script><div id="xml-root" class="tree-view"></div>`, safeContent)
	case viewerDocumentTypeYAML, viewerDocumentTypeTOML:
		if doc.structuredContent != "" {
			return renderStructuredViewerPayload(doc.structuredContent)
		}
		return `<div class="code-frame">` + renderCodeWithLineNumbers(doc.content, "") + `</div>`
	case viewerDocumentTypeCSV:
		if doc.csvTable != nil {
			return renderCSVTableHTML(*doc.csvTable)
		}
		return `<div class="code-frame">` + renderCodeWithLineNumbers(doc.content, "") + `</div>`
	case viewerDocumentTypeEnv:
		return renderEnvHTML(doc.content)
	case viewerDocumentTypeText:
		return `<div class="code-frame">` + renderCodeWithLineNumbers(doc.content, "") + `</div>`
	default:
		return `<div class="text-frame"><p>Preview not available.</p></div>`
	}
}

func renderStructuredViewerPayload(content string) string {
	safeContent := strings.ReplaceAll(content, "</script>", `<\/script>`)
	return fmt.Sprintf(`<script type="application/json" id="viewer-source">%s</script><div id="json-root" class="tree-view"></div>`, safeContent)
}

func renderCodeWithLineNumbers(content string, lang string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	// Trim trailing empty line if file ends with newline
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder
	b.WriteString(`<table class="line-table">`)
	for i, line := range lines {
		ln := i + 1
		var highlighted string
		switch lang {
		case "json":
			highlighted = highlightJSONLine(line)
		case "xml":
			highlighted = highlightXMLLine(line)
		default:
			highlighted = template.HTMLEscapeString(line)
		}
		fmt.Fprintf(&b, `<tr><td class="ln">%d</td><td class="lc">%s</td></tr>`, ln, highlighted)
	}
	b.WriteString(`</table>`)
	return b.String()
}

func highlightJSONLine(line string) string {
	// Preserve indentation, then tokenize the rest
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]
	if trimmed == "" {
		return template.HTMLEscapeString(indent)
	}

	var b strings.Builder
	b.WriteString(template.HTMLEscapeString(indent))

	s := trimmed
	for len(s) > 0 {
		// String (key or value)
		if s[0] == '"' {
			end := 1
			for end < len(s) {
				if s[end] == '\\' {
					end += 2
					continue
				}
				if s[end] == '"' {
					end++
					break
				}
				end++
			}
			strContent := s[:end]
			rest := strings.TrimLeft(s[end:], " \t")
			// If followed by ':', it's a key
			if strings.HasPrefix(rest, ":") {
				fmt.Fprintf(&b, `<span class="tok-key">%s</span>`, template.HTMLEscapeString(strContent))
			} else {
				fmt.Fprintf(&b, `<span class="tok-str">%s</span>`, template.HTMLEscapeString(strContent))
			}
			s = s[end:]
			continue
		}
		// Number
		if s[0] >= '0' && s[0] <= '9' || (s[0] == '-' && len(s) > 1 && s[1] >= '0' && s[1] <= '9') {
			end := 1
			for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.' || s[end] == 'e' || s[end] == 'E' || s[end] == '+' || s[end] == '-') {
				end++
			}
			fmt.Fprintf(&b, `<span class="tok-num">%s</span>`, template.HTMLEscapeString(s[:end]))
			s = s[end:]
			continue
		}
		// Booleans
		if strings.HasPrefix(s, "true") || strings.HasPrefix(s, "false") {
			end := 4
			if s[0] == 'f' {
				end = 5
			}
			fmt.Fprintf(&b, `<span class="tok-bool">%s</span>`, s[:end])
			s = s[end:]
			continue
		}
		// Null
		if strings.HasPrefix(s, "null") {
			b.WriteString(`<span class="tok-null">null</span>`)
			s = s[4:]
			continue
		}
		// Punctuation: { } [ ] : ,
		if strings.ContainsRune("{}[]:,", rune(s[0])) {
			fmt.Fprintf(&b, `<span class="tok-punct">%s</span>`, template.HTMLEscapeString(string(s[0])))
			s = s[1:]
			continue
		}
		// Whitespace and anything else
		b.WriteString(template.HTMLEscapeString(string(s[0])))
		s = s[1:]
	}
	return b.String()
}

func highlightXMLLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]

	var b strings.Builder
	b.WriteString(template.HTMLEscapeString(indent))

	s := trimmed

	// XML comment
	if strings.HasPrefix(s, "<!--") {
		end := strings.Index(s, "-->")
		if end >= 0 {
			fmt.Fprintf(&b, `<span class="tok-cmt">%s</span>`, template.HTMLEscapeString(s[:end+3]))
			s = s[end+3:]
		} else {
			fmt.Fprintf(&b, `<span class="tok-cmt">%s</span>`, template.HTMLEscapeString(s))
			return b.String()
		}
	}

	for len(s) > 0 {
		if s[0] != '<' {
			// Text content between tags
			end := strings.IndexByte(s, '<')
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(s))
				return b.String()
			}
			b.WriteString(template.HTMLEscapeString(s[:end]))
			s = s[end:]
			continue
		}
		// Tag starts
		end := strings.IndexByte(s, '>')
		if end < 0 {
			fmt.Fprintf(&b, `<span class="tok-tag">%s</span>`, template.HTMLEscapeString(s))
			return b.String()
		}
		tag := s[:end+1]
		s = s[end+1:]

		// Highlight tag name and attributes
		inner := tag[1 : len(tag)-1]
		closing := ""
		if strings.HasSuffix(inner, "/") {
			closing = "/"
			inner = inner[:len(inner)-1]
		}
		slash := ""
		if strings.HasPrefix(inner, "/") {
			slash = "/"
			inner = inner[1:]
		}

		parts := strings.SplitN(inner, " ", 2)
		tagName := parts[0]

		attrHTML := ""
		if len(parts) > 1 {
			attrHTML = " " + highlightXMLAttrs(parts[1])
		}

		fmt.Fprintf(&b, `<span class="tok-punct">&lt;</span><span class="tok-tag">%s%s</span>%s<span class="tok-punct">%s&gt;</span>`,
			template.HTMLEscapeString(slash),
			template.HTMLEscapeString(tagName),
			attrHTML,
			template.HTMLEscapeString(closing),
		)
	}
	return b.String()
}

func highlightXMLAttrs(attrs string) string {
	// Simple attr=value tokenizer
	var b strings.Builder
	s := attrs
	for len(s) > 0 {
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			fmt.Fprintf(&b, `<span class="tok-attr">%s</span>`, template.HTMLEscapeString(s))
			return b.String()
		}
		attrName := s[:eqIdx]
		fmt.Fprintf(&b, `<span class="tok-attr">%s</span><span class="tok-punct">=</span>`, template.HTMLEscapeString(attrName))
		s = s[eqIdx+1:]
		if len(s) == 0 {
			break
		}
		// Quoted value
		if s[0] == '"' || s[0] == '\'' {
			q := s[0]
			end := strings.IndexByte(s[1:], q)
			if end < 0 {
				fmt.Fprintf(&b, `<span class="tok-val">%s</span>`, template.HTMLEscapeString(s))
				return b.String()
			}
			fmt.Fprintf(&b, `<span class="tok-val">%s</span>`, template.HTMLEscapeString(s[:end+2]))
			s = s[end+2:]
		}
		// Skip trailing space
		s = strings.TrimLeft(s, " \t")
	}
	return b.String()
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
	case viewerDocumentTypeYAML:
		return "YAML preview"
	case viewerDocumentTypeTOML:
		return "TOML preview"
	case viewerDocumentTypeCSV:
		return "CSV preview"
	case viewerDocumentTypeEnv:
		return "ENV preview"
	case viewerDocumentTypeText:
		return "Text preview"
	default:
		return "Local file preview"
	}
}

func viewerDocumentUsesStructuredTree(doc viewerDocument) bool {
	switch doc.docType {
	case viewerDocumentTypeJSON, viewerDocumentTypeXML:
		return true
	case viewerDocumentTypeYAML, viewerDocumentTypeTOML:
		return doc.structuredContent != ""
	default:
		return false
	}
}

func renderCSVTableHTML(table viewerCSVTable) string {
	var b strings.Builder
	b.WriteString(`<div class="text-frame csv-frame">`)
	if table.Truncated {
		fmt.Fprintf(&b, `<div class="viewer-meta">Showing first %d of %d rows</div>`, len(table.Rows), table.TotalRows)
	} else {
		fmt.Fprintf(&b, `<div class="viewer-meta">%d row`, table.TotalRows)
		if table.TotalRows != 1 {
			b.WriteByte('s')
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<div class="table-wrap"><table><thead><tr>`)
	for _, header := range table.Headers {
		b.WriteString(`<th>`)
		b.WriteString(template.HTMLEscapeString(header))
		b.WriteString(`</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range table.Rows {
		b.WriteString(`<tr>`)
		for _, cell := range row {
			b.WriteString(`<td>`)
			b.WriteString(template.HTMLEscapeString(cell))
			b.WriteString(`</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div></div>`)
	return b.String()
}

func renderEnvHTML(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString(`<div class="code-frame env-frame"><div class="viewer-meta">Values are masked before rendering.</div><table class="line-table">`)
	for i, line := range lines {
		fmt.Fprintf(&b, `<tr><td class="ln">%d</td><td class="lc">%s</td></tr>`, i+1, renderEnvLine(line))
	}
	b.WriteString(`</table></div>`)
	return b.String()
}

func renderEnvLine(line string) string {
	escapedLine := template.HTMLEscapeString(line)
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return escapedLine
	}
	trimmedLeft := strings.TrimLeft(line, " \t")
	indent := template.HTMLEscapeString(line[:len(line)-len(trimmedLeft)])
	if strings.HasPrefix(strings.TrimSpace(trimmedLeft), "#") {
		return indent + `<span class="tok-env-comment">` + template.HTMLEscapeString(trimmedLeft) + `</span>`
	}

	exportPrefix := ""
	rest := trimmedLeft
	if strings.HasPrefix(rest, "export ") {
		exportPrefix = "export "
		rest = strings.TrimLeft(rest[len("export "):], " \t")
	}
	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx < 0 {
		return escapedLine
	}
	key := strings.TrimSpace(rest[:eqIdx])
	value := rest[eqIdx+1:]
	if key == "" {
		return escapedLine
	}

	var b strings.Builder
	b.WriteString(indent)
	if exportPrefix != "" {
		b.WriteString(`<span class="tok-env-export">export</span> `)
	}
	b.WriteString(`<span class="tok-env-key">`)
	b.WriteString(template.HTMLEscapeString(key))
	b.WriteString(`</span><span class="tok-env-sep">=</span><span class="tok-env-val">`)
	b.WriteString(template.HTMLEscapeString(maskEnvValue(value)))
	b.WriteString(`</span>`)
	return b.String()
}

func maskEnvValue(value string) string {
	leading := len(value) - len(strings.TrimLeft(value, " \t"))
	trailing := len(value) - len(strings.TrimRight(value, " \t"))
	core := strings.TrimSpace(value)
	if core == "" {
		return value
	}
	if len(core) >= 2 && ((core[0] == '"' && core[len(core)-1] == '"') || (core[0] == '\'' && core[len(core)-1] == '\'')) {
		inner := core[1 : len(core)-1]
		return value[:leading] + string(core[0]) + maskEnvSecret(inner) + string(core[len(core)-1]) + value[len(value)-trailing:]
	}
	return value[:leading] + maskEnvSecret(core) + value[len(value)-trailing:]
}

func maskEnvSecret(secret string) string {
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	switch {
	case len(runes) <= 4:
		return strings.Repeat("•", len(runes))
	case len(runes) <= 8:
		return string(runes[:1]) + strings.Repeat("•", len(runes)-2) + string(runes[len(runes)-1:])
	default:
		return string(runes[:2]) + strings.Repeat("•", len(runes)-4) + string(runes[len(runes)-2:])
	}
}

func buildCSVTable(content string, maxRows int) (*viewerCSVTable, error) {
	reader := csv.NewReader(strings.NewReader(stripUTF8BOM(content)))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return &viewerCSVTable{Headers: []string{"Column 1"}}, nil
	}

	headerWidth := len(records[0])
	for _, row := range records[1:] {
		if len(row) > headerWidth {
			headerWidth = len(row)
		}
	}
	headers := padCSVRow(records[0], headerWidth)
	for i := range headers {
		if strings.TrimSpace(headers[i]) == "" {
			headers[i] = fmt.Sprintf("Column %d", i+1)
		}
	}

	rows := make([][]string, 0, len(records)-1)
	for i, row := range records[1:] {
		if i >= maxRows {
			return &viewerCSVTable{
				Headers:   headers,
				Rows:      rows,
				TotalRows: len(records) - 1,
				Truncated: true,
			}, nil
		}
		rows = append(rows, padCSVRow(row, headerWidth))
	}
	return &viewerCSVTable{
		Headers:   headers,
		Rows:      rows,
		TotalRows: len(records) - 1,
	}, nil
}

func padCSVRow(row []string, width int) []string {
	if len(row) >= width {
		return append([]string(nil), row...)
	}
	padded := make([]string, width)
	copy(padded, row)
	return padded
}

func yamlToStructuredJSON(content string) (string, error) {
	parser := newYAMLParser(content)
	value, err := parser.parse()
	if err != nil {
		return "", err
	}
	return marshalStructuredJSON(value)
}

type yamlParser struct {
	lines []yamlLine
	i     int
}

type yamlLine struct {
	indent int
	text   string
}

func newYAMLParser(content string) yamlParser {
	rawLines := strings.Split(strings.ReplaceAll(stripUTF8BOM(content), "\r\n", "\n"), "\n")
	lines := make([]yamlLine, 0, len(rawLines))
	for _, raw := range rawLines {
		raw = strings.TrimRight(raw, " \t")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := countLeadingIndent(raw)
		text := stripYAMLComment(strings.TrimLeft(raw, " \t"))
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, yamlLine{indent: indent, text: text})
	}
	return yamlParser{lines: lines}
}

func (p *yamlParser) parse() (any, error) {
	if len(p.lines) == 0 {
		return map[string]any{}, nil
	}
	return p.parseBlock(p.lines[0].indent)
}

func (p *yamlParser) parseBlock(indent int) (any, error) {
	if p.i >= len(p.lines) {
		return map[string]any{}, nil
	}
	if strings.HasPrefix(strings.TrimSpace(p.lines[p.i].text), "- ") {
		return p.parseSequence(indent)
	}
	return p.parseMapping(indent)
}

func (p *yamlParser) parseSequence(indent int) (any, error) {
	items := make([]any, 0)
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if line.indent != indent || !strings.HasPrefix(strings.TrimSpace(line.text), "- ") {
			break
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line.text), "- "))
		p.i++
		item, err := p.parseYAMLSequenceItem(indent, rest)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (p *yamlParser) parseYAMLSequenceItem(indent int, rest string) (any, error) {
	if rest == "" {
		if p.hasIndentedChild(indent) {
			return p.parseBlock(p.lines[p.i].indent)
		}
		return "", nil
	}
	if key, valueText, hasValue, ok := parseYAMLMappingLine(rest); ok {
		item := map[string]any{}
		if hasValue {
			value, err := parseStructuredScalar(valueText)
			if err != nil {
				return nil, err
			}
			item[key] = value
		} else if p.hasIndentedChild(indent) {
			child, err := p.parseBlock(p.lines[p.i].indent)
			if err != nil {
				return nil, err
			}
			item[key] = child
		} else {
			item[key] = map[string]any{}
		}
		if p.hasIndentedChild(indent) {
			child, err := p.parseBlock(p.lines[p.i].indent)
			if err != nil {
				return nil, err
			}
			if childMap, ok := child.(map[string]any); ok {
				mergeStringMaps(item, childMap)
			}
		}
		return item, nil
	}
	value, err := parseStructuredScalar(rest)
	if err != nil {
		return nil, err
	}
	if p.hasIndentedChild(indent) {
		child, err := p.parseBlock(p.lines[p.i].indent)
		if err != nil {
			return nil, err
		}
		return map[string]any{"value": value, "items": child}, nil
	}
	return value, nil
}

func (p *yamlParser) parseMapping(indent int) (any, error) {
	obj := make(map[string]any)
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if line.indent != indent || strings.HasPrefix(strings.TrimSpace(line.text), "- ") {
			break
		}
		key, valueText, hasValue, ok := parseYAMLMappingLine(line.text)
		if !ok {
			value, err := parseStructuredScalar(line.text)
			if err != nil {
				return nil, err
			}
			return value, nil
		}
		p.i++
		if hasValue {
			value, err := parseStructuredScalar(valueText)
			if err != nil {
				return nil, err
			}
			obj[key] = value
			continue
		}
		if p.hasIndentedChild(indent) {
			child, err := p.parseBlock(p.lines[p.i].indent)
			if err != nil {
				return nil, err
			}
			obj[key] = child
		} else {
			obj[key] = map[string]any{}
		}
	}
	return obj, nil
}

func (p *yamlParser) hasIndentedChild(parentIndent int) bool {
	return p.i < len(p.lines) && p.lines[p.i].indent > parentIndent
}

func parseYAMLMappingLine(text string) (string, string, bool, bool) {
	idx := findUnquotedSeparator(text, ':')
	if idx < 0 {
		return "", "", false, false
	}
	key := strings.TrimSpace(text[:idx])
	if key == "" {
		return "", "", false, false
	}
	value := strings.TrimSpace(text[idx+1:])
	return key, value, value != "", true
}

func tomlToStructuredJSON(content string) (string, error) {
	root := make(map[string]any)
	current := root
	lines := strings.Split(strings.ReplaceAll(stripUTF8BOM(content), "\r\n", "\n"), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(stripTOMLComment(strings.TrimRight(raw, " \t")))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			path := splitStructuredPath(strings.TrimSpace(line[2:len(line)-2]), '.')
			table, err := ensureTOMLArrayTable(root, path)
			if err != nil {
				return "", err
			}
			current = table
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			path := splitStructuredPath(strings.TrimSpace(line[1:len(line)-1]), '.')
			current = ensureTOMLTable(root, path)
			continue
		}
		idx := findUnquotedSeparator(line, '=')
		if idx < 0 {
			return "", fmt.Errorf("invalid TOML assignment: %q", line)
		}
		keyPath := splitStructuredPath(strings.TrimSpace(line[:idx]), '.')
		value, err := parseStructuredScalar(strings.TrimSpace(line[idx+1:]))
		if err != nil {
			return "", err
		}
		assignStructuredPath(current, keyPath, value)
	}
	return marshalStructuredJSON(root)
}

func ensureTOMLTable(root map[string]any, path []string) map[string]any {
	current := root
	for _, key := range path {
		next, ok := current[key]
		if !ok {
			child := make(map[string]any)
			current[key] = child
			current = child
			continue
		}
		if child, ok := next.(map[string]any); ok {
			current = child
			continue
		}
		child := make(map[string]any)
		current[key] = child
		current = child
	}
	return current
}

func ensureTOMLArrayTable(root map[string]any, path []string) (map[string]any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty array table path")
	}
	parent := ensureTOMLTable(root, path[:len(path)-1])
	key := path[len(path)-1]
	existing, ok := parent[key]
	if !ok {
		item := make(map[string]any)
		parent[key] = []any{item}
		return item, nil
	}
	arr, ok := existing.([]any)
	if !ok {
		return nil, fmt.Errorf("array table path %q already used as non-array", strings.Join(path, "."))
	}
	item := make(map[string]any)
	parent[key] = append(arr, item)
	return item, nil
}

func assignStructuredPath(root map[string]any, path []string, value any) {
	current := root
	for _, key := range path[:len(path)-1] {
		next := current[key]
		if child, ok := next.(map[string]any); ok {
			current = child
			continue
		}
		child := make(map[string]any)
		current[key] = child
		current = child
	}
	current[path[len(path)-1]] = value
}

func mergeStringMaps(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func marshalStructuredJSON(value any) (string, error) {
	normalized := normalizeStructuredValue(value)
	pretty, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return "", err
	}
	return string(pretty), nil
}

func normalizeStructuredValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized[key] = normalizeStructuredValue(item)
		}
		return normalized
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeStructuredValue(item)
		}
		return out
	default:
		return typed
	}
}

func parseStructuredScalar(text string) (any, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	switch {
	case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
		parts := splitDelimited(trimmed[1:len(trimmed)-1], ',')
		items := make([]any, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			value, err := parseStructuredScalar(part)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, nil
	case strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}"):
		obj := make(map[string]any)
		entries := splitDelimited(trimmed[1:len(trimmed)-1], ',')
		for _, entry := range entries {
			if strings.TrimSpace(entry) == "" {
				continue
			}
			idx := findUnquotedSeparator(entry, ':')
			if idx < 0 {
				idx = findUnquotedSeparator(entry, '=')
			}
			if idx < 0 {
				return nil, fmt.Errorf("invalid inline object entry: %q", entry)
			}
			key := strings.TrimSpace(entry[:idx])
			value, err := parseStructuredScalar(entry[idx+1:])
			if err != nil {
				return nil, err
			}
			obj[trimStructuredQuotes(key)] = value
		}
		return obj, nil
	case (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) || (strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`)):
		return trimStructuredQuotes(trimmed), nil
	case trimmed == "true":
		return true, nil
	case trimmed == "false":
		return false, nil
	case trimmed == "null" || trimmed == "~":
		return nil, nil
	}
	if intValue, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return intValue, nil
	}
	if floatValue, err := strconv.ParseFloat(trimmed, 64); err == nil && strings.ContainsAny(trimmed, ".eE") {
		return floatValue, nil
	}
	return trimmed, nil
}

func trimStructuredQuotes(text string) string {
	if len(text) >= 2 && ((text[0] == '"' && text[len(text)-1] == '"') || (text[0] == '\'' && text[len(text)-1] == '\'')) {
		if text[0] == '"' {
			if unquoted, err := strconv.Unquote(text); err == nil {
				return unquoted
			}
		}
		return strings.ReplaceAll(text[1:len(text)-1], `''`, `'`)
	}
	return text
}

func splitStructuredPath(text string, sep rune) []string {
	parts := splitDelimited(text, sep)
	path := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		path = append(path, trimStructuredQuotes(part))
	}
	return path
}

func splitDelimited(text string, sep rune) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var (
		parts       []string
		current     strings.Builder
		inSingle    bool
		inDouble    bool
		inBacktick  bool
		squareDepth int
		curlyDepth  int
		parenDepth  int
	)
	for i := 0; i < len(text); i++ {
		ch := rune(text[i])
		if ch == '\\' && inDouble && i+1 < len(text) {
			current.WriteByte(text[i])
			i++
			current.WriteByte(text[i])
			continue
		}
		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '[':
			if !inSingle && !inDouble && !inBacktick {
				squareDepth++
			}
		case ']':
			if !inSingle && !inDouble && !inBacktick && squareDepth > 0 {
				squareDepth--
			}
		case '{':
			if !inSingle && !inDouble && !inBacktick {
				curlyDepth++
			}
		case '}':
			if !inSingle && !inDouble && !inBacktick && curlyDepth > 0 {
				curlyDepth--
			}
		case '(':
			if !inSingle && !inDouble && !inBacktick {
				parenDepth++
			}
		case ')':
			if !inSingle && !inDouble && !inBacktick && parenDepth > 0 {
				parenDepth--
			}
		}
		if ch == sep && !inSingle && !inDouble && !inBacktick && squareDepth == 0 && curlyDepth == 0 && parenDepth == 0 {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	parts = append(parts, current.String())
	return parts
}

func findUnquotedSeparator(text string, sep rune) int {
	parts := splitDelimited(text, sep)
	if len(parts) <= 1 {
		return -1
	}
	return len(parts[0])
}

func stripYAMLComment(text string) string {
	return stripStructuredComment(text)
}

func stripTOMLComment(text string) string {
	return stripStructuredComment(text)
}

func stripStructuredComment(text string) string {
	inSingle := false
	inDouble := false
	inBacktick := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '\\' && inDouble && i+1 < len(text) {
			i++
			continue
		}
		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '#':
			if !inSingle && !inDouble && !inBacktick && (i == 0 || unicode.IsSpace(rune(text[i-1]))) {
				return strings.TrimRight(text[:i], " \t")
			}
		}
	}
	return text
}

func countLeadingIndent(text string) int {
	width := 0
	for _, r := range text {
		if r == ' ' {
			width++
			continue
		}
		if r == '\t' {
			width += 2
			continue
		}
		break
	}
	return width
}

func stripUTF8BOM(text string) string {
	return strings.TrimPrefix(text, "\uFEFF")
}

func renderMarkdownHTML(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	return renderMarkdownLines(lines)
}

type markdownParser struct {
	lines []string
	i     int
}

type markdownListInfo struct {
	indent        int
	tag           string
	text          string
	markerWidth   int
	contentIndent int
	task          bool
	taskChecked   bool
}

type markdownTableAlign string

const (
	markdownAlignDefault markdownTableAlign = ""
	markdownAlignLeft    markdownTableAlign = "left"
	markdownAlignCenter  markdownTableAlign = "center"
	markdownAlignRight   markdownTableAlign = "right"
)

func renderMarkdownLines(lines []string) string {
	p := markdownParser{lines: lines}
	return p.renderBlocks()
}

func (p *markdownParser) renderBlocks() string {
	var b strings.Builder
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			p.i++
			continue
		}
		switch {
		case markdownFenceInfo(trimmed) != "":
			b.WriteString(p.renderCodeBlock())
		case isMarkdownTableStart(p.lines, p.i):
			b.WriteString(p.renderTable())
		case isMarkdownBlockquoteLine(line):
			b.WriteString(p.renderBlockquote())
		default:
			if listInfo, ok := parseMarkdownListInfo(line); ok {
				b.WriteString(p.renderList(listInfo.indent))
				continue
			}
			if level := markdownHeadingLevel(trimmed); level > 0 {
				text := strings.TrimSpace(trimmed[level+1:])
				anchor := headingAnchor(text)
				b.WriteString(fmt.Sprintf(`<h%d id="%s">%s</h%d>`, level, anchor, renderMarkdownInline(text), level))
				p.i++
				continue
			}
			if markdownHorizontalRule(trimmed) {
				b.WriteString("<hr>")
				p.i++
				continue
			}
			if isMarkdownRawHTML(trimmed) {
				b.WriteString(trimmed)
				b.WriteByte('\n')
				p.i++
				continue
			}
			b.WriteString(p.renderParagraph())
		}
	}
	return b.String()
}

func (p *markdownParser) renderCodeBlock() string {
	line := strings.TrimSpace(p.lines[p.i])
	fence := markdownFenceInfo(line)
	lang := strings.TrimSpace(strings.TrimPrefix(line, fence))
	p.i++

	var b strings.Builder
	if lang != "" {
		b.WriteString(fmt.Sprintf(`<pre data-lang="%s"><code>`, template.HTMLEscapeString(lang)))
	} else {
		b.WriteString(`<pre><code>`)
	}

	for p.i < len(p.lines) {
		current := p.lines[p.i]
		if strings.TrimSpace(current) == fence {
			p.i++
			break
		}
		b.WriteString(template.HTMLEscapeString(current))
		b.WriteByte('\n')
		p.i++
	}
	b.WriteString(`</code></pre>`)
	return b.String()
}

// hardBreakLine holds a line plus whether it ends with an explicit hard break.
type hardBreakLine struct {
	text      string
	hardBreak bool
}

func (p *markdownParser) renderParagraph() string {
	var lines []hardBreakLine
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if len(lines) > 0 && markdownStartsBlock(p.lines, p.i) {
			break
		}
		lines = append(lines, markdownHardBreakLine(line))
		p.i++
	}
	return `<p>` + renderMarkdownInlineLinesHB(lines) + `</p>`
}

func renderMarkdownInlineLinesHB(lines []hardBreakLine) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, hl := range lines {
		b.WriteString(renderMarkdownInline(hl.text))
		if i < len(lines)-1 {
			if hl.hardBreak {
				b.WriteString("<br>")
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

func (p *markdownParser) renderBlockquote() string {
	var quoted []string
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			quoted = append(quoted, "")
			p.i++
			continue
		}
		if !isMarkdownBlockquoteLine(line) {
			break
		}
		quoted = append(quoted, stripMarkdownBlockquoteMarker(line))
		p.i++
	}
	return `<blockquote>` + renderMarkdownLines(quoted) + `</blockquote>`
}

func (p *markdownParser) renderList(baseIndent int) string {
	var b strings.Builder
	currentTag := ""
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if strings.TrimSpace(line) == "" {
			p.i++
			if next, ok := p.peekNextListItem(); !ok || next.indent < baseIndent {
				break
			}
			continue
		}
		info, ok := parseMarkdownListInfo(line)
		if !ok || info.indent != baseIndent {
			break
		}
		if currentTag != info.tag {
			if currentTag != "" {
				b.WriteString("</")
				b.WriteString(currentTag)
				b.WriteString(">")
			}
			currentTag = info.tag
			b.WriteString("<")
			b.WriteString(currentTag)
			b.WriteString(">")
		}
		p.i++
		b.WriteString(p.renderListItem(info))
	}
	if currentTag != "" {
		b.WriteString("</")
		b.WriteString(currentTag)
		b.WriteString(">")
	}
	return b.String()
}

func (p *markdownParser) renderListItem(info markdownListInfo) string {
	continuation := p.collectListItemContinuation(info)

	var b strings.Builder
	liClass := ""
	if info.task {
		liClass = ` class="task-list-item"`
	}
	b.WriteString("<li")
	b.WriteString(liClass)
	b.WriteString(">")

	paragraphLines, childLines := splitMarkdownListContinuation(info.text, continuation)
	inlineText := renderMarkdownInlineLinesHB(paragraphLines)
	if info.task {
		b.WriteString(`<label class="task-list-label"><input class="task-checkbox" type="checkbox" disabled`)
		if info.taskChecked {
			b.WriteString(` checked`)
		}
		b.WriteString(`><span>`)
		b.WriteString(inlineText)
		b.WriteString(`</span></label>`)
	} else if inlineText != "" {
		b.WriteString(inlineText)
	}
	if len(childLines) > 0 {
		b.WriteString(renderMarkdownLines(childLines))
	}
	b.WriteString("</li>")
	return b.String()
}

func (p *markdownParser) collectListItemContinuation(info markdownListInfo) []string {
	var continuation []string
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if strings.TrimSpace(line) == "" {
			nextIndex, ok := p.nextNonBlankLineIndex(p.i + 1)
			if !ok {
				break
			}
			nextLine := p.lines[nextIndex]
			if nextInfo, ok := parseMarkdownListInfo(nextLine); ok && nextInfo.indent <= info.indent {
				break
			}
			if leadingMarkdownIndentWidth(nextLine) <= info.indent {
				break
			}
			continuation = append(continuation, "")
			p.i++
			continue
		}
		if nextInfo, ok := parseMarkdownListInfo(line); ok && nextInfo.indent <= info.indent {
			break
		}
		if leadingMarkdownIndentWidth(line) <= info.indent {
			break
		}
		continuation = append(continuation, trimMarkdownIndent(line, info.contentIndent))
		p.i++
	}
	return continuation
}

func (p *markdownParser) renderTable() string {
	headerCells := parseMarkdownTableRow(p.lines[p.i])
	alignments := parseMarkdownTableAlignment(p.lines[p.i+1])
	p.i += 2

	var rows [][]string
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if strings.TrimSpace(line) == "" || !looksLikeMarkdownTableRow(line) {
			break
		}
		rows = append(rows, parseMarkdownTableRow(line))
		p.i++
	}

	var b strings.Builder
	b.WriteString(`<div class="table-wrap"><table><thead><tr>`)
	for idx, cell := range headerCells {
		b.WriteString(`<th`)
		if align := markdownTableCellStyle(alignments, idx); align != "" {
			b.WriteString(align)
		}
		b.WriteString(`>`)
		b.WriteString(renderMarkdownInline(strings.TrimSpace(cell)))
		b.WriteString(`</th>`)
	}
	b.WriteString(`</tr></thead>`)
	if len(rows) > 0 {
		b.WriteString(`<tbody>`)
		for _, row := range rows {
			b.WriteString(`<tr>`)
			for idx, cell := range row {
				b.WriteString(`<td`)
				if align := markdownTableCellStyle(alignments, idx); align != "" {
					b.WriteString(align)
				}
				b.WriteString(`>`)
				b.WriteString(renderMarkdownInline(strings.TrimSpace(cell)))
				b.WriteString(`</td>`)
			}
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody>`)
	}
	b.WriteString(`</table></div>`)
	return b.String()
}

func headingAnchor(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

type tocEntry struct {
	Level int
	Text  string
	ID    string
}

func extractTOC(content string) []tocEntry {
	var entries []tocEntry
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		level := markdownHeadingLevel(trimmed)
		if level > 0 && level <= 3 {
			text := strings.TrimSpace(trimmed[level+1:])
			entries = append(entries, tocEntry{Level: level, Text: text, ID: headingAnchor(text)})
		}
	}
	return entries
}

func renderTOC(entries []tocEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`
<div class="toc-trigger" id="tocTrigger"></div>
<div class="toc-panel" id="tocPanel">
  <div class="toc-header">
    <span>On this page</span>
    <button class="toc-close" id="tocClose">&#x2715;</button>
  </div>
  <nav class="toc-list" id="tocList">`)
	for _, e := range entries {
		cls := fmt.Sprintf("h%d", e.Level)
		b.WriteString(fmt.Sprintf(`<a class="toc-item %s" data-target="%s">%s</a>`,
			cls,
			template.HTMLEscapeString(e.ID),
			template.HTMLEscapeString(e.Text),
		))
	}
	b.WriteString(`</nav></div>`)
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

func markdownHorizontalRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	if strings.Trim(line, "-") == "" || strings.Trim(line, "*") == "" || strings.Trim(line, "_") == "" {
		return true
	}
	return false
}

func markdownFenceInfo(line string) string {
	switch {
	case strings.HasPrefix(line, "```"):
		return "```"
	case strings.HasPrefix(line, "~~~"):
		return "~~~"
	default:
		return ""
	}
}

func markdownStartsBlock(lines []string, index int) bool {
	if index < 0 || index >= len(lines) {
		return false
	}
	line := lines[index]
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	if markdownFenceInfo(trimmed) != "" || isMarkdownBlockquoteLine(line) || markdownHeadingLevel(trimmed) > 0 || markdownHorizontalRule(trimmed) || isMarkdownRawHTML(trimmed) {
		return true
	}
	if _, ok := parseMarkdownListInfo(line); ok {
		return true
	}
	return isMarkdownTableStart(lines, index)
}

func isMarkdownRawHTML(line string) bool {
	return strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">")
}

func isMarkdownBlockquoteLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, ">")
}

func stripMarkdownBlockquoteMarker(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, ">") {
		return line
	}
	trimmed = trimmed[1:]
	if strings.HasPrefix(trimmed, " ") {
		trimmed = trimmed[1:]
	}
	return trimmed
}

func leadingMarkdownIndentWidth(line string) int {
	width := 0
	for _, r := range line {
		if r == ' ' {
			width++
			continue
		}
		if r == '\t' {
			width += 4
			continue
		}
		break
	}
	return width
}

func trimMarkdownIndent(line string, width int) string {
	remaining := width
	for len(line) > 0 && remaining > 0 {
		switch line[0] {
		case ' ':
			line = line[1:]
			remaining--
		case '\t':
			line = line[1:]
			remaining -= 4
		default:
			return line
		}
	}
	return line
}

func parseMarkdownListInfo(line string) (markdownListInfo, bool) {
	indent := leadingMarkdownIndentWidth(line)
	trimmed := trimMarkdownIndent(line, indent)
	if len(trimmed) < 2 {
		return markdownListInfo{}, false
	}

	var (
		tag         string
		markerWidth int
		text        string
	)
	switch trimmed[0] {
	case '-', '*', '+':
		if len(trimmed) < 2 || trimmed[1] != ' ' {
			return markdownListInfo{}, false
		}
		tag = "ul"
		markerWidth = 2
		text = strings.TrimSpace(trimmed[2:])
	default:
		pos := 0
		for pos < len(trimmed) && trimmed[pos] >= '0' && trimmed[pos] <= '9' {
			pos++
		}
		if pos == 0 || pos+1 >= len(trimmed) {
			return markdownListInfo{}, false
		}
		if (trimmed[pos] != '.' && trimmed[pos] != ')') || trimmed[pos+1] != ' ' {
			return markdownListInfo{}, false
		}
		tag = "ol"
		markerWidth = pos + 2
		text = strings.TrimSpace(trimmed[pos+2:])
	}

	info := markdownListInfo{
		indent:        indent,
		tag:           tag,
		text:          text,
		markerWidth:   markerWidth,
		contentIndent: indent + markerWidth,
	}
	if checked, ok := parseMarkdownTaskState(text); ok {
		info.task = true
		info.taskChecked = checked
		info.text = strings.TrimSpace(text[3:])
	}
	return info, true
}

func parseMarkdownTaskState(text string) (bool, bool) {
	if len(text) < 4 || text[0] != '[' || text[2] != ']' {
		return false, false
	}
	if text[3] != ' ' {
		return false, false
	}
	switch text[1] {
	case ' ':
		return false, true
	case 'x', 'X':
		return true, true
	default:
		return false, false
	}
}

func splitMarkdownListContinuation(firstText string, lines []string) ([]hardBreakLine, []string) {
	paragraph := []hardBreakLine{{text: firstText}}
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if markdownStartsBlock(lines, i) {
			break
		}
		paragraph = append(paragraph, markdownHardBreakLine(line))
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return paragraph, lines[i:]
}

func (p *markdownParser) peekNextListItem() (markdownListInfo, bool) {
	for idx := p.i; idx < len(p.lines); idx++ {
		if strings.TrimSpace(p.lines[idx]) == "" {
			continue
		}
		return parseMarkdownListInfo(p.lines[idx])
	}
	return markdownListInfo{}, false
}

func (p *markdownParser) nextNonBlankLineIndex(start int) (int, bool) {
	for idx := start; idx < len(p.lines); idx++ {
		if strings.TrimSpace(p.lines[idx]) != "" {
			return idx, true
		}
	}
	return 0, false
}

func isMarkdownTableStart(lines []string, index int) bool {
	if index+1 >= len(lines) {
		return false
	}
	header := strings.TrimSpace(lines[index])
	separator := strings.TrimSpace(lines[index+1])
	if !looksLikeMarkdownTableRow(header) || !looksLikeMarkdownTableDivider(separator) {
		return false
	}
	headerCells := parseMarkdownTableRow(header)
	dividerCells := parseMarkdownTableRow(separator)
	return len(headerCells) > 0 && len(headerCells) == len(dividerCells)
}

func looksLikeMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.Count(trimmed, "|") == 0 {
		return false
	}
	cells := parseMarkdownTableRow(trimmed)
	return len(cells) >= 1
}

func looksLikeMarkdownTableDivider(line string) bool {
	cells := parseMarkdownTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		core := strings.Trim(cell, ":")
		if len(core) < 1 || strings.Trim(core, "-") != "" {
			return false
		}
	}
	return true
}

func parseMarkdownTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	// Strip optional leading and trailing pipes
	if strings.HasPrefix(trimmed, "|") {
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "|") {
		trimmed = trimmed[:len(trimmed)-1]
	}

	// Split on | but skip pipes that are inside backtick spans or preceded by backslash.
	var cells []string
	var cell strings.Builder
	inCode := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch {
		case ch == '`':
			inCode = !inCode
			cell.WriteByte(ch)
		case ch == '\\' && !inCode && i+1 < len(trimmed) && trimmed[i+1] == '|':
			// escaped pipe — include literal pipe in cell, skip backslash
			cell.WriteByte('|')
			i++
		case ch == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(ch)
		}
	}
	// flush last cell (handles rows without trailing pipe)
	if cell.Len() > 0 || len(cells) > 0 {
		cells = append(cells, strings.TrimSpace(cell.String()))
	}
	return cells
}

func parseMarkdownTableAlignment(line string) []markdownTableAlign {
	cells := parseMarkdownTableRow(line)
	alignments := make([]markdownTableAlign, 0, len(cells))
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		switch {
		case strings.HasPrefix(cell, ":") && strings.HasSuffix(cell, ":"):
			alignments = append(alignments, markdownAlignCenter)
		case strings.HasPrefix(cell, ":"):
			alignments = append(alignments, markdownAlignLeft)
		case strings.HasSuffix(cell, ":"):
			alignments = append(alignments, markdownAlignRight)
		default:
			alignments = append(alignments, markdownAlignDefault)
		}
	}
	return alignments
}

func markdownTableCellStyle(alignments []markdownTableAlign, index int) string {
	if index >= len(alignments) {
		return ""
	}
	switch alignments[index] {
	case markdownAlignLeft, markdownAlignCenter, markdownAlignRight:
		return fmt.Sprintf(` style="text-align:%s"`, alignments[index])
	default:
		return ""
	}
}

func renderMarkdownInline(text string) string {
	var b strings.Builder
	for len(text) > 0 {

		switch {
		case strings.HasPrefix(text, `\`) && len(text) > 1 && markdownEscapableChar(text[1]):
			b.WriteString(template.HTMLEscapeString(text[1:2]))
			text = text[2:]
		case strings.HasPrefix(text, "```") || strings.HasPrefix(text, "``") || strings.HasPrefix(text, "`"):
			fenceLen := markdownBacktickRunLength(text)
			fence := strings.Repeat("`", fenceLen)
			end := strings.Index(text[fenceLen:], fence)
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(text[:fenceLen]))
				text = text[fenceLen:]
				continue
			}
			code := text[fenceLen : fenceLen+end]
			b.WriteString("<code>")
			b.WriteString(template.HTMLEscapeString(code))
			b.WriteString("</code>")
			text = text[fenceLen+end+fenceLen:]
		case strings.HasPrefix(text, "***") || strings.HasPrefix(text, "___"):
			token := text[:3]
			end := strings.Index(text[3:], token)
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(text[:3]))
				text = text[3:]
				continue
			}
			b.WriteString("<strong><em>")
			b.WriteString(renderMarkdownInline(text[3 : 3+end]))
			b.WriteString("</em></strong>")
			text = text[3+end+3:]
		case strings.HasPrefix(text, "~~"):
			end := strings.Index(text[2:], "~~")
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(text[:2]))
				text = text[2:]
				continue
			}
			b.WriteString("<del>")
			b.WriteString(renderMarkdownInline(text[2 : 2+end]))
			b.WriteString("</del>")
			text = text[2+end+2:]
		case strings.HasPrefix(text, "**") || strings.HasPrefix(text, "__"):
			token := text[:2]
			end := strings.Index(text[2:], token)
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(text[:2]))
				text = text[2:]
				continue
			}
			b.WriteString("<strong>")
			b.WriteString(renderMarkdownInline(text[2 : 2+end]))
			b.WriteString("</strong>")
			text = text[2+end+2:]
		case strings.HasPrefix(text, "*") || strings.HasPrefix(text, "_"):
			token := text[:1]
			end := strings.Index(text[1:], token)
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(text[:1]))
				text = text[1:]
				continue
			}
			b.WriteString("<em>")
			b.WriteString(renderMarkdownInline(text[1 : 1+end]))
			b.WriteString("</em>")
			text = text[1+end+1:]
		case strings.HasPrefix(text, "["):
			labelEnd := strings.Index(text, "](")
			if labelEnd < 0 {
				b.WriteString(template.HTMLEscapeString(text[:1]))
				text = text[1:]
				continue
			}
			urlEnd := strings.Index(text[labelEnd+2:], ")")
			if urlEnd < 0 {
				b.WriteString(template.HTMLEscapeString(text[:1]))
				text = text[1:]
				continue
			}
			label := text[1:labelEnd]
			urlValue := text[labelEnd+2 : labelEnd+2+urlEnd]
			b.WriteString(`<a href="`)
			b.WriteString(template.HTMLEscapeString(urlValue))
			if strings.HasPrefix(urlValue, "#") {
				b.WriteString(`">`)
			} else {
				b.WriteString(`" target="_blank" rel="noreferrer">`)
			}
			b.WriteString(renderMarkdownInline(label))
			b.WriteString("</a>")
			text = text[labelEnd+2+urlEnd+1:]
		default:
			b.WriteString(template.HTMLEscapeString(text[:1]))
			text = text[1:]
		}
	}
	return b.String()
}

func markdownHardBreakLine(line string) hardBreakLine {
	hardBreak := strings.HasSuffix(line, "  ") || strings.HasSuffix(line, " \\") || strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
	text := strings.TrimSpace(line)
	if strings.HasSuffix(text, "\\") {
		text = strings.TrimRight(text[:len(text)-1], " ")
	}
	return hardBreakLine{text: text, hardBreak: hardBreak}
}

func markdownBacktickRunLength(text string) int {
	length := 0
	for length < len(text) && text[length] == '`' {
		length++
	}
	return length
}

func markdownEscapableChar(ch byte) bool {
	return strings.ContainsRune(`\`+"`*_{}[]()#+-.!|~>", rune(ch))
}

func openURLInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start() // Start not Run — don't block the server goroutine
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
		// GUI pickers — try in order of common availability
		for _, picker := range []struct {
			name string
			args []string
		}{
			{"zenity", []string{"--file-selection", "--title=Open with jot"}},
			{"kdialog", []string{"--getopenfilename", ".", "*"}},
			{"yad", []string{"--file-selection", "--title=Open with jot"}},
			{"qarma", []string{"--file-selection", "--title=Open with jot"}},
		} {
			if _, err := exec.LookPath(picker.name); err == nil {
				return runPickerCommand(picker.name, picker.args...)
			}
		}

		// Terminal fuzzy picker — works without any GUI, ideal for terminal users
		if _, err := exec.LookPath("fzf"); err == nil {
			return pickFileWithFZF()
		}

		return "", errors.New("no file picker available; install zenity, kdialog, yad, or fzf")
	}
}

func pickFileWithFZF() (string, error) {
	// Use find to list files, pipe through fzf for interactive selection.
	// Start from current directory, show relative paths.
	cmd := exec.Command("sh", "-c", `find . -type f | fzf --prompt="jot open > " --height=40% --border`)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		// fzf exits with code 130 when user presses Escape (cancelled)
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130) {
			return "", nil
		}
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", nil
	}
	// Convert relative path to absolute
	return filepath.Abs(path)
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

type convertOptions struct {
	SourcePath   string
	TargetFormat string
	OutputPath   string
	Overwrite    bool
}

type convertResult struct {
	OutputPath string
	Warning    string
}

var convertOutputFormats = []string{"ico", "svg", "png", "jpg", "gif"}

func isSupportedConvertTargetFormat(format string) bool {
	for _, candidate := range convertOutputFormats {
		if format == candidate {
			return true
		}
	}
	return false
}

func canonicalConvertFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "jpeg":
		return "jpg"
	default:
		return format
	}
}

func defaultExtensionForConvertFormat(format string) string {
	return "." + canonicalConvertFormat(format)
}

func jotConvert(w io.Writer, args []string) error {
	options, err := parseConvertArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeHelp(w, "convert")
		}
		return err
	}

	result, err := convertImageFile(options)
	if err != nil {
		return err
	}

	ui := newTermUI(w)
	outName := filepath.Base(result.OutputPath)
	info, statErr := os.Stat(result.OutputPath)
	line := outName
	if statErr == nil {
		line = fmt.Sprintf("%s  %s", outName, ui.tdim(fmt.Sprintf("%.1f KB", float64(info.Size())/1024.0)))
	}
	if _, err := fmt.Fprintln(w, ui.success(line)); err != nil {
		return err
	}
	if result.Warning != "" {
		if _, err := fmt.Fprintln(w, ui.warnLine(result.Warning)); err != nil {
			return err
		}
	}
	return nil
}

func parseConvertArgs(args []string) (convertOptions, error) {
	var options convertOptions
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			return options, flag.ErrHelp
		}
		if strings.HasPrefix(arg, "-") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--out", "-o":
				if hasValue {
					options.OutputPath = value
					continue
				}
				if i+1 >= len(args) {
					return options, fmt.Errorf("missing value for %s", name)
				}
				i++
				options.OutputPath = args[i]
			case "--overwrite":
				options.Overwrite = true
			default:
				return options, fmt.Errorf("unknown flag: %s", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	if len(positional) != 2 {
		return options, fmt.Errorf("usage: jot convert <image-path> <png|jpg|jpeg|gif|ico|svg>")
	}

	options.SourcePath = strings.TrimSpace(positional[0])
	options.TargetFormat = canonicalConvertFormat(positional[1])
	if options.SourcePath == "" {
		return options, errors.New("image path must be provided")
	}
	if !isSupportedConvertTargetFormat(options.TargetFormat) {
		return options, fmt.Errorf("unsupported output format %q; use `png`, `jpg`, `gif`, `ico`, or `svg`", positional[1])
	}
	return options, nil
}

func jotTask(stdin io.Reader, w io.Writer, args []string, getwd func() string) error {
	if len(args) == 1 && isHelpFlag(args[0]) {
		return writeHelp(w, "task")
	}
	if len(args) > 1 {
		return writeHelp(w, "task")
	}

	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "convert":
			return runConvertTask(stdin, w, getwd())
		case "minify":
			return runMinifyTask(stdin, w, getwd())
		case "encode":
			return runEncodeTask(stdin, w, getwd())
		case "hash":
			return runHashTask(stdin, w, getwd())
		case "compress":
			return runCompressTask(stdin, w, getwd())
		case "timestamp":
			return runTimestampTask(stdin, w, getwd(), time.Now)
		case "uuid":
			return runUUIDTask(stdin, w, getwd())
		case "resize":
			return runResizeTask(stdin, w, getwd())
		case "diff":
			return runDiffTask(stdin, w, getwd())
		case "rename":
			return runRenameTask(stdin, w, getwd())
		case "qr":
			return runQRTask(stdin, w, getwd())
		case "strip":
			return runStripTask(stdin, w, getwd())
		case "palette":
			return runPaletteTask(stdin, w, getwd())
		default:
			return fmt.Errorf("unknown task %q", args[0])
		}
	}

	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("jot task")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("tasks")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "convert image", "Turn raster images into png, jpg, gif, ico, or svg", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "minify json", "Minify or pretty-print local JSON from files, text, or stdin", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "encode base64", "Base64 encode or decode local files, text, or stdin", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(4, "hash content", "Compute or verify md5, sha1, sha256, and sha512 digests", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(5, "compress files", "Create zip, tar, or tar.gz archives from local files and folders", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(6, "convert timestamp", "Convert unix timestamps and human-readable dates", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(7, "generate ids", "Generate uuid, nanoid, and random string values", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(8, "resize image", "Resize local images with fit, fill, or stretch modes", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(9, "diff files", "Compare two local text files with a detailed terminal render", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(10, "rename files", "Preview and apply safe local renames", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(11, "generate qr", "Generate PNG, SVG, or ASCII QR codes from local text", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(12, "strip metadata", "Strip metadata from local jpg, png, and gif files", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(13, "extract palette", "Extract hex, swatch, or JSON palettes from images", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select task", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(selection) {
	case "", "1", "convert", "convert image":
		return runConvertTask(reader, w, getwd())
	case "2", "minify", "minify json":
		return runMinifyTask(reader, w, getwd())
	case "3", "encode", "encode base64":
		return runEncodeTask(reader, w, getwd())
	case "4", "hash", "hash content":
		return runHashTask(reader, w, getwd())
	case "5", "compress", "compress files":
		return runCompressTask(reader, w, getwd())
	case "6", "timestamp", "convert timestamp":
		return runTimestampTask(reader, w, getwd(), time.Now)
	case "7", "uuid", "generate ids":
		return runUUIDTask(reader, w, getwd())
	case "8", "resize", "resize image":
		return runResizeTask(reader, w, getwd())
	case "9", "diff", "diff files":
		return runDiffTask(reader, w, getwd())
	case "10", "rename", "rename files":
		return runRenameTask(reader, w, getwd())
	case "11", "qr", "generate qr":
		return runQRTask(reader, w, getwd())
	case "12", "strip", "strip metadata":
		return runStripTask(reader, w, getwd())
	case "13", "palette", "extract palette":
		return runPaletteTask(reader, w, getwd())
	default:
		return fmt.Errorf("unknown task selection %q", selection)
	}
}

func runConvertTask(stdin io.Reader, w io.Writer, dir string) error {
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	images, err := listConvertibleImages(dir)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, ui.header("Convert Image")); err != nil {
		return err
	}

	if len(images) > 0 {
		if _, err := fmt.Fprint(w, ui.sectionLabel("images in this folder")); err != nil {
			return err
		}
		for i, imgPath := range images {
			meta := ""
			if info, statErr := os.Stat(imgPath); statErr == nil {
				kb := float64(info.Size()) / 1024.0
				if kb < 1 {
					meta = fmt.Sprintf("< 1 KB")
				} else {
					meta = fmt.Sprintf("%.0f KB", kb)
				}
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(imgPath), "", meta)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}

	sourcePath, err := promptTaskImagePath(reader, w, ui, dir, images)
	if err != nil {
		return err
	}
	targetFormat, err := promptTaskFormat(reader, w, ui)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	result, err := convertImageFile(convertOptions{
		SourcePath:   sourcePath,
		TargetFormat: targetFormat,
	})
	if err != nil {
		return err
	}

	outName := filepath.Base(result.OutputPath)
	info, statErr := os.Stat(result.OutputPath)
	line := outName
	if statErr == nil {
		line = fmt.Sprintf("%s  %s", outName, ui.tdim(fmt.Sprintf("%.1f KB", float64(info.Size())/1024.0)))
	}
	if _, err := fmt.Fprintln(w, ui.success(line)); err != nil {
		return err
	}
	if result.Warning != "" {
		if _, err := fmt.Fprintln(w, ui.warnLine(result.Warning)); err != nil {
			return err
		}
	}
	tipText := fmt.Sprintf("next time: jot convert %s %s", filepath.Base(sourcePath), targetFormat)
	if _, err := fmt.Fprintln(w, ui.tip(tipText)); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func promptTaskImagePath(reader *bufio.Reader, w io.Writer, ui termUI, dir string, images []string) (string, error) {
	label := "Select image"
	hint := ""
	if len(images) == 0 {
		label = "Image path"
	} else if len(images) == 1 {
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		if len(images) == 1 {
			return images[0], nil
		}
		if len(images) == 0 {
			return "", errors.New("image path must be provided")
		}
		return "", errors.New("select an image by number or enter a path")
	}
	if idx, err := strconv.Atoi(selection); err == nil {
		if idx < 1 || idx > len(images) {
			return "", fmt.Errorf("image selection must be between 1 and %d", len(images))
		}
		return images[idx-1], nil
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return selection, nil
}

func promptTaskFormat(reader *bufio.Reader, w io.Writer, ui termUI) (string, error) {
	if _, err := fmt.Fprint(w, ui.sectionLabel("output format")); err != nil {
		return "", err
	}
	rows := []struct {
		key  string
		name string
		desc string
	}{
		{key: "ico", name: ".ico", desc: "Multi-size favicon (16x16 to 256x256)"},
		{key: "svg", name: ".svg", desc: "Embedded SVG wrapper (scalable container)"},
		{key: "png", name: ".png", desc: "Lossless raster output with alpha support"},
		{key: "jpg", name: ".jpg", desc: "Compressed raster output for photos and screenshots"},
		{key: "gif", name: ".gif", desc: "Palette-based raster output for simple graphics"},
	}
	for i, row := range rows {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return "", err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select format", "ico"))
	if err != nil {
		return "", err
	}
	selection = canonicalConvertFormat(selection)
	switch selection {
	case "", "1", "ico":
		return "ico", nil
	case "2", "svg":
		return "svg", nil
	case "3", "png":
		return "png", nil
	case "4", "jpg":
		return "jpg", nil
	case "5", "gif":
		return "gif", nil
	default:
		return "", fmt.Errorf("unknown format %q", selection)
	}
}

func promptLine(reader *bufio.Reader, w io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(w, prompt); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	return strings.TrimSpace(line), nil
}

func listConvertibleImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var images []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isSupportedRasterPath(entry.Name()) {
			continue
		}
		images = append(images, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(images)
	return images, nil
}

func convertImageFile(options convertOptions) (convertResult, error) {
	sourcePath, err := filepath.Abs(options.SourcePath)
	if err != nil {
		return convertResult{}, err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return convertResult{}, err
	}
	if info.IsDir() {
		return convertResult{}, fmt.Errorf("%s is a directory, expected an image file", sourcePath)
	}

	outputPath, err := resolveConvertOutputPath(sourcePath, options.TargetFormat, options.OutputPath)
	if err != nil {
		return convertResult{}, err
	}
	if err := ensureWritableOutputPath(sourcePath, outputPath, options.Overwrite); err != nil {
		return convertResult{}, err
	}

	var data []byte
	var warning string
	switch options.TargetFormat {
	case "ico":
		data, err = buildICOFile(sourcePath)
	case "svg":
		data, warning, err = buildEmbeddedSVG(sourcePath)
	case "png", "jpg", "gif":
		data, warning, err = buildRasterOutputFile(sourcePath, options.TargetFormat)
	default:
		err = fmt.Errorf("unsupported output format %q", options.TargetFormat)
	}
	if err != nil {
		return convertResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return convertResult{}, err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return convertResult{}, err
	}
	return convertResult{OutputPath: outputPath, Warning: warning}, nil
}

func resolveConvertOutputPath(sourcePath string, targetFormat string, explicit string) (string, error) {
	defaultName := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)) + defaultExtensionForConvertFormat(targetFormat)
	if strings.TrimSpace(explicit) == "" {
		return filepath.Join(filepath.Dir(sourcePath), defaultName), nil
	}

	outputPath := explicit
	if !filepath.IsAbs(outputPath) {
		absPath, err := filepath.Abs(outputPath)
		if err != nil {
			return "", err
		}
		outputPath = absPath
	}
	if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
		return filepath.Join(outputPath, defaultName), nil
	}
	return outputPath, nil
}

func ensureWritableOutputPath(sourcePath string, outputPath string, overwrite bool) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if sourceAbs == outputAbs {
		return errors.New("output path would overwrite the source file; choose a different format or use --out")
	}
	if _, err := os.Stat(outputAbs); err == nil && !overwrite {
		return fmt.Errorf("%s already exists; rerun with --overwrite or choose --out", outputAbs)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func buildICOFile(sourcePath string) ([]byte, error) {
	if !isSupportedRasterPath(sourcePath) {
		return nil, fmt.Errorf("`%s` is not a supported raster source for `.ico`; use `.png`, `.jpg`, `.jpeg`, or `.gif`", sourcePath)
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("could not decode %s as a raster image: %w", sourcePath, err)
	}

	sizes := []int{16, 32, 48, 64, 128, 256}
	type icoFrame struct {
		size int
		data []byte
	}
	frames := make([]icoFrame, 0, len(sizes))
	for _, size := range sizes {
		icon := resizeImageForIcon(src, size)
		var buf bytes.Buffer
		if err := png.Encode(&buf, icon); err != nil {
			return nil, err
		}
		frames = append(frames, icoFrame{size: size, data: buf.Bytes()})
	}

	var out bytes.Buffer
	if err := binary.Write(&out, binary.LittleEndian, uint16(0)); err != nil {
		return nil, err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(1)); err != nil {
		return nil, err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(len(frames))); err != nil {
		return nil, err
	}

	offset := 6 + len(frames)*16
	for _, frame := range frames {
		dimension := byte(frame.size)
		if frame.size >= 256 {
			dimension = 0
		}
		out.WriteByte(dimension)
		out.WriteByte(dimension)
		out.WriteByte(0)
		out.WriteByte(0)
		if err := binary.Write(&out, binary.LittleEndian, uint16(1)); err != nil {
			return nil, err
		}
		if err := binary.Write(&out, binary.LittleEndian, uint16(32)); err != nil {
			return nil, err
		}
		if err := binary.Write(&out, binary.LittleEndian, uint32(len(frame.data))); err != nil {
			return nil, err
		}
		if err := binary.Write(&out, binary.LittleEndian, uint32(offset)); err != nil {
			return nil, err
		}
		offset += len(frame.data)
	}

	for _, frame := range frames {
		if _, err := out.Write(frame.data); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func buildEmbeddedSVG(sourcePath string) ([]byte, string, error) {
	if !isSupportedRasterPath(sourcePath) {
		return nil, "", fmt.Errorf("`%s` is not a supported raster source for `.svg`; use `.png`, `.jpg`, `.jpeg`, or `.gif`", sourcePath)
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("could not inspect %s as a raster image: %w", sourcePath, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", fmt.Errorf("image %s reports zero dimensions", sourcePath)
	}

	mimeType := rasterMIMEType(sourcePath)
	encoded := base64.StdEncoding.EncodeToString(data)

	// Produce a well-formed SVG 1.1 document. The raster is embedded as a
	// base64 data URI so the file is self-contained and works in any browser
	// or SVG viewer without a companion image file.
	//
	// preserveAspectRatio="xMidYMid meet" ensures the image scales uniformly
	// when the SVG container is resized, which is the behaviour designers
	// expect from an icon or logo SVG.
	svg := fmt.Sprintf(
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
			"<svg xmlns=\"http://www.w3.org/2000/svg\"\n"+
			"     xmlns:xlink=\"http://www.w3.org/1999/xlink\"\n"+
			"     width=\"%d\" height=\"%d\"\n"+
			"     viewBox=\"0 0 %d %d\"\n"+
			"     preserveAspectRatio=\"xMidYMid meet\"\n"+
			"     role=\"img\">\n"+
			"  <title>%s</title>\n"+
			"  <image\n"+
			"    x=\"0\" y=\"0\"\n"+
			"    width=\"%d\" height=\"%d\"\n"+
			"    preserveAspectRatio=\"xMidYMid meet\"\n"+
			"    href=\"data:%s;base64,%s\"\n"+
			"  />\n"+
			"</svg>\n",
		cfg.Width, cfg.Height,
		cfg.Width, cfg.Height,
		svgEscapeTitle(filepath.Base(sourcePath)),
		cfg.Width, cfg.Height,
		mimeType, encoded,
	)

	warning := "the SVG wraps the original raster pixels; it will not scale to crisp vectors. " +
		"For a true vector, open the SVG in Inkscape and use Path > Trace Bitmap."
	return []byte(svg), warning, nil
}

func buildRasterOutputFile(sourcePath string, targetFormat string) ([]byte, string, error) {
	if !isSupportedRasterPath(sourcePath) {
		return nil, "", fmt.Errorf("`%s` is not a supported raster source; use `.png`, `.jpg`, `.jpeg`, or `.gif`", sourcePath)
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil, "", fmt.Errorf("could not decode %s as a raster image: %w", sourcePath, err)
	}

	var buf bytes.Buffer
	var warning string
	switch targetFormat {
	case "png":
		err = png.Encode(&buf, src)
	case "jpg":
		if hasAlpha(src) {
			src = flattenImageOnBackground(src, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			warning = "transparent pixels were flattened onto a white background for JPG output."
		}
		err = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 92})
	case "gif":
		err = gif.Encode(&buf, src, &gif.Options{NumColors: 256})
		warning = "GIF output is single-frame and palette-limited; gradients and photos may lose detail."
	default:
		return nil, "", fmt.Errorf("unsupported raster output format %q", targetFormat)
	}
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), warning, nil
}

// svgEscapeTitle escapes characters that are not valid in an SVG <title> text node.
func svgEscapeTitle(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func isSupportedRasterPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	default:
		return false
	}
}

func rasterMIMEType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

func hasAlpha(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

func flattenImageOnBackground(src image.Image, bg color.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sr, sg, sb, sa := src.At(x, y).RGBA()
			alpha := float64(sa) / 65535.0
			red := uint8(alpha*float64(sr/257) + (1-alpha)*float64(bg.R))
			green := uint8(alpha*float64(sg/257) + (1-alpha)*float64(bg.G))
			blue := uint8(alpha*float64(sb/257) + (1-alpha)*float64(bg.B))
			dst.SetRGBA(x, y, color.RGBA{R: red, G: green, B: blue, A: 255})
		}
	}
	return dst
}

func resizeImageForIcon(src image.Image, size int) *image.RGBA {
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 {
		return image.NewRGBA(image.Rect(0, 0, size, size))
	}

	// Preserve aspect ratio, fit within size×size, centre on transparent canvas.
	targetWidth := size
	targetHeight := size
	if srcWidth >= srcHeight {
		targetHeight = srcHeight * size / srcWidth
		if targetHeight < 1 {
			targetHeight = 1
		}
	} else {
		targetWidth = srcWidth * size / srcHeight
		if targetWidth < 1 {
			targetWidth = 1
		}
	}

	scaled := resizeImageBilinear(src, targetWidth, targetHeight)
	icon := image.NewRGBA(image.Rect(0, 0, size, size))
	offsetX := (size - targetWidth) / 2
	offsetY := (size - targetHeight) / 2
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			icon.Set(offsetX+x, offsetY+y, scaled.At(x, y))
		}
	}
	return icon
}

// resizeImageBilinear scales src to (width × height) using bilinear interpolation.
// This produces smooth edges at small icon sizes (16×16, 32×32) where nearest-
// neighbor sampling would create obvious aliasing artefacts.
func resizeImageBilinear(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	srcW := float64(bounds.Dx())
	srcH := float64(bounds.Dy())
	minX := bounds.Min.X
	minY := bounds.Min.Y
	maxX := bounds.Max.X - 1
	maxY := bounds.Max.Y - 1

	clampX := func(x int) int {
		if x < minX {
			return minX
		}
		if x > maxX {
			return maxX
		}
		return x
	}
	clampY := func(y int) int {
		if y < minY {
			return minY
		}
		if y > maxY {
			return maxY
		}
		return y
	}

	for dy := 0; dy < height; dy++ {
		// Map destination pixel centre back to source space.
		sy := (float64(dy)+0.5)*srcH/float64(height) - 0.5 + float64(minY)
		y0 := clampY(int(math.Floor(sy)))
		y1 := clampY(y0 + 1)
		ty := sy - math.Floor(sy)

		for dx := 0; dx < width; dx++ {
			sx := (float64(dx)+0.5)*srcW/float64(width) - 0.5 + float64(minX)
			x0 := clampX(int(math.Floor(sx)))
			x1 := clampX(x0 + 1)
			tx := sx - math.Floor(sx)

			// Sample the four surrounding pixels.
			c00r, c00g, c00b, c00a := src.At(x0, y0).RGBA()
			c10r, c10g, c10b, c10a := src.At(x1, y0).RGBA()
			c01r, c01g, c01b, c01a := src.At(x0, y1).RGBA()
			c11r, c11g, c11b, c11a := src.At(x1, y1).RGBA()

			// Bilinear blend — values are in 16-bit (0–65535); scale back to 8-bit.
			bilerp := func(tl, tr, bl, br uint32) uint8 {
				top := float64(tl)*(1-tx) + float64(tr)*tx
				bot := float64(bl)*(1-tx) + float64(br)*tx
				return uint8((top*(1-ty) + bot*ty) / 257.0)
			}
			dst.SetRGBA(dx, dy, color.RGBA{
				R: bilerp(c00r, c10r, c01r, c11r),
				G: bilerp(c00g, c10g, c01g, c11g),
				B: bilerp(c00b, c10b, c01b, c11b),
				A: bilerp(c00a, c10a, c01a, c11a),
			})
		}
	}
	return dst
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
