package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type renameStrategy string

const (
	renameStrategyReplace  renameStrategy = "replace"
	renameStrategyPrefix   renameStrategy = "prefix"
	renameStrategySuffix   renameStrategy = "suffix"
	renameStrategyExt      renameStrategy = "ext"
	renameStrategyCase     renameStrategy = "case"
	renameStrategyTemplate renameStrategy = "template"
)

type renameConflictMode string

const (
	renameConflictAbort  renameConflictMode = "abort"
	renameConflictSkip   renameConflictMode = "skip"
	renameConflictSuffix renameConflictMode = "suffix"
)

type renameOptions struct {
	Selector string
	Strategy renameStrategy

	ReplaceFrom string
	ReplaceTo   string
	Prefix      string
	Suffix      string
	Ext         string
	CaseMode    string
	Template    string

	Recursive   bool
	Apply       bool
	DryRun      bool
	Quiet       bool
	OnConflict  renameConflictMode
	help       bool
}

type renameCandidate struct {
	Path string
	Info fs.FileInfo
}

type renamePlanEntry struct {
	SourcePath string
	TargetPath string
	Status     string
	Reason     string
	TempPath   string
}

type renamePlan struct {
	Entries []renamePlanEntry
}

func jotRename(w io.Writer, args []string) error {
	return jotRenameWithInput(os.Stdin, w, args, os.Getwd)
}

func jotRenameWithInput(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	opts, err := parseRenameArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, writeErr := io.WriteString(w, renderRenameHelp(isTTY(w)))
			return writeErr
		}
		return err
	}
	cwd, err := getwd()
	if err != nil {
		return err
	}
	plan, err := buildRenamePlan(cwd, opts)
	if err != nil {
		return err
	}
	if !opts.Apply {
		return renderRenamePlan(w, plan, opts, false)
	}
	return applyRenamePlan(w, plan, opts)
}

func renderRenameHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot rename", "Preview and apply safe local renames without leaving the terminal.")
	writeUsageSection(&b, style, []string{
		"jot rename <selector> --prefix <text>",
		"jot rename <selector> --replace <from> <to> --apply",
		"jot rename <selector> --template <pattern> --dry-run",
	}, []string{
		"Preview is the default. Add `--apply` to execute the plan.",
		"Selectors can be a single file, a folder, or a glob.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--replace FROM TO", description: "Replace text in the filename stem."},
		{name: "--prefix TEXT", description: "Add text before the filename stem."},
		{name: "--suffix TEXT", description: "Add text after the filename stem."},
		{name: "--ext EXT", description: "Change the filename extension."},
		{name: "--case lower|upper|title|kebab|snake", description: "Apply a case transform to the filename stem."},
		{name: "--template PATTERN", description: "Build the new name from tokens such as `{stem}`, `{ext}`, `{n}`, and `{parent}`."},
		{name: "--recursive", description: "Walk subdirectories when the selector is a folder."},
		{name: "--apply", description: "Execute the rename plan instead of only previewing it."},
		{name: "--dry-run", description: "Preview the plan explicitly."},
		{name: "--on-conflict abort|skip|suffix", description: "Choose how to handle destination collisions."},
		{name: "--quiet", description: "Suppress the success summary after apply."},
	})
	writeExamplesSection(&b, style, []string{
		"jot rename logo.jpeg --ext .jpg --apply",
		"jot rename ./photos --prefix icon- --recursive --apply",
		"jot rename \"*.md\" --template \"{n:03}-{stem}{ext}\" --dry-run",
		"jot task rename",
	})
	return b.String()
}

func runRenameTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	candidates, err := listRenameCandidates(dir, false)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, ui.header("Rename")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("source")); err != nil {
		return err
	}
	if len(candidates) == 0 {
		if _, err := fmt.Fprintln(w, ui.listItem(1, "path", "Enter a file, folder, or glob", "")); err != nil {
			return err
		}
	} else {
		for i, cand := range candidates {
			meta := ""
			if cand.Info != nil {
				meta = formatSizeLabel(cand.Info.Size())
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(cand.Path), "", meta)); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	sourceSel, err := promptLine(reader, w, ui.styledPrompt("Select source", "1"))
	if err != nil {
		return err
	}
	sourcePath, err := resolveRenameSource(dir, sourceSel, candidates)
	if err != nil {
		return err
	}

	opts := renameOptions{Selector: sourcePath, OnConflict: renameConflictAbort}
	if isDirectoryPath(sourcePath) {
		if _, err := fmt.Fprint(w, ui.sectionLabel("scope")); err != nil {
			return err
		}
		scopeSel, err := promptLine(reader, w, ui.styledPrompt("Recursive", "n"))
		if err != nil {
			return err
		}
		opts.Recursive = isYes(scopeSel)
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("strategy")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "replace", "Replace one substring in the filename stem", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "prefix", "Add text before the stem", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "suffix", "Add text after the stem", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(4, "ext", "Change the extension only", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(5, "case", "Transform the stem casing", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(6, "template", "Render the new name from tokens", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	strategySel, err := promptLine(reader, w, ui.styledPrompt("Select strategy", "2"))
	if err != nil {
		return err
	}
	if err := applyRenameStrategySelection(&opts, strategySel); err != nil {
		return err
	}

	if err := promptRenameStrategyValues(reader, w, ui, &opts); err != nil {
		return err
	}

	plan, err := buildRenamePlan(dir, opts)
	if err != nil {
		return err
	}
	if err := renderRenamePlan(w, plan, opts, false); err != nil {
		return err
	}
	applySel, err := promptLine(reader, w, ui.styledPrompt("Apply these renames", "y/N"))
	if err != nil {
		return err
	}
	if !isYes(applySel) {
		if _, err := fmt.Fprintln(w, ui.tip(renameTip(opts))); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, "")
		return err
	}
	if err := applyRenamePlan(w, plan, opts); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(renameTip(opts))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func parseRenameArgs(args []string) (renameOptions, error) {
	opts := renameOptions{OnConflict: renameConflictAbort}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			opts.help = true
			return opts, flag.ErrHelp
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--replace":
				if hasValue {
					parts := strings.SplitN(value, ",", 2)
					if len(parts) != 2 {
						return opts, fmt.Errorf("--replace expects FROM and TO")
					}
					opts.Strategy = renameStrategyReplace
					opts.ReplaceFrom = parts[0]
					opts.ReplaceTo = parts[1]
					continue
				}
				if i+2 >= len(args) {
					return opts, fmt.Errorf("--replace expects FROM and TO")
				}
				opts.Strategy = renameStrategyReplace
				opts.ReplaceFrom = args[i+1]
				opts.ReplaceTo = args[i+2]
				i += 2
			case "--prefix":
				opts.Strategy = renameStrategyPrefix
				if hasValue {
					opts.Prefix = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Prefix = args[i]
			case "--suffix":
				opts.Strategy = renameStrategySuffix
				if hasValue {
					opts.Suffix = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Suffix = args[i]
			case "--ext":
				opts.Strategy = renameStrategyExt
				if hasValue {
					opts.Ext = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Ext = args[i]
			case "--case":
				opts.Strategy = renameStrategyCase
				if hasValue {
					opts.CaseMode = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.CaseMode = args[i]
			case "--template":
				opts.Strategy = renameStrategyTemplate
				if hasValue {
					opts.Template = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Template = args[i]
			case "--recursive":
				opts.Recursive = true
			case "--apply":
				opts.Apply = true
			case "--dry-run":
				opts.DryRun = true
			case "--quiet":
				opts.Quiet = true
			case "--on-conflict":
				if hasValue {
					opts.OnConflict = renameConflictMode(strings.ToLower(strings.TrimSpace(value)))
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.OnConflict = renameConflictMode(strings.ToLower(strings.TrimSpace(args[i])))
			default:
				return opts, fmt.Errorf("unsupported flag %q", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	if len(positional) != 1 {
		return opts, errors.New("usage: jot rename <selector> [strategy flags]")
	}
	opts.Selector = positional[0]
	if opts.Strategy == "" {
		return opts, errors.New("choose one rename strategy: --replace, --prefix, --suffix, --ext, --case, or --template")
	}
	if opts.Apply && opts.DryRun {
		return opts, errors.New("--apply and --dry-run cannot be combined")
	}
	if opts.OnConflict == "" {
		opts.OnConflict = renameConflictAbort
	}
	switch opts.OnConflict {
	case renameConflictAbort, renameConflictSkip, renameConflictSuffix:
	default:
		return opts, fmt.Errorf("unsupported conflict mode %q", opts.OnConflict)
	}
	return opts, nil
}

func applyRenameStrategySelection(opts *renameOptions, selection string) error {
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "", "1", "replace":
		opts.Strategy = renameStrategyReplace
	case "2", "prefix":
		opts.Strategy = renameStrategyPrefix
	case "3", "suffix":
		opts.Strategy = renameStrategySuffix
	case "4", "ext":
		opts.Strategy = renameStrategyExt
	case "5", "case":
		opts.Strategy = renameStrategyCase
	case "6", "template":
		opts.Strategy = renameStrategyTemplate
	default:
		return fmt.Errorf("unknown strategy %q", selection)
	}
	return nil
}

func promptRenameStrategyValues(reader *bufio.Reader, w io.Writer, ui termUI, opts *renameOptions) error {
	switch opts.Strategy {
	case renameStrategyReplace:
		from, err := promptLine(reader, w, ui.styledPrompt("Replace from", "text"))
		if err != nil {
			return err
		}
		to, err := promptLine(reader, w, ui.styledPrompt("Replace to", "text"))
		if err != nil {
			return err
		}
		opts.ReplaceFrom = from
		opts.ReplaceTo = to
	case renameStrategyPrefix:
		value, err := promptLine(reader, w, ui.styledPrompt("Prefix", "text"))
		if err != nil {
			return err
		}
		opts.Prefix = value
	case renameStrategySuffix:
		value, err := promptLine(reader, w, ui.styledPrompt("Suffix", "text"))
		if err != nil {
			return err
		}
		opts.Suffix = value
	case renameStrategyExt:
		value, err := promptLine(reader, w, ui.styledPrompt("Extension", ".ext"))
		if err != nil {
			return err
		}
		opts.Ext = value
	case renameStrategyCase:
		if _, err := fmt.Fprint(w, ui.sectionLabel("case")); err != nil {
			return err
		}
		for i, item := range []string{"lower", "upper", "title", "kebab", "snake"} {
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, item, "", "")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
		value, err := promptLine(reader, w, ui.styledPrompt("Select case", "1"))
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "1", "lower", "2":
			opts.CaseMode = "lower"
		case "upper", "3":
			opts.CaseMode = "upper"
		case "title", "4":
			opts.CaseMode = "title"
		case "kebab", "5":
			opts.CaseMode = "kebab"
		case "snake", "6":
			opts.CaseMode = "snake"
		default:
			return fmt.Errorf("unknown case mode %q", value)
		}
	case renameStrategyTemplate:
		value, err := promptLine(reader, w, ui.styledPrompt("Template", "{n:03}-{stem}{ext}"))
		if err != nil {
			return err
		}
		opts.Template = value
	default:
		return fmt.Errorf("unsupported rename strategy %q", opts.Strategy)
	}
	return nil
}

func buildRenamePlan(cwd string, opts renameOptions) (renamePlan, error) {
	candidates, err := collectRenameCandidates(cwd, opts.Selector, opts.Recursive)
	if err != nil {
		return renamePlan{}, err
	}
	if len(candidates) == 0 {
		return renamePlan{}, errors.New("no files matched the selector")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })

	plan := renamePlan{Entries: make([]renamePlanEntry, 0, len(candidates))}
	sourceSet := make(map[string]struct{}, len(candidates))
	for _, cand := range candidates {
		sourceSet[cand.Path] = struct{}{}
	}
	usedDest := make(map[string]struct{}, len(candidates))
	for idx, cand := range candidates {
		nextName, err := renameNextName(cand, opts, idx+1)
		if err != nil {
			return renamePlan{}, err
		}
		destPath := filepath.Join(filepath.Dir(cand.Path), nextName)
		if destPath == cand.Path {
			plan.Entries = append(plan.Entries, renamePlanEntry{SourcePath: cand.Path, TargetPath: destPath, Status: "skip", Reason: "already named"})
			continue
		}
		if conflictPathExists(destPath, sourceSet) || pathUsed(usedDest, destPath) {
			switch opts.OnConflict {
			case renameConflictAbort:
				return renamePlan{}, fmt.Errorf("destination already exists: %s", destPath)
			case renameConflictSkip:
				plan.Entries = append(plan.Entries, renamePlanEntry{SourcePath: cand.Path, TargetPath: destPath, Status: "skip", Reason: "conflict"})
				continue
			case renameConflictSuffix:
				destPath = uniqueRenameDestination(filepath.Dir(cand.Path), nextName, sourceSet, usedDest)
			}
		}
		usedDest[destPath] = struct{}{}
		plan.Entries = append(plan.Entries, renamePlanEntry{SourcePath: cand.Path, TargetPath: destPath, Status: "rename"})
	}
	return plan, nil
}

func uniqueRenameDestination(dir, baseName string, sourceSet, usedDest map[string]struct{}) string {
	stem, ext := splitRenameName(baseName)
	for n := 2; n < 1000; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, n, ext))
		if conflictPathExists(candidate, sourceSet) || pathUsed(usedDest, candidate) {
			continue
		}
		return candidate
	}
	return filepath.Join(dir, baseName)
}

func renderRenamePlan(w io.Writer, plan renamePlan, opts renameOptions, applied bool) error {
	ui := newTermUI(w)
	title := "Rename Preview"
	if applied {
		title = "Rename"
	}
	if _, err := fmt.Fprint(w, ui.header(title)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("plan")); err != nil {
		return err
	}
	for i, entry := range plan.Entries {
		status := entry.Status
		if entry.Reason != "" {
			status += " " + entry.Reason
		}
		line := fmt.Sprintf("%s -> %s", filepath.Base(entry.SourcePath), filepath.Base(entry.TargetPath))
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, line, "", status)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if !opts.Quiet {
		summary := summarizeRenamePlan(plan)
		if _, err := fmt.Fprintln(w, ui.success(summary)); err != nil {
			return err
		}
	}
	return nil
}

func applyRenamePlan(w io.Writer, plan renamePlan, opts renameOptions) error {
	if len(plan.Entries) == 0 {
		return nil
	}
	ui := newTermUI(w)
	stage := make([]renamePlanEntry, 0, len(plan.Entries))
	for i, entry := range plan.Entries {
		if entry.Status != "rename" {
			continue
		}
		tempPath := uniqueRenameTempPath(filepath.Dir(entry.SourcePath), i)
		if err := os.Rename(entry.SourcePath, tempPath); err != nil {
			return err
		}
		plan.Entries[i].TempPath = tempPath
		stage = append(stage, plan.Entries[i])
	}
	for i := range plan.Entries {
		entry := plan.Entries[i]
		if entry.Status != "rename" {
			continue
		}
		if err := os.Rename(entry.TempPath, entry.TargetPath); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ui.success(summarizeRenamePlan(plan))); err != nil {
		return err
	}
	return nil
}

func summarizeRenamePlan(plan renamePlan) string {
	renamed := 0
	skipped := 0
	for _, entry := range plan.Entries {
		switch entry.Status {
		case "rename":
			renamed++
		case "skip":
			skipped++
		}
	}
	if skipped == 0 {
		return fmt.Sprintf("renamed %d file(s)", renamed)
	}
	return fmt.Sprintf("renamed %d file(s), skipped %d", renamed, skipped)
}

func renameTip(opts renameOptions) string {
	switch opts.Strategy {
	case renameStrategyReplace:
		return "next time: jot rename <selector> --replace FROM TO --apply"
	case renameStrategyPrefix:
		return "next time: jot rename <selector> --prefix TEXT --apply"
	case renameStrategySuffix:
		return "next time: jot rename <selector> --suffix TEXT --apply"
	case renameStrategyExt:
		return "next time: jot rename <selector> --ext .new --apply"
	case renameStrategyCase:
		return "next time: jot rename <selector> --case kebab --apply"
	case renameStrategyTemplate:
		return "next time: jot rename <selector> --template \"{n:03}-{stem}{ext}\" --apply"
	default:
		return "next time: jot rename <selector> --apply"
	}
}

func collectRenameCandidates(cwd, selector string, recursive bool) ([]renameCandidate, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, errors.New("selector must be provided")
	}
	absSelector := selector
	if !filepath.IsAbs(absSelector) {
		absSelector = filepath.Join(cwd, absSelector)
	}
	if hasRenameGlob(absSelector) {
		matches, err := filepath.Glob(absSelector)
		if err != nil {
			return nil, err
		}
		var out []renameCandidate
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			out = append(out, renameCandidate{Path: match, Info: info})
		}
		return out, nil
	}
	info, err := os.Stat(absSelector)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		if recursive {
			var out []renameCandidate
			err := filepath.WalkDir(absSelector, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				out = append(out, renameCandidate{Path: path, Info: info})
				return nil
			})
			return out, err
		}
		entries, err := os.ReadDir(absSelector)
		if err != nil {
			return nil, err
		}
		var out []renameCandidate
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			out = append(out, renameCandidate{Path: filepath.Join(absSelector, entry.Name()), Info: info})
		}
		return out, nil
	}
	return []renameCandidate{{Path: absSelector, Info: info}}, nil
}

func listRenameCandidates(dir string, recursive bool) ([]renameCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []renameCandidate
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if !recursive {
				continue
			}
			if err := filepath.WalkDir(path, func(child string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				out = append(out, renameCandidate{Path: child, Info: info})
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, renameCandidate{Path: path, Info: info})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func resolveRenameSource(cwd, selection string, candidates []renameCandidate) (string, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		if len(candidates) == 1 {
			return candidates[0].Path, nil
		}
		return "", errors.New("selector must be provided")
	}
	if idx, err := strconv.Atoi(selection); err == nil && idx >= 1 && idx <= len(candidates) {
		return candidates[idx-1].Path, nil
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(cwd, selection)
	}
	return selection, nil
}

func renameNextName(cand renameCandidate, opts renameOptions, index int) (string, error) {
	name := filepath.Base(cand.Path)
	stem, ext := splitRenameName(name)
	switch opts.Strategy {
	case renameStrategyReplace:
		nameStem := strings.ReplaceAll(stem, opts.ReplaceFrom, opts.ReplaceTo)
		return nameStem + ext, nil
	case renameStrategyPrefix:
		return opts.Prefix + stem + ext, nil
	case renameStrategySuffix:
		return stem + opts.Suffix + ext, nil
	case renameStrategyExt:
		return stem + normalizeRenameExt(opts.Ext), nil
	case renameStrategyCase:
		transformed, err := applyRenameCase(stem, opts.CaseMode)
		if err != nil {
			return "", err
		}
		return transformed + ext, nil
	case renameStrategyTemplate:
		parent := filepath.Base(filepath.Dir(cand.Path))
		return renderRenameTemplate(opts.Template, stem, ext, parent, index)
	default:
		return "", fmt.Errorf("unsupported rename strategy %q", opts.Strategy)
	}
}

func applyRenameCase(stem, mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "lower":
		return strings.ToLower(stem), nil
	case "upper":
		return strings.ToUpper(stem), nil
	case "title":
		return titleCaseWords(stem), nil
	case "kebab":
		return joinRenameWords(stem, "-"), nil
	case "snake":
		return joinRenameWords(stem, "_"), nil
	default:
		return "", fmt.Errorf("unknown case mode %q", mode)
	}
}

func renderRenameTemplate(pattern, stem, ext, parent string, index int) (string, error) {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		if pattern[i] != '{' {
			b.WriteByte(pattern[i])
			i++
			continue
		}
		end := strings.IndexByte(pattern[i+1:], '}')
		if end < 0 {
			return "", errors.New("template token is missing a closing brace")
		}
		token := pattern[i+1 : i+1+end]
		rendered, err := renderRenameToken(token, stem, ext, parent, index)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		i += end + 2
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", errors.New("template produced an empty filename")
	}
	return out, nil
}

func renderRenameToken(token, stem, ext, parent string, index int) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("template token cannot be empty")
	}
	switch {
	case token == "stem":
		return stem, nil
	case strings.HasPrefix(token, "stem|"):
		value, err := applyRenameFilter(stem, strings.TrimSpace(strings.TrimPrefix(token, "stem|")))
		if err != nil {
			return "", err
		}
		return value, nil
	case token == "ext":
		return ext, nil
	case token == "parent":
		return parent, nil
	case strings.HasPrefix(token, "parent|"):
		value, err := applyRenameFilter(parent, strings.TrimSpace(strings.TrimPrefix(token, "parent|")))
		if err != nil {
			return "", err
		}
		return value, nil
	case token == "n":
		return strconv.Itoa(index), nil
	case strings.HasPrefix(token, "n:"):
		widthText := strings.TrimPrefix(token, "n:")
		width, err := strconv.Atoi(widthText)
		if err != nil || width <= 0 {
			return "", fmt.Errorf("invalid sequence width %q", widthText)
		}
		return fmt.Sprintf("%0*d", width, index), nil
	default:
		return "", fmt.Errorf("unknown template token %q", token)
	}
}

func applyRenameFilter(value, filter string) (string, error) {
	switch filter {
	case "lower":
		return strings.ToLower(value), nil
	case "upper":
		return strings.ToUpper(value), nil
	case "title":
		return titleCaseWords(value), nil
	case "kebab":
		return joinRenameWords(value, "-"), nil
	case "snake":
		return joinRenameWords(value, "_"), nil
	default:
		return "", fmt.Errorf("unknown template filter %q", filter)
	}
}

func splitRenameName(name string) (string, string) {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext), ext
}

func normalizeRenameExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func hasRenameGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func conflictPathExists(path string, sourceSet map[string]struct{}) bool {
	if _, ok := sourceSet[path]; ok {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func pathUsed(set map[string]struct{}, path string) bool {
	_, ok := set[path]
	return ok
}

func uniqueRenameTempPath(dir string, idx int) string {
	base := fmt.Sprintf(".jot-rename-%d-%d", os.Getpid(), idx)
	return filepath.Join(dir, base)
}

func formatSizeLabel(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(size)/1024.0)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024.0*1024.0))
}

func isDirectoryPath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "true", "1":
		return true
	default:
		return false
	}
}

func titleCaseWords(text string) string {
	parts := splitRenameWords(text)
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func joinRenameWords(text, sep string) string {
	parts := splitRenameWords(text)
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, sep)
}

func splitRenameWords(text string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			current.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	if len(out) == 0 {
		return []string{strings.TrimSpace(text)}
	}
	return out
}
