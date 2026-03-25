package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type compressOptions struct {
	Inputs        []string
	Format        string
	OutputPath    string
	Name          string
	Force         bool
	DryRun        bool
	Quiet         bool
	IncludeHidden bool
	Excludes      []string
}

type compressInput struct {
	Path        string
	Info        os.FileInfo
	ArchiveRoot string
	IsDir       bool
}

type compressEntry struct {
	SourcePath  string
	ArchivePath string
	Info        os.FileInfo
	IsDir       bool
}

type compressPlan struct {
	Format     string
	OutputPath string
	Entries    []compressEntry
}

func jotCompress(w io.Writer, args []string) error {
	opts, err := parseCompressArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return executeCompress(w, opts, cwd)
}

func parseCompressArgs(args []string) (compressOptions, error) {
	return resolveCompressArgs(args)
}

func renderCompressHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot compress", "Create local zip, tar, or tar.gz archives without leaving the terminal.")
	writeUsageSection(&b, style, []string{
		"jot compress <path...> [zip|tar|tar.gz]",
		"jot compress <path...> --format <zip|tar|tar.gz>",
		"jot compress <path...> --out <archive-path>",
	}, []string{
		"Folder inputs keep their own top-level folder name in the archive.",
		"Globs expand locally before archiving.",
		"Default format is `zip` when none is specified.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--format FORMAT", description: "Choose `zip`, `tar`, or `tar.gz` explicitly."},
		{name: "--out PATH", description: "Write the archive to an exact path instead of using the default sibling name."},
		{name: "--name NAME", description: "Set the archive base name while keeping the chosen format suffix."},
		{name: "--force", description: "Replace an existing archive file if one is already present."},
		{name: "--dry-run", description: "Show the planned archive without writing any file."},
		{name: "--quiet", description: "Suppress the success summary."},
		{name: "--include-hidden", description: "Include files and folders whose names start with a dot."},
		{name: "--exclude PATTERN", description: "Skip entries matching a glob pattern. Repeat the flag to add more."},
	})
	writeExamplesSection(&b, style, []string{
		"jot compress ./project zip",
		"jot compress ./project --format tar.gz",
		"jot compress ./assets/*.png --name release-assets --dry-run",
		"jot compress ./project --exclude node_modules/* --exclude .git/*",
	})
	return b.String()
}

func runCompressTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	candidates, err := listCompressCandidates(dir, false)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, ui.header("Compress")); err != nil {
		return err
	}
	if len(candidates) > 0 {
		if _, err := fmt.Fprint(w, ui.sectionLabel("items in this folder")); err != nil {
			return err
		}
		for i, candidate := range candidates {
			meta := "file"
			if candidate.IsDir {
				meta = "dir"
			} else if candidate.SizeLabel != "" {
				meta = candidate.SizeLabel
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, candidate.DisplayName, "", meta)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}

	sourcePath, sourceLabel, err := promptCompressSource(reader, w, ui, dir, candidates)
	if err != nil {
		return err
	}
	format, err := promptCompressFormat(reader, w, ui)
	if err != nil {
		return err
	}
	customOutput, err := promptCompressOutput(reader, w, ui)
	if err != nil {
		return err
	}
	includeHidden, excludes, err := promptCompressAdvanced(reader, w, ui)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	opts := compressOptions{
		Inputs:        []string{sourcePath},
		Format:        format,
		IncludeHidden: includeHidden,
		Excludes:      excludes,
	}
	if customOutput.kind == compressOutputName {
		opts.Name = customOutput.value
	}
	if customOutput.kind == compressOutputPath {
		opts.OutputPath = customOutput.value
	}

	if err := executeCompress(w, opts, dir); err != nil {
		return err
	}

	if !opts.Quiet {
		tip := "next time: " + buildCompressCommandTip(sourceLabel, opts, customOutput)
		ui2 := newTermUI(w)
		if _, err := fmt.Fprintln(w, ui2.tip(tip)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}
	return nil
}

type compressOutputKind string

const (
	compressOutputNone compressOutputKind = ""
	compressOutputName compressOutputKind = "name"
	compressOutputPath compressOutputKind = "path"
)

type compressOutputChoice struct {
	kind  compressOutputKind
	value string
}

type compressCandidate struct {
	Path        string
	DisplayName string
	SizeLabel   string
	IsDir       bool
}

func promptCompressSource(reader *bufio.Reader, w io.Writer, ui termUI, dir string, candidates []compressCandidate) (string, string, error) {
	label := "Select source"
	hint := ""
	if len(candidates) == 1 {
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", "", err
	}
	if selection == "" {
		if len(candidates) == 1 {
			return candidates[0].Path, candidates[0].DisplayName, nil
		}
		return "", "", errors.New("source path must be provided")
	}
	if idx, err := strconv.Atoi(selection); err == nil {
		if idx < 1 || idx > len(candidates) {
			return "", "", fmt.Errorf("source selection must be between 1 and %d", len(candidates))
		}
		return candidates[idx-1].Path, candidates[idx-1].DisplayName, nil
	}
	resolved, err := resolveCompressPath(dir, selection)
	if err != nil {
		return "", "", err
	}
	return resolved, compressCommandPathForDisplay(dir, selection), nil
}

func promptCompressFormat(reader *bufio.Reader, w io.Writer, ui termUI) (string, error) {
	if _, err := fmt.Fprint(w, ui.sectionLabel("archive format")); err != nil {
		return "", err
	}
	rows := []struct {
		key  string
		name string
		desc string
	}{
		{key: "1", name: ".zip", desc: "Zip archive"},
		{key: "2", name: ".tar", desc: "Tar archive"},
		{key: "3", name: ".tar.gz", desc: "Tar archive compressed with gzip"},
	}
	for i, row := range rows {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return "", err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select format", "zip"))
	if err != nil {
		return "", err
	}
	if selection == "" {
		return "zip", nil
	}
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "1", "zip":
		return "zip", nil
	case "2", "tar":
		return "tar", nil
	case "3", "tar.gz", "tgz":
		return "tar.gz", nil
	default:
		return "", fmt.Errorf("unknown format %q", selection)
	}
}

func promptCompressOutput(reader *bufio.Reader, w io.Writer, ui termUI) (compressOutputChoice, error) {
	if _, err := fmt.Fprint(w, ui.sectionLabel("output")); err != nil {
		return compressOutputChoice{}, err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Output name or path", "auto"))
	if err != nil {
		return compressOutputChoice{}, err
	}
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return compressOutputChoice{}, nil
	}
	if looksLikeArchivePath(selection) || strings.ContainsAny(selection, `\/`) {
		return compressOutputChoice{kind: compressOutputPath, value: selection}, nil
	}
	return compressOutputChoice{kind: compressOutputName, value: selection}, nil
}

func promptCompressAdvanced(reader *bufio.Reader, w io.Writer, ui termUI) (bool, []string, error) {
	if _, err := fmt.Fprint(w, ui.sectionLabel("advanced")); err != nil {
		return false, nil, err
	}
	advanced, err := promptLine(reader, w, ui.styledPrompt("Advanced options", "n"))
	if err != nil {
		return false, nil, err
	}
	if !isYesAnswer(advanced) {
		return false, nil, nil
	}
	includeHiddenAnswer, err := promptLine(reader, w, ui.styledPrompt("Include hidden files", "n"))
	if err != nil {
		return false, nil, err
	}
	includeHidden := isYesAnswer(includeHiddenAnswer)
	var excludes []string
	for {
		pattern, err := promptLine(reader, w, ui.styledPrompt("Exclude pattern", "blank to finish"))
		if err != nil {
			return false, nil, err
		}
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			break
		}
		excludes = append(excludes, pattern)
	}
	return includeHidden, excludes, nil
}

func executeCompress(w io.Writer, opts compressOptions, cwd string) error {
	if len(opts.Inputs) == 0 {
		return errors.New("at least one input path must be provided")
	}
	opts.Format = canonicalCompressFormat(opts.Format)
	if !isSupportedCompressFormat(opts.Format) {
		return fmt.Errorf("unsupported archive format %q; use `zip`, `tar`, or `tar.gz`", opts.Format)
	}
	if strings.TrimSpace(opts.OutputPath) != "" && strings.TrimSpace(opts.Name) != "" {
		return errors.New("--out and --name cannot be used together")
	}
	if strings.TrimSpace(opts.Name) != "" && strings.ContainsAny(opts.Name, `\/`) {
		return fmt.Errorf("archive name must not contain path separators: %q", opts.Name)
	}

	roots, err := resolveCompressInputs(opts.Inputs, opts.IncludeHidden)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return errors.New("no archive entries matched the provided inputs")
	}

	entries, err := collectCompressEntries(roots, opts.IncludeHidden, opts.Excludes)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("no archive entries matched the provided filters")
	}

	outputPath, err := resolveCompressOutputPath(opts, roots, cwd)
	if err != nil {
		return err
	}
	if err := ensureCompressOutputPath(outputPath, roots, opts.Force); err != nil {
		return err
	}

	plan := compressPlan{
		Format:     opts.Format,
		OutputPath: outputPath,
		Entries:    entries,
	}
	if opts.DryRun {
		if opts.Quiet {
			return nil
		}
		ui := newTermUI(w)
		line := fmt.Sprintf("dry run: would create %s with %d entries", filepath.Base(plan.OutputPath), len(plan.Entries))
		if _, err := fmt.Fprintln(w, ui.tip(line)); err != nil {
			return err
		}
		return nil
	}

	if err := writeCompressArchive(plan, opts.Force); err != nil {
		return err
	}
	if opts.Quiet {
		return nil
	}

	info, statErr := os.Stat(plan.OutputPath)
	ui := newTermUI(w)
	line := filepath.Base(plan.OutputPath)
	if statErr == nil {
		line = fmt.Sprintf("%s  %s", line, ui.tdim(fmt.Sprintf("%.1f KB", float64(info.Size())/1024.0)))
	}
	line = fmt.Sprintf("%s  %d entries", line, len(plan.Entries))
	if _, err := fmt.Fprintln(w, ui.success(line)); err != nil {
		return err
	}
	return nil
}

func resolveCompressArgs(args []string) (compressOptions, error) {
	var opts compressOptions
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			return opts, flag.ErrHelp
		}
		if strings.HasPrefix(arg, "-") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--format":
				if hasValue {
					opts.Format = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Format = args[i]
			case "--out":
				if hasValue {
					opts.OutputPath = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.OutputPath = args[i]
			case "--name":
				if hasValue {
					opts.Name = value
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				opts.Name = args[i]
			case "--exclude":
				if hasValue {
					if strings.TrimSpace(value) == "" {
						return opts, errors.New("exclude pattern must not be empty")
					}
					opts.Excludes = append(opts.Excludes, value)
					continue
				}
				if i+1 >= len(args) {
					return opts, fmt.Errorf("missing value for %s", name)
				}
				i++
				if strings.TrimSpace(args[i]) == "" {
					return opts, errors.New("exclude pattern must not be empty")
				}
				opts.Excludes = append(opts.Excludes, args[i])
			case "--force":
				opts.Force = true
			case "--dry-run":
				opts.DryRun = true
			case "--quiet":
				opts.Quiet = true
			case "--include-hidden":
				opts.IncludeHidden = true
			default:
				return opts, fmt.Errorf("unknown flag: %s", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	if len(positional) >= 2 && opts.Format == "" {
		candidate := canonicalCompressFormat(positional[len(positional)-1])
		if isSupportedCompressFormat(candidate) {
			opts.Format = candidate
			positional = positional[:len(positional)-1]
		}
	}
	if opts.Format == "" {
		opts.Format = "zip"
	}
	opts.Format = canonicalCompressFormat(opts.Format)
	if !isSupportedCompressFormat(opts.Format) {
		return opts, fmt.Errorf("unsupported archive format %q; use `zip`, `tar`, or `tar.gz`", opts.Format)
	}
	if len(positional) == 0 {
		return opts, flag.ErrHelp
	}
	opts.Inputs = positional
	return opts, nil
}

func canonicalCompressFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case ".zip", "zip":
		return "zip"
	case ".tar", "tar":
		return "tar"
	case ".tar.gz", "tar.gz", "tgz":
		return "tar.gz"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

func isSupportedCompressFormat(format string) bool {
	switch canonicalCompressFormat(format) {
	case "zip", "tar", "tar.gz":
		return true
	default:
		return false
	}
}

func defaultExtensionForCompressFormat(format string) string {
	switch canonicalCompressFormat(format) {
	case "zip":
		return ".zip"
	case "tar":
		return ".tar"
	case "tar.gz":
		return ".tar.gz"
	default:
		return ""
	}
}

func resolveCompressInputs(inputs []string, includeHidden bool) ([]compressInput, error) {
	seen := make(map[string]struct{})
	var roots []compressInput

	for _, raw := range inputs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		matches, err := expandCompressInput(raw)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			abs, err := filepath.Abs(match)
			if err != nil {
				return nil, err
			}
			abs = filepath.Clean(abs)
			if _, ok := seen[abs]; ok {
				return nil, fmt.Errorf("duplicate input path: %s", match)
			}
			info, err := os.Lstat(match)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("symlinks are not supported: %s", match)
			}
			if !includeHidden && isHiddenName(filepath.Base(match)) {
				continue
			}
			root := compressInput{
				Path:        match,
				Info:        info,
				ArchiveRoot: compressArchiveRootForInput(match, info),
				IsDir:       info.IsDir(),
			}
			roots = append(roots, root)
			seen[abs] = struct{}{}
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Path < roots[j].Path
	})
	return roots, nil
}

func expandCompressInput(input string) ([]string, error) {
	if compressLooksLikeGlob(input) {
		matches, err := filepath.Glob(input)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no matches for glob pattern %q", input)
		}
		sort.Strings(matches)
		return matches, nil
	}
	if _, err := os.Stat(input); err != nil {
		return nil, err
	}
	return []string{input}, nil
}

func compressLooksLikeGlob(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func collectCompressEntries(inputs []compressInput, includeHidden bool, excludes []string) ([]compressEntry, error) {
	var entries []compressEntry
	seen := make(map[string]struct{})

	for _, input := range inputs {
		if input.IsDir {
			if !includeHidden && isHiddenName(filepath.Base(input.Path)) {
				continue
			}
			rootEntry := compressEntry{
				SourcePath:  input.Path,
				ArchivePath: filepath.ToSlash(input.ArchiveRoot) + "/",
				Info:        input.Info,
				IsDir:       true,
			}
			if shouldSkipCompressEntry(rootEntry, excludes) {
				continue
			}
			if err := appendCompressEntry(rootEntry, seen, &entries); err != nil {
				return nil, err
			}
			err := filepath.WalkDir(input.Path, func(current string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if current == input.Path {
					return nil
				}
				rel, err := filepath.Rel(input.Path, current)
				if err != nil {
					return err
				}
				if !includeHidden && compressPathHasHiddenSegment(rel) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("symlinks are not supported: %s", current)
				}
				archivePath := filepath.ToSlash(filepath.Join(input.ArchiveRoot, rel))
				entry := compressEntry{
					SourcePath:  current,
					ArchivePath: archivePath,
					Info:        info,
					IsDir:       info.IsDir(),
				}
				if shouldSkipCompressEntry(entry, excludes) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if info.IsDir() {
					entry.ArchivePath += "/"
				}
				return appendCompressEntry(entry, seen, &entries)
			})
			if err != nil {
				return nil, err
			}
			continue
		}

		entry := compressEntry{
			SourcePath:  input.Path,
			ArchivePath: filepath.ToSlash(input.ArchiveRoot),
			Info:        input.Info,
			IsDir:       false,
		}
		if shouldSkipCompressEntry(entry, excludes) {
			continue
		}
		if err := appendCompressEntry(entry, seen, &entries); err != nil {
			return nil, err
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ArchivePath < entries[j].ArchivePath
	})
	return entries, nil
}

func appendCompressEntry(entry compressEntry, seen map[string]struct{}, out *[]compressEntry) error {
	key := entry.ArchivePath
	if _, ok := seen[key]; ok {
		return fmt.Errorf("archive path collision: %s", entry.ArchivePath)
	}
	seen[key] = struct{}{}
	*out = append(*out, entry)
	return nil
}

func shouldSkipCompressEntry(entry compressEntry, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}
	relPath := strings.TrimPrefix(entry.ArchivePath, "/")
	sourceRelative := filepath.ToSlash(entry.SourcePath)
	base := path.Base(strings.TrimSuffix(relPath, "/"))
	for _, pattern := range excludes {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if compressPatternMatch(pattern, relPath) || compressPatternMatch(pattern, sourceRelative) || compressPatternMatch(pattern, base) {
			return true
		}
	}
	return false
}

func compressPatternMatch(pattern string, value string) bool {
	value = filepath.ToSlash(value)
	if ok, err := path.Match(pattern, value); err == nil && ok {
		return true
	}
	if ok, err := path.Match(pattern, path.Base(value)); err == nil && ok {
		return true
	}
	return false
}

func compressPathHasHiddenSegment(relPath string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(relPath), "/") {
		if isHiddenName(segment) {
			return true
		}
	}
	return false
}

func isHiddenName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && strings.HasPrefix(name, ".")
}

func compressArchiveRootForInput(inputPath string, info os.FileInfo) string {
	base := filepath.Base(inputPath)
	if info.IsDir() {
		return base
	}
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		return base
	}
	return base
}

func resolveCompressOutputPath(opts compressOptions, roots []compressInput, cwd string) (string, error) {
	if strings.TrimSpace(opts.OutputPath) != "" {
		return filepath.Clean(opts.OutputPath), nil
	}

	baseDir := cwd
	if len(roots) == 1 {
		baseDir = filepath.Dir(roots[0].Path)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = defaultCompressBaseName(roots)
	}
	if name == "" {
		name = "archive"
	}
	return filepath.Join(baseDir, name+defaultExtensionForCompressFormat(opts.Format)), nil
}

func defaultCompressBaseName(roots []compressInput) string {
	if len(roots) != 1 {
		return "archive"
	}
	root := roots[0]
	if root.IsDir {
		return filepath.Base(root.Path)
	}
	base := filepath.Base(root.Path)
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		return base
	}
	return stem
}

func ensureCompressOutputPath(outputPath string, roots []compressInput, force bool) error {
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	outputAbs = filepath.Clean(outputAbs)

	for _, root := range roots {
		rootAbs, err := filepath.Abs(root.Path)
		if err != nil {
			return err
		}
		rootAbs = filepath.Clean(rootAbs)
		if sameCompressPath(rootAbs, outputAbs) {
			return errors.New("output path would overwrite the source path; choose a different name or use --out")
		}
		if root.IsDir {
			inside, err := pathWithinCompressDir(outputAbs, rootAbs)
			if err != nil {
				return err
			}
			if inside {
				return fmt.Errorf("output path must not be inside source directory %s", root.Path)
			}
		}
	}

	if _, err := os.Stat(outputAbs); err == nil && !force {
		return fmt.Errorf("%s already exists; rerun with --force to replace it", outputAbs)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sameCompressPath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func pathWithinCompressDir(pathName, dir string) (bool, error) {
	rel, err := filepath.Rel(dir, pathName)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." {
		return false, nil
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(rel, prefix), nil
}

func writeCompressArchive(plan compressPlan, force bool) error {
	parent := filepath.Dir(plan.OutputPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(parent, ".jot-compress-*"+defaultExtensionForCompressFormat(plan.Format))
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeCompressArchiveToWriter(tmp, plan); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if force {
		_ = os.Remove(plan.OutputPath)
	}
	if err := os.Rename(tmpPath, plan.OutputPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func writeCompressArchiveToWriter(w io.Writer, plan compressPlan) error {
	switch plan.Format {
	case "zip":
		return writeZipArchive(w, plan.Entries)
	case "tar":
		return writeTarArchive(w, plan.Entries, false)
	case "tar.gz":
		return writeTarArchive(w, plan.Entries, true)
	default:
		return fmt.Errorf("unsupported archive format %q", plan.Format)
	}
}

func writeZipArchive(w io.Writer, entries []compressEntry) error {
	zw := zip.NewWriter(w)
	for _, entry := range entries {
		if entry.IsDir {
			if err := writeZipDirEntry(zw, entry); err != nil {
				_ = zw.Close()
				return err
			}
			continue
		}
		if err := writeZipFileEntry(zw, entry); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func writeZipDirEntry(zw *zip.Writer, entry compressEntry) error {
	header, err := zip.FileInfoHeader(entry.Info)
	if err != nil {
		return err
	}
	header.Name = strings.TrimSuffix(filepath.ToSlash(entry.ArchivePath), "/") + "/"
	header.Method = zip.Store
	header.Modified = entry.Info.ModTime()
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(nil)
	return err
}

func writeZipFileEntry(zw *zip.Writer, entry compressEntry) error {
	header, err := zip.FileInfoHeader(entry.Info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(entry.ArchivePath)
	header.Method = zip.Deflate
	header.Modified = entry.Info.ModTime()
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	file, err := os.Open(entry.SourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func writeTarArchive(w io.Writer, entries []compressEntry, useGzip bool) error {
	var out io.Writer = w
	var gz *gzip.Writer
	if useGzip {
		gz = gzip.NewWriter(w)
		out = gz
	}
	tw := tar.NewWriter(out)
	for _, entry := range entries {
		if err := writeTarEntry(tw, entry); err != nil {
			_ = tw.Close()
			if gz != nil {
				_ = gz.Close()
			}
			return err
		}
	}
	if err := tw.Close(); err != nil {
		if gz != nil {
			_ = gz.Close()
		}
		return err
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}

func writeTarEntry(tw *tar.Writer, entry compressEntry) error {
	hdr, err := tar.FileInfoHeader(entry.Info, "")
	if err != nil {
		return err
	}
	name := filepath.ToSlash(entry.ArchivePath)
	if entry.IsDir && !strings.HasSuffix(name, "/") {
		name += "/"
	}
	hdr.Name = name
	hdr.ModTime = entry.Info.ModTime()
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if entry.IsDir {
		return nil
	}
	file, err := os.Open(entry.SourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(tw, file)
	return err
}

func buildCompressCommandTip(sourceLabel string, opts compressOptions, output compressOutputChoice) string {
	parts := []string{"jot compress"}
	if strings.TrimSpace(sourceLabel) != "" {
		parts = append(parts, sourceLabel)
	}
	if opts.Format != "" && !(opts.Format == "zip" && opts.Name == "" && opts.OutputPath == "" && len(opts.Excludes) == 0 && !opts.IncludeHidden) {
		parts = append(parts, "--format", opts.Format)
	}
	if opts.OutputPath != "" {
		parts = append(parts, "--out", opts.OutputPath)
	} else if opts.Name != "" {
		parts = append(parts, "--name", opts.Name)
	}
	if opts.Force {
		parts = append(parts, "--force")
	}
	if opts.DryRun {
		parts = append(parts, "--dry-run")
	}
	if opts.Quiet {
		parts = append(parts, "--quiet")
	}
	if opts.IncludeHidden {
		parts = append(parts, "--include-hidden")
	}
	for _, pattern := range opts.Excludes {
		parts = append(parts, "--exclude", pattern)
	}
	return strings.Join(parts, " ")
}

func listCompressCandidates(dir string, includeHidden bool) ([]compressCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var candidates []compressCandidate
	for _, entry := range entries {
		name := entry.Name()
		if !includeHidden && isHiddenName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		candidate := compressCandidate{
			Path:        path,
			DisplayName: name,
			IsDir:       info.IsDir(),
		}
		if info.IsDir() {
			candidate.SizeLabel = "dir"
		} else if info.Size() < 1024 {
			candidate.SizeLabel = "< 1 KB"
		} else {
			candidate.SizeLabel = fmt.Sprintf("%.0f KB", float64(info.Size())/1024.0)
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].IsDir != candidates[j].IsDir {
			return candidates[i].IsDir && !candidates[j].IsDir
		}
		return strings.ToLower(candidates[i].DisplayName) < strings.ToLower(candidates[j].DisplayName)
	})
	return candidates, nil
}

func resolveCompressPath(dir, selection string) (string, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return "", errors.New("source path must be provided")
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return filepath.Clean(selection), nil
}

func compressCommandPathForDisplay(dir, selection string) string {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return selection
	}
	if filepath.IsAbs(selection) {
		return selection
	}
	return filepath.Clean(selection)
}

func looksLikeArchivePath(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".tar.gz")
}

func isYesAnswer(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "1", "true":
		return true
	default:
		return false
	}
}
