package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type diffOptions struct {
	leftPath         string
	rightPath        string
	viewer           bool
	summaryOnly      bool
	context          int
	ignoreWhitespace bool
	ignoreEOL        bool
	wordDiff         bool
	noColor          bool
}

type diffOpKind int

const (
	diffEqual diffOpKind = iota
	diffDelete
	diffAdd
)

type diffOp struct {
	kind    diffOpKind
	leftNo  int
	rightNo int
	text    string
}

type diffBlock struct {
	start int
	end   int
}

// jotDiff is the direct command entry point for local text diffs.
func jotDiff(w io.Writer, args []string) error {
	return jotDiffWithInput(os.Stdin, w, args, os.Getwd)
}

func jotDiffWithInput(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	opts, helpRequested, err := parseDiffArgs(args)
	if err != nil {
		return err
	}
	if helpRequested {
		_, writeErr := io.WriteString(w, renderDiffHelp(isTTY(w)))
		return writeErr
	}
	cwd, err := getwd()
	if err != nil {
		return err
	}
	return executeDiff(stdin, w, cwd, opts)
}

func renderDiffHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot diff", "Compare two local text files from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot diff before.txt after.txt",
		"jot diff before.txt after.txt --viewer",
		"jot diff before.txt after.txt --summary-only",
	}, []string{
		"`jot diff` rejects binary files and stays local.",
		"`--viewer` renders a detailed terminal diff instead of opening a browser.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--viewer", description: "Show the detailed local diff render after the summary."},
		{name: "--summary-only", description: "Skip the detailed render and print only the terminal summary."},
		{name: "--context N", description: "Show N context lines around each changed block."},
		{name: "--ignore-whitespace", description: "Treat whitespace-only changes as unchanged."},
		{name: "--ignore-eol", description: "Treat line-ending differences as unchanged."},
		{name: "--word-diff", description: "Show inline word changes inside modified lines."},
		{name: "--no-color", description: "Force plain text output."},
	})
	writeExamplesSection(&b, style, []string{
		"jot diff README.md README.new.md",
		"jot diff before.txt after.txt --viewer",
		"jot task diff",
	})
	return b.String()
}

func renderTaskDiffHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot task", "Discover and run terminal-first tasks without leaving the current folder.")
	writeUsageSection(&b, style, []string{
		"jot task",
		"jot task diff",
	}, []string{
		"`jot task` is the guided front door for jot's task layer.",
		"`jot task diff` keeps the current terminal UI and teaches the direct command after the first run.",
	})
	writeExamplesSection(&b, style, []string{
		"jot task",
		"jot task diff",
		"jot diff before.txt after.txt --viewer",
	})
	return b.String()
}

func parseDiffArgs(args []string) (diffOptions, bool, error) {
	opts := diffOptions{context: 3, ignoreEOL: true}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			return opts, true, nil
		}
		if strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "-") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--viewer":
				opts.viewer = true
			case "--summary-only":
				opts.summaryOnly = true
			case "--ignore-whitespace":
				opts.ignoreWhitespace = true
			case "--ignore-eol":
				opts.ignoreEOL = true
			case "--word-diff":
				opts.wordDiff = true
			case "--no-color":
				opts.noColor = true
			case "--context":
				if !hasValue {
					if i+1 >= len(args) {
						return opts, false, fmt.Errorf("missing value for %s", name)
					}
					i++
					value = args[i]
				}
				context, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil || context < 0 {
					return opts, false, errors.New("--context must be a non-negative integer")
				}
				opts.context = context
			default:
				return opts, false, fmt.Errorf("unknown flag %q", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	if opts.viewer && opts.summaryOnly {
		return opts, false, errors.New("--viewer cannot be combined with --summary-only")
	}
	if len(positional) != 2 {
		return opts, false, errors.New("usage: jot diff <left-path> <right-path>")
	}
	opts.leftPath = positional[0]
	opts.rightPath = positional[1]
	return opts, false, nil
}

func executeDiff(stdin io.Reader, w io.Writer, cwd string, opts diffOptions) error {
	_ = stdin

	leftPath, err := resolveDiffPath(cwd, opts.leftPath)
	if err != nil {
		return err
	}
	rightPath, err := resolveDiffPath(cwd, opts.rightPath)
	if err != nil {
		return err
	}
	if samePath(leftPath, rightPath) {
		return errors.New("cannot diff the same file against itself")
	}

	leftDoc, err := loadDiffDocument(leftPath, opts.ignoreEOL)
	if err != nil {
		return err
	}
	rightDoc, err := loadDiffDocument(rightPath, opts.ignoreEOL)
	if err != nil {
		return err
	}

	ui := newTermUI(w)
	if opts.noColor {
		ui.color = false
	}

	result := buildDiffResult(leftDoc, rightDoc, opts)
	renderDiffSummary(w, ui, result)
	if opts.viewer && !opts.summaryOnly {
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
		return renderDiffViewer(w, ui, result, opts)
	}
	return nil
}

func runDiffTask(stdin io.Reader, w io.Writer, dir string) error {
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprint(w, ui.header("Diff")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("files")); err != nil {
		return err
	}
	files, err := listDiffFiles(dir)
	if err != nil {
		return err
	}
	for i, item := range files {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(item), "", "")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	leftPath, err := promptDiffPath(reader, w, ui, dir, "Left file", files)
	if err != nil {
		return err
	}
	rightPath, err := promptDiffPath(reader, w, ui, dir, "Right file", files)
	if err != nil {
		return err
	}

	viewer := false
	wordDiff := false
	context := 3
	if _, err := fmt.Fprint(w, ui.sectionLabel("options")); err != nil {
		return err
	}
	advanced, err := promptLine(reader, w, ui.styledPrompt("Advanced settings", "y/N"))
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(advanced), "y") || strings.EqualFold(strings.TrimSpace(advanced), "yes") {
		viewerSel, err := promptLine(reader, w, ui.styledPrompt("Show detailed render", "y/N"))
		if err != nil {
			return err
		}
		viewer = strings.EqualFold(strings.TrimSpace(viewerSel), "y") || strings.EqualFold(strings.TrimSpace(viewerSel), "yes")
		wordSel, err := promptLine(reader, w, ui.styledPrompt("Enable word diff", "y/N"))
		if err != nil {
			return err
		}
		wordDiff = strings.EqualFold(strings.TrimSpace(wordSel), "y") || strings.EqualFold(strings.TrimSpace(wordSel), "yes")
		contextSel, err := promptLine(reader, w, ui.styledPrompt("Context lines", "3"))
		if err != nil {
			return err
		}
		if strings.TrimSpace(contextSel) != "" {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(contextSel))
			if parseErr != nil || parsed < 0 {
				return errors.New("context lines must be a non-negative integer")
			}
			context = parsed
		}
	}

	opts := diffOptions{
		leftPath:    leftPath,
		rightPath:   rightPath,
		viewer:      viewer,
		context:     context,
		wordDiff:    wordDiff,
		noColor:     false,
		ignoreEOL:   true,
		summaryOnly: false,
	}
	if err := executeDiff(nil, w, dir, opts); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(fmt.Sprintf("next time: jot diff %s %s%s", filepath.Base(leftPath), filepath.Base(rightPath), diffTipSuffix(opts)))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func diffTipSuffix(opts diffOptions) string {
	var parts []string
	if opts.viewer {
		parts = append(parts, "--viewer")
	}
	if opts.wordDiff {
		parts = append(parts, "--word-diff")
	}
	if opts.context != 3 {
		parts = append(parts, fmt.Sprintf("--context %d", opts.context))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

type diffDocument struct {
	path     string
	name     string
	display  []string
	compare  []string
	original string
	isText   bool
}

func loadDiffDocument(path string, ignoreEOL bool) (diffDocument, error) {
	info, err := os.Stat(path)
	if err != nil {
		return diffDocument{}, err
	}
	if info.IsDir() {
		return diffDocument{}, fmt.Errorf("%s is a directory, expected a text file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return diffDocument{}, err
	}
	if !isDiffTextBytes(data) {
		return diffDocument{}, fmt.Errorf("%s is binary and outside the text diff scope", path)
	}
	text := normalizeDiffContent(string(data), ignoreEOL)
	display := splitDiffLines(text)
	compare := make([]string, len(display))
	for i, line := range display {
		compare[i] = normalizeDiffCompareLine(line, false)
	}
	return diffDocument{
		path:     path,
		name:     filepath.Base(path),
		display:  display,
		compare:  compare,
		original: text,
		isText:   true,
	}, nil
}

func isDiffTextBytes(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

func normalizeDiffContent(text string, ignoreEOL bool) string {
	text = stripUTF8BOM(text)
	if ignoreEOL {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	return text
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	if strings.HasSuffix(text, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func normalizeDiffCompareLine(line string, ignoreWhitespace bool) string {
	if !ignoreWhitespace {
		return line
	}
	fields := strings.Fields(line)
	return strings.Join(fields, " ")
}

type diffResult struct {
	left      diffDocument
	right     diffDocument
	ops       []diffOp
	additions int
	deletions int
	hunks     int
}

func buildDiffResult(left, right diffDocument, opts diffOptions) diffResult {
	leftKeys := make([]string, len(left.display))
	for i, line := range left.display {
		leftKeys[i] = normalizeDiffCompareLine(line, opts.ignoreWhitespace)
	}
	rightKeys := make([]string, len(right.display))
	for i, line := range right.display {
		rightKeys[i] = normalizeDiffCompareLine(line, opts.ignoreWhitespace)
	}
	ops := buildDiffOps(leftKeys, rightKeys, left.display, right.display)
	additions := 0
	deletions := 0
	hunks := 0
	inHunk := false
	for _, op := range ops {
		switch op.kind {
		case diffAdd:
			additions++
			if !inHunk {
				hunks++
				inHunk = true
			}
		case diffDelete:
			deletions++
			if !inHunk {
				hunks++
				inHunk = true
			}
		default:
			inHunk = false
		}
	}
	return diffResult{left: left, right: right, ops: ops, additions: additions, deletions: deletions, hunks: hunks}
}

func buildDiffOps(leftKeys, rightKeys, leftLines, rightLines []string) []diffOp {
	n := len(leftKeys)
	m := len(rightKeys)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if leftKeys[i] == rightKeys[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && leftKeys[i] == rightKeys[j]:
			ops = append(ops, diffOp{
				kind:    diffEqual,
				leftNo:  i + 1,
				rightNo: j + 1,
				text:    leftLines[i],
			})
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			ops = append(ops, diffOp{
				kind:    diffAdd,
				leftNo:  0,
				rightNo: j + 1,
				text:    rightLines[j],
			})
			j++
		default:
			ops = append(ops, diffOp{
				kind:    diffDelete,
				leftNo:  i + 1,
				rightNo: 0,
				text:    leftLines[i],
			})
			i++
		}
	}
	return ops
}

func renderDiffSummary(w io.Writer, ui termUI, result diffResult) {
	if result.additions == 0 && result.deletions == 0 {
		_, _ = fmt.Fprintln(w, ui.success(fmt.Sprintf("%s and %s are identical", result.left.name, result.right.name)))
		return
	}
	_, _ = fmt.Fprintln(w, ui.listItem(1, "files", fmt.Sprintf("%s -> %s", result.left.name, result.right.name), ""))
	_, _ = fmt.Fprintln(w, ui.listItem(2, "summary", fmt.Sprintf("+%d  -%d  %d hunk(s)", result.additions, result.deletions, result.hunks), ""))
}

func renderDiffViewer(w io.Writer, ui termUI, result diffResult, opts diffOptions) error {
	if _, err := fmt.Fprint(w, ui.header("Diff Viewer")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("summary")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %s -> %s\n", result.left.name, result.right.name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  +%d  -%d  %d hunk(s)\n", result.additions, result.deletions, result.hunks); err != nil {
		return err
	}
	if result.additions == 0 && result.deletions == 0 {
		return nil
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("diff")); err != nil {
		return err
	}
	for _, block := range diffBlocks(result.ops, opts.context) {
		if _, err := fmt.Fprintf(w, "%s\n", ui.tdim(fmt.Sprintf("@@ block %d-%d @@", block.start+1, block.end))); err != nil {
			return err
		}
		for i := block.start; i < block.end; {
			op := result.ops[i]
			if opts.wordDiff && i+1 < block.end {
				next := result.ops[i+1]
				switch {
				case op.kind == diffDelete && next.kind == diffAdd:
					oldMarked, newMarked := diffWordMarkup(op.text, next.text)
					if _, err := fmt.Fprintln(w, renderDiffLine(ui, diffDelete, op.leftNo, 0, oldMarked)); err != nil {
						return err
					}
					if _, err := fmt.Fprintln(w, renderDiffLine(ui, diffAdd, 0, next.rightNo, newMarked)); err != nil {
						return err
					}
					i += 2
					continue
				case op.kind == diffAdd && next.kind == diffDelete:
					oldMarked, newMarked := diffWordMarkup(next.text, op.text)
					if _, err := fmt.Fprintln(w, renderDiffLine(ui, diffDelete, next.leftNo, 0, oldMarked)); err != nil {
						return err
					}
					if _, err := fmt.Fprintln(w, renderDiffLine(ui, diffAdd, 0, op.rightNo, newMarked)); err != nil {
						return err
					}
					i += 2
					continue
				}
			}
			if _, err := fmt.Fprintln(w, renderDiffLine(ui, op.kind, op.leftNo, op.rightNo, op.text)); err != nil {
				return err
			}
			i++
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}
	return nil
}

func diffBlocks(ops []diffOp, context int) []diffBlock {
	if len(ops) == 0 {
		return nil
	}
	var raw []diffBlock
	for i := 0; i < len(ops); {
		if ops[i].kind == diffEqual {
			i++
			continue
		}
		start := i
		for start > 0 && ops[start-1].kind == diffEqual && i-start < context {
			start--
		}
		end := i
		for end < len(ops) && ops[end].kind != diffEqual {
			end++
		}
		after := end
		for after < len(ops) && ops[after].kind == diffEqual && after-end < context {
			after++
		}
		raw = append(raw, diffBlock{start: start, end: after})
		i = after
	}
	if len(raw) == 0 {
		return nil
	}
	merged := []diffBlock{raw[0]}
	for _, block := range raw[1:] {
		last := &merged[len(merged)-1]
		if block.start <= last.end {
			if block.end > last.end {
				last.end = block.end
			}
			continue
		}
		merged = append(merged, block)
	}
	return merged
}

func renderDiffLine(ui termUI, kind diffOpKind, leftNo, rightNo int, text string) string {
	left := diffNumber(leftNo)
	right := diffNumber(rightNo)
	prefix := " "
	switch kind {
	case diffDelete:
		prefix = "-"
		if ui.color {
			prefix = ui.tmagenta("-")
		}
	case diffAdd:
		prefix = "+"
		if ui.color {
			prefix = ui.tgreen("+")
		}
	default:
		if ui.color {
			prefix = ui.tdim(" ")
		}
	}
	return fmt.Sprintf("  %s  %s  %s  %s", prefix, left, right, text)
}

func diffNumber(n int) string {
	if n <= 0 {
		return "   "
	}
	return fmt.Sprintf("%3d", n)
}

func diffWordMarkup(oldLine, newLine string) (string, string) {
	oldTokens := strings.Fields(oldLine)
	newTokens := strings.Fields(newLine)
	type tokenOp int
	const (
		tokenEqual tokenOp = iota
		tokenDelete
		tokenAdd
	)
	type tokenStep struct {
		op   tokenOp
		text string
	}

	n := len(oldTokens)
	m := len(newTokens)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldTokens[i] == newTokens[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var oldParts []string
	var newParts []string
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && oldTokens[i] == newTokens[j]:
			oldParts = append(oldParts, oldTokens[i])
			newParts = append(newParts, newTokens[j])
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			newParts = append(newParts, "{+"+newTokens[j]+"+}")
			j++
		default:
			oldParts = append(oldParts, "[-"+oldTokens[i]+"-]")
			i++
		}
	}
	return strings.Join(oldParts, " "), strings.Join(newParts, " ")
}

func resolveDiffPath(cwd, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("path must be provided")
	}
	if !filepath.IsAbs(input) {
		input = filepath.Join(cwd, input)
	}
	return filepath.Abs(input)
}

func promptDiffPath(reader *bufio.Reader, w io.Writer, ui termUI, dir, label string, files []string) (string, error) {
	hint := ""
	if len(files) > 0 {
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		if len(files) == 0 {
			return "", errors.New("file path must be provided")
		}
		return "", fmt.Errorf("%s must be provided", strings.ToLower(label))
	}
	if idx, parseErr := strconv.Atoi(selection); parseErr == nil {
		if idx < 1 || idx > len(files) {
			return "", fmt.Errorf("file selection must be between 1 and %d", len(files))
		}
		return files[idx-1], nil
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return selection, nil
}

func listDiffFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isDiffTextCandidate(entry.Name()) {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func isDiffTextCandidate(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".log", ".json", ".jsonl", ".xml", ".yaml", ".yml", ".toml", ".csv", ".env":
		return true
	default:
		return false
	}
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	if errA != nil {
		aa = filepath.Clean(a)
	}
	bb, errB := filepath.Abs(b)
	if errB != nil {
		bb = filepath.Clean(b)
	}
	return strings.EqualFold(aa, bb)
}
