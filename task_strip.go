package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"strconv"
)

type stripSourceKind int

const (
	stripSourceFile stripSourceKind = iota + 1
	stripSourceDir
	stripSourceGlob
)

type stripOptions struct {
	sources   []string
	outDir    string
	inPlace   bool
	force     bool
	recursive bool
	dryRun    bool
	quiet     bool
	help      bool
}

type stripCandidate struct {
	SourcePath string
	SourceAbs  string
	RelPath    string
	Kind       stripSourceKind
}

type stripPlanItem struct {
	SourcePath string
	SourceAbs  string
	OutputPath string
	Format     string
	Warning    string
}

var stripTempCounter uint64

func jotStrip(w io.Writer, args []string) error {
	return stripCLI(os.Stdin, w, args, os.Getwd)
}

func stripCLI(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	opts, err := parseStripArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, writeErr := io.WriteString(w, renderStripHelp(isTTY(w)))
			return writeErr
		}
		return err
	}
	_ = stdin
	cwd, err := getwd()
	if err != nil {
		return err
	}
	return executeStrip(w, cwd, opts)
}

func renderStripHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot strip", "Strip local image metadata by re-encoding jpg, jpeg, png, and gif files.")
	writeUsageSection(&b, style, []string{
		"jot strip photo.jpg",
		"jot strip ./images --recursive",
		"jot strip ./images --out-dir ./stripped",
		"jot strip photo.jpg --in-place --force",
	}, []string{
		"Single files write a `-stripped` sibling by default.",
		"Folder and glob inputs write into a local `stripped/` tree unless `--out-dir` is set.",
		"GIF animation frames are preserved when possible; metadata and comments are stripped.",
		"JPEG output is re-encoded, so a small amount of generation loss is possible even when the format stays the same.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--out-dir PATH", description: "Write output into a specific directory tree."},
		{name: "--in-place", description: "Rewrite the original files instead of creating sibling copies."},
		{name: "--force", description: "Allow overwriting an existing destination."},
		{name: "--recursive", description: "Walk folders recursively when the source is a directory."},
		{name: "--dry-run", description: "Show the planned outputs without writing files."},
		{name: "--quiet", description: "Suppress success summaries."},
	})
	writeExamplesSection(&b, style, []string{
		"jot strip photo.jpg",
		"jot strip ./images --recursive",
		"jot strip ./images --out-dir ./stripped",
		"jot strip photo.jpg --in-place --force",
	})
	return b.String()
}

func runStripTask(stdin io.Reader, w io.Writer, dir string) error {
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprint(w, ui.header("Strip Metadata")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("images in this folder")); err != nil {
		return err
	}
	images, err := listStripImages(dir)
	if err != nil {
		return err
	}
	for i, imgPath := range images {
		meta := ""
		if info, statErr := os.Stat(imgPath); statErr == nil {
			kb := float64(info.Size()) / 1024.0
			if kb < 1 {
				meta = "< 1 KB"
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

	selection, err := promptLine(reader, w, ui.styledPrompt("Source path", hintForStripSelection(images)))
	if err != nil {
		return err
	}
	if selection == "" {
		if len(images) == 1 {
			selection = images[0]
		} else {
			return errors.New("source path must be provided")
		}
	}
	if idx, convErr := strconvAtoi(selection); convErr == nil && idx >= 1 && idx <= len(images) {
		selection = images[idx-1]
	}

	opts := stripOptions{sources: []string{selection}}
	if info, statErr := os.Stat(resolveStripPath(dir, selection)); statErr == nil && info.IsDir() {
		if _, err := fmt.Fprint(w, ui.sectionLabel("folder options")); err != nil {
			return err
		}
		recursive, err := promptYesNo(reader, w, ui.styledPrompt("Recursive walk", "n"))
		if err != nil {
			return err
		}
		opts.recursive = recursive
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if err := executeStrip(w, dir, opts); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip("next time: jot strip " + filepath.Base(selection))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func parseStripArgs(args []string) (stripOptions, error) {
	var opts stripOptions
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		switch arg {
		case "-h", "--help":
			opts.help = true
		case "--in-place":
			opts.inPlace = true
		case "--force":
			opts.force = true
		case "--recursive", "-r":
			opts.recursive = true
		case "--dry-run":
			opts.dryRun = true
		case "--quiet":
			opts.quiet = true
		case "--out-dir":
			i++
			if i >= len(args) {
				return stripOptions{}, errors.New("--out-dir requires a path")
			}
			opts.outDir = strings.TrimSpace(args[i])
		default:
			if strings.HasPrefix(arg, "--out-dir=") {
				opts.outDir = strings.TrimSpace(strings.TrimPrefix(arg, "--out-dir="))
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return stripOptions{}, fmt.Errorf("unknown flag %q", arg)
			}
			opts.sources = append(opts.sources, arg)
		}
	}

	if opts.help {
		return opts, nil
	}
	if len(opts.sources) == 0 {
		return stripOptions{}, errors.New("choose one or more sources: <path>, folder, or glob")
	}
	if opts.inPlace && opts.outDir != "" {
		return stripOptions{}, errors.New("--in-place and --out-dir cannot be used together")
	}
	return opts, nil
}

func executeStrip(w io.Writer, cwd string, opts stripOptions) error {
	candidates, err := expandStripSources(cwd, opts.sources, opts.recursive)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return errors.New("no supported image files were found")
	}

	useSibling := len(opts.sources) == 1 && len(candidates) == 1 && candidates[0].Kind == stripSourceFile && !opts.inPlace && strings.TrimSpace(opts.outDir) == ""
	outputRoot := opts.outDir
	if strings.TrimSpace(outputRoot) == "" {
		outputRoot = filepath.Join(cwd, "stripped")
	} else if !filepath.IsAbs(outputRoot) {
		outputRoot = resolveStripPath(cwd, outputRoot)
	}

	plan := make([]stripPlanItem, 0, len(candidates))
	for _, candidate := range candidates {
		outputPath, err := resolveStripOutputPath(cwd, outputRoot, candidate, useSibling, opts.inPlace)
		if err != nil {
			return err
		}
		if err := ensureStripWritable(candidate.SourceAbs, outputPath, opts.force); err != nil {
			return err
		}
		format := stripFormatFromPath(candidate.SourceAbs)
		warning := stripFormatWarning(format)
		plan = append(plan, stripPlanItem{
			SourcePath: candidate.RelPath,
			SourceAbs:  candidate.SourceAbs,
			OutputPath: outputPath,
			Format:     format,
			Warning:    warning,
		})
	}

	if opts.dryRun {
		return renderStripPlan(w, plan, true)
	}

	for _, item := range plan {
		data, err := stripImageFile(item.SourceAbs, item.Format)
		if err != nil {
			return err
		}
		if err := writeAtomicBytes(item.OutputPath, data, 0o644, opts.force); err != nil {
			return err
		}
		if !opts.quiet {
			ui := newTermUI(w)
			if _, err := fmt.Fprintln(w, ui.success(fmt.Sprintf("stripped %s -> %s", item.SourcePath, item.OutputPath))); err != nil {
				return err
			}
			if item.Warning != "" {
				if _, err := fmt.Fprintln(w, ui.warnLine(item.Warning)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func renderStripPlan(w io.Writer, plan []stripPlanItem, dryRun bool) error {
	ui := newTermUI(w)
	label := "planned"
	if dryRun {
		label = "dry run"
	}
	if _, err := fmt.Fprintln(w, ui.sectionLabel(label)); err != nil {
		return err
	}
	for i, item := range plan {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(item.SourcePath), "-> "+item.OutputPath, "")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ui.tip("no files were written")); err != nil {
		return err
	}
	return nil
}

func expandStripSources(cwd string, sources []string, recursive bool) ([]stripCandidate, error) {
	seen := map[string]struct{}{}
	var candidates []stripCandidate
	for _, source := range sources {
		matches, _, err := expandStripSource(cwd, source, recursive)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if _, ok := seen[match.SourceAbs]; ok {
				continue
			}
			seen[match.SourceAbs] = struct{}{}
			candidates = append(candidates, match)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].SourceAbs < candidates[j].SourceAbs
	})
	return candidates, nil
}

func expandStripSource(cwd, source string, recursive bool) ([]stripCandidate, stripSourceKind, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, stripSourceFile, errors.New("source path must be provided")
	}
	if containsGlobMeta(source) {
		return expandStripGlob(cwd, source, recursive)
	}
	abs := resolveStripPath(cwd, source)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, stripSourceFile, err
	}
	if info.IsDir() {
		return expandStripDir(cwd, abs, recursive)
	}
	if !isSupportedStripImagePath(abs) {
		return nil, stripSourceFile, fmt.Errorf("%s is not a supported image file", source)
	}
	rel := stripRelativePath(cwd, abs)
	return []stripCandidate{{SourcePath: source, SourceAbs: abs, RelPath: rel, Kind: stripSourceFile}}, stripSourceFile, nil
}

func expandStripGlob(cwd, pattern string, recursive bool) ([]stripCandidate, stripSourceKind, error) {
	absPattern := pattern
	if !filepath.IsAbs(absPattern) {
		absPattern = filepath.Join(cwd, absPattern)
	}
	matches, err := filepath.Glob(absPattern)
	if err != nil {
		return nil, stripSourceGlob, err
	}
	if len(matches) == 0 {
		return nil, stripSourceGlob, fmt.Errorf("no matches for %q", pattern)
	}
	var out []stripCandidate
	for _, match := range matches {
		info, statErr := os.Stat(match)
		if statErr != nil {
			return nil, stripSourceGlob, statErr
		}
		if info.IsDir() {
			items, _, walkErr := expandStripDir(cwd, match, recursive)
			if walkErr != nil {
				return nil, stripSourceGlob, walkErr
			}
			out = append(out, items...)
			continue
		}
		if !isSupportedStripImagePath(match) {
			continue
		}
		out = append(out, stripCandidate{
			SourcePath: pattern,
			SourceAbs:  match,
			RelPath:    stripRelativePath(cwd, match),
			Kind:       stripSourceGlob,
		})
	}
	return out, stripSourceGlob, nil
}

func expandStripDir(cwd, dir string, recursive bool) ([]stripCandidate, stripSourceKind, error) {
	var out []stripCandidate
	if !recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, stripSourceDir, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			full := filepath.Join(dir, entry.Name())
			if !isSupportedStripImagePath(full) {
				continue
			}
			out = append(out, stripCandidate{
				SourcePath: dir,
				SourceAbs:  full,
				RelPath:    stripRelativePath(cwd, full),
				Kind:       stripSourceDir,
			})
		}
		return out, stripSourceDir, nil
	}

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !isSupportedStripImagePath(path) {
			return nil
		}
		out = append(out, stripCandidate{
			SourcePath: dir,
			SourceAbs:  path,
			RelPath:    stripRelativePath(cwd, path),
			Kind:       stripSourceDir,
		})
		return nil
	})
	if err != nil {
		return nil, stripSourceDir, err
	}
	return out, stripSourceDir, nil
}

func resolveStripOutputPath(cwd, outputRoot string, candidate stripCandidate, useSibling, inPlace bool) (string, error) {
	if inPlace {
		return candidate.SourceAbs, nil
	}
	base := filepath.Base(candidate.SourceAbs)
	stripName := siblingStripName(base)
	if useSibling {
		return filepath.Join(filepath.Dir(candidate.SourceAbs), stripName), nil
	}
	if strings.TrimSpace(outputRoot) == "" {
		outputRoot = filepath.Join(cwd, "stripped")
	}
	rel := stripRelativePath(cwd, candidate.SourceAbs)
	dir := filepath.Dir(rel)
	if dir == "." || dir == string(filepath.Separator) {
		return filepath.Join(outputRoot, stripName), nil
	}
	return filepath.Join(outputRoot, dir, stripName), nil
}

func siblingStripName(base string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = base
	}
	return stem + "-stripped" + ext
}

func ensureStripWritable(sourcePath, outputPath string, force bool) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if sourceAbs == outputAbs && !force {
		return errors.New("output path would overwrite the source file; use --in-place --force")
	}
	if _, err := os.Stat(outputAbs); err == nil && !force {
		return fmt.Errorf("%s already exists; rerun with --force", outputAbs)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func stripImageFile(sourcePath, format string) ([]byte, error) {
	switch format {
	case "png":
		file, err := os.Open(sourcePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		img, _, err := image.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("could not decode %s as a PNG image: %w", sourcePath, err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "jpg", "jpeg":
		file, err := os.Open(sourcePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		img, _, err := image.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("could not decode %s as a JPEG image: %w", sourcePath, err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "gif":
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, err
		}
		decoded, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("could not decode %s as a GIF image: %w", sourcePath, err)
		}
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, decoded); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
}

func writeAtomicBytes(path string, data []byte, perm os.FileMode, overwrite bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; rerun with --force", path)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, ".jot-strip-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if overwrite {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func stripFormatFromPath(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

func stripFormatWarning(format string) string {
	switch format {
	case "jpg", "jpeg":
		return "JPEG output is re-encoded; metadata is stripped and a small amount of generation loss is possible."
	case "gif":
		return "GIF frames are preserved when possible; comments and metadata are stripped."
	default:
		return ""
	}
}

func isSupportedStripImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	default:
		return false
	}
}

func stripRelativePath(cwd, path string) string {
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return filepath.Base(path)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.Base(path)
	}
	return rel
}

func resolveStripPath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

func containsGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func promptYesNo(reader *bufio.Reader, w io.Writer, prompt string) (bool, error) {
	answer, err := promptLine(reader, w, prompt)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "n", "no":
		return false, nil
	case "y", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("expected yes or no, got %q", answer)
	}
}

func hintForStripSelection(images []string) string {
	if len(images) == 1 {
		return "1"
	}
	return ""
}

func listStripImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var images []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isSupportedStripImagePath(entry.Name()) {
			continue
		}
		images = append(images, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(images)
	return images, nil
}

func strconvAtoi(text string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(text))
}
