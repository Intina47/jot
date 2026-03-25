package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type resizeMode string

const (
	resizeModeFit     resizeMode = "fit"
	resizeModeFill    resizeMode = "fill"
	resizeModeStretch resizeMode = "stretch"
)

type resizeSourceKind int

const (
	resizeSourceFile resizeSourceKind = iota + 1
	resizeSourceFolder
	resizeSourceGlob
)

type resizeRequest struct {
	selector  string
	sizeText  string
	width     int
	height    int
	mode      resizeMode
	outDir    string
	inPlace   bool
	force     bool
	recursive bool
	dryRun    bool
	quiet     bool
}

type resizeTarget struct {
	SourcePath string
	RelPath    string
	OutputPath string
	Format     string
	Width      int
	Height     int
	Mode       resizeMode
	SourceKind resizeSourceKind
}

type resizeResult struct {
	OutputPath string
	Width      int
	Height     int
	Mode       resizeMode
}

// jotResize is the direct command entry point. It is self-contained so main.go
// can wire it in later without moving the resize logic again.
func jotResize(w io.Writer, args []string) error {
	return resizeCLI(os.Stdin, w, args, os.Getwd)
}

func resizeCLI(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	req, helpRequested, err := parseResizeArgs(args)
	if err != nil {
		return err
	}
	if helpRequested {
		_, writeErr := io.WriteString(w, renderResizeHelp(isTTY(w)))
		return writeErr
	}
	cwd, err := getwd()
	if err != nil {
		return err
	}
	_ = stdin
	return executeResizeCommand(w, cwd, req)
}

func renderResizeHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot resize", "Resize local images from the terminal without leaving the folder.")
	writeUsageSection(&b, style, []string{
		"jot resize logo.png 512x512",
		"jot resize ./photos 1280x720 --recursive",
		"jot resize ./images/*.png --size 640x480 --mode fill",
	}, []string{
		"`jot resize` keeps the output format the same as the source image.",
		"Single files write a sibling output by default; folders and globs write into `resized/` unless `--out-dir` is set.",
		"`jot task resize` is the guided path that will reuse the same resize logic when wired in.",
	})
	writeCommandSection(&b, style, []helpCommand{
		{name: "--size WIDTHxHEIGHT", description: "Target box to fit, fill, or stretch into."},
		{name: "--mode fit|fill|stretch", description: "Choose aspect-ratio preserving fit, center-cropped fill, or exact stretch."},
		{name: "--out-dir PATH", description: "Write batch output into a specific directory."},
		{name: "--in-place", description: "Rewrite the source files instead of writing siblings."},
		{name: "--force", description: "Replace any existing output files."},
		{name: "--recursive", description: "Walk nested folders when the source is a directory."},
		{name: "--dry-run", description: "Show the planned output paths without writing files."},
		{name: "--quiet", description: "Suppress the success summary."},
	})
	writeExamplesSection(&b, style, []string{
		"jot resize logo.png 512x512",
		"jot resize ./photos 1280x720 --recursive",
		"jot resize ./images/*.png --size 640x480 --mode fill",
		"jot task resize",
	})
	return b.String()
}

func runResizeTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	candidates, err := listResizeCandidates(dir)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, ui.header("Resize")); err != nil {
		return err
	}
	if len(candidates) > 0 {
		if _, err := fmt.Fprint(w, ui.sectionLabel("images in this folder")); err != nil {
			return err
		}
		for i, candidate := range candidates {
			meta := candidate.SizeLabel
			if meta == "" {
				meta = "image"
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, candidate.DisplayName, "", meta)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}

	sourcePath, err := promptResizeSource(reader, w, ui, dir, candidates)
	if err != nil {
		return err
	}

	sizeText, err := promptLine(reader, w, ui.styledPrompt("Target size", "WIDTHxHEIGHT"))
	if err != nil {
		return err
	}
	width, height, err := parseResizeSize(sizeText)
	if err != nil {
		return err
	}

	mode, err := promptResizeMode(reader, w, ui)
	if err != nil {
		return err
	}

	req := resizeRequest{
		selector: sourcePath,
		sizeText: sizeText,
		width:    width,
		height:   height,
		mode:     mode,
	}
	if err := executeResizeCommand(w, dir, req); err != nil {
		return err
	}

	tip := resizeTip(req, sourcePath)
	if tip != "" {
		if _, err := fmt.Fprintln(w, ui.tip(tip)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func parseResizeArgs(args []string) (resizeRequest, bool, error) {
	req := resizeRequest{mode: resizeModeFit}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			return req, true, nil
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--mode":
				if !hasValue {
					if i+1 >= len(args) {
						return req, false, fmt.Errorf("missing value for %s", name)
					}
					i++
					value = args[i]
				}
				req.mode = resizeMode(strings.ToLower(strings.TrimSpace(value)))
			case "--size":
				if !hasValue {
					if i+1 >= len(args) {
						return req, false, fmt.Errorf("missing value for %s", name)
					}
					i++
					value = args[i]
				}
				req.sizeText = strings.TrimSpace(value)
			case "--out-dir":
				if !hasValue {
					if i+1 >= len(args) {
						return req, false, fmt.Errorf("missing value for %s", name)
					}
					i++
					value = args[i]
				}
				req.outDir = strings.TrimSpace(value)
			case "--in-place":
				req.inPlace = true
			case "--force":
				req.force = true
			case "--recursive":
				req.recursive = true
			case "--dry-run":
				req.dryRun = true
			case "--quiet":
				req.quiet = true
			default:
				return req, false, fmt.Errorf("unsupported flag %q", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	switch {
	case req.sizeText != "" && len(positional) == 1:
		req.selector = positional[0]
	case req.sizeText == "" && len(positional) == 2:
		req.selector = positional[0]
		req.sizeText = positional[1]
	default:
		return req, false, errors.New("usage: jot resize <path|glob> <WIDTHxHEIGHT>")
	}

	if req.mode != resizeModeFit && req.mode != resizeModeFill && req.mode != resizeModeStretch {
		return req, false, fmt.Errorf("unsupported resize mode %q", req.mode)
	}

	var err error
	req.width, req.height, err = parseResizeSize(req.sizeText)
	if err != nil {
		return req, false, err
	}
	if req.width <= 0 || req.height <= 0 {
		return req, false, errors.New("target size must be positive")
	}
	if req.inPlace && req.outDir != "" {
		return req, false, errors.New("--in-place cannot be combined with --out-dir")
	}
	return req, false, nil
}

func parseResizeSize(text string) (int, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size %q; use WIDTHxHEIGHT such as 800x600", text)
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("invalid width in %q; use WIDTHxHEIGHT such as 800x600", text)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("invalid height in %q; use WIDTHxHEIGHT such as 800x600", text)
	}
	return w, h, nil
}

func executeResizeCommand(w io.Writer, cwd string, req resizeRequest) error {
	if req.selector == "" {
		return errors.New("image path or glob must be provided")
	}

	kind, inputs, root, err := collectResizeInputs(cwd, req.selector, req.recursive)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no supported raster images found in %q", req.selector)
	}

	outDir := req.outDir
	if outDir == "" && kind != resizeSourceFile && !req.inPlace {
		outDir = filepath.Join(cwd, "resized")
	}

	targets := make([]resizeTarget, 0, len(inputs))
	for _, input := range inputs {
		targetPath, err := resizeOutputPath(input, root, cwd, outDir, req.width, req.height, req.inPlace)
		if err != nil {
			return err
		}
		targets = append(targets, resizeTarget{
			SourcePath: input,
			RelPath:    relativeResizePath(root, cwd, input, kind),
			OutputPath: targetPath,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(input)), "."),
			Width:      req.width,
			Height:     req.height,
			Mode:       req.mode,
			SourceKind: kind,
		})
	}

	if req.dryRun {
		for _, target := range targets {
			if _, err := fmt.Fprintln(w, resizePreviewLine(target)); err != nil {
				return err
			}
		}
		return nil
	}

	var results []resizeResult
	for _, target := range targets {
		result, err := resizeSingleImage(target, req)
		if err != nil {
			return err
		}
		results = append(results, result)
		if !req.quiet {
			if _, err := fmt.Fprintln(w, resizeSuccessLine(result)); err != nil {
				return err
			}
		}
	}
	return nil
}

func resizeSuccessLine(result resizeResult) string {
	return fmt.Sprintf("  %s  %s", "✓", fmt.Sprintf("wrote %s (%dx%d, %s)", result.OutputPath, result.Width, result.Height, result.Mode))
}

func resizePreviewLine(target resizeTarget) string {
	return fmt.Sprintf("would write %s (%dx%d, %s)", target.OutputPath, target.Width, target.Height, target.Mode)
}

func resizeTip(req resizeRequest, sourcePath string) string {
	base := filepath.Base(sourcePath)
	if req.inPlace {
		return fmt.Sprintf("next time: jot resize %s %s --mode %s --in-place", base, req.sizeText, req.mode)
	}
	if req.outDir != "" {
		return fmt.Sprintf("next time: jot resize %s %s --mode %s --out-dir %q", base, req.sizeText, req.mode, req.outDir)
	}
	if req.selector != "" && req.selector != base {
		return fmt.Sprintf("next time: jot resize %s %s --mode %s", base, req.sizeText, req.mode)
	}
	return fmt.Sprintf("next time: jot resize %s %s --mode %s", base, req.sizeText, req.mode)
}

func collectResizeInputs(cwd, selector string, recursive bool) (resizeSourceKind, []string, string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return 0, nil, "", errors.New("image path or glob must be provided")
	}

	if containsResizeGlob(selector) {
		pattern := selector
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(cwd, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return 0, nil, "", err
		}
		var inputs []string
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			if !isSupportedRasterPath(match) {
				continue
			}
			inputs = append(inputs, match)
		}
		sort.Strings(inputs)
		if len(inputs) == 0 {
			return 0, nil, "", fmt.Errorf("no supported raster images matched %q", selector)
		}
		return resizeSourceGlob, inputs, cwd, nil
	}

	path := selector
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, nil, "", err
	}
	if info.IsDir() {
		inputs, err := collectResizeFilesFromDir(path, recursive)
		if err != nil {
			return 0, nil, "", err
		}
		if len(inputs) == 0 {
			return 0, nil, "", fmt.Errorf("no supported raster images found under %q", selector)
		}
		return resizeSourceFolder, inputs, path, nil
	}
	if !isSupportedRasterPath(path) {
		return 0, nil, "", fmt.Errorf("%s is not a supported raster image; use .png, .jpg, .jpeg, or .gif", selector)
	}
	return resizeSourceFile, []string{path}, filepath.Dir(path), nil
}

func collectResizeFilesFromDir(root string, recursive bool) ([]string, error) {
	var inputs []string
	if recursive {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if isSupportedRasterPath(path) {
				inputs = append(inputs, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if isSupportedRasterPath(path) {
				inputs = append(inputs, path)
			}
		}
	}
	sort.Strings(inputs)
	return inputs, nil
}

func relativeResizePath(root, cwd, path string, kind resizeSourceKind) string {
	switch kind {
	case resizeSourceFile:
		return ""
	case resizeSourceFolder:
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return filepath.Base(path)
		}
		return rel
	case resizeSourceGlob:
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return filepath.Base(path)
		}
		return rel
	default:
		return filepath.Base(path)
	}
}

func resizeOutputPath(sourcePath, root, cwd, outDir string, width, height int, inPlace bool) (string, error) {
	if inPlace {
		return sourcePath, nil
	}
	base := filepath.Base(sourcePath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	outputName := fmt.Sprintf("%s-%dx%d%s", stem, width, height, ext)

	if outDir == "" {
		return filepath.Join(filepath.Dir(sourcePath), outputName), nil
	}
	rel, err := filepath.Rel(root, sourcePath)
	if err != nil {
		rel = filepath.Base(sourcePath)
	}
	if rel == "." {
		rel = filepath.Base(sourcePath)
	}
	if filepath.Clean(outDir) == filepath.Clean(cwd) && root != cwd {
		return filepath.Join(outDir, outputName), nil
	}
	if root != cwd {
		dir := filepath.Dir(rel)
		if dir == "." {
			return filepath.Join(outDir, outputName), nil
		}
		return filepath.Join(outDir, dir, outputName), nil
	}
	if strings.Contains(rel, string(filepath.Separator)) {
		return filepath.Join(outDir, filepath.Dir(rel), outputName), nil
	}
	return filepath.Join(outDir, outputName), nil
}

func resizeSingleImage(target resizeTarget, req resizeRequest) (resizeResult, error) {
	file, err := os.Open(target.SourcePath)
	if err != nil {
		return resizeResult{}, err
	}

	src, _, err := image.Decode(file)
	if err != nil {
		_ = file.Close()
		return resizeResult{}, fmt.Errorf("could not decode %s as an image: %w", target.SourcePath, err)
	}
	if err := file.Close(); err != nil {
		return resizeResult{}, err
	}

	resized, outW, outH, err := resizeImageForMode(src, target.Width, target.Height, req.mode)
	if err != nil {
		return resizeResult{}, err
	}

	var output bytes.Buffer
	ext := strings.ToLower(filepath.Ext(target.SourcePath))
	switch ext {
	case ".png":
		if err := png.Encode(&output, resized); err != nil {
			return resizeResult{}, err
		}
	case ".jpg", ".jpeg":
		if hasAlpha(resized) {
			resized = flattenImageOnBackground(resized, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
		if err := jpeg.Encode(&output, resized, &jpeg.Options{Quality: 92}); err != nil {
			return resizeResult{}, err
		}
	case ".gif":
		if err := gif.Encode(&output, resized, &gif.Options{NumColors: 256}); err != nil {
			return resizeResult{}, err
		}
	default:
		return resizeResult{}, fmt.Errorf("unsupported raster output format %q", ext)
	}

	if req.dryRun {
		return resizeResult{OutputPath: target.OutputPath, Width: outW, Height: outH, Mode: req.mode}, nil
	}

	if err := writeResizeOutput(target.OutputPath, output.Bytes(), req.force, req.inPlace); err != nil {
		return resizeResult{}, err
	}
	return resizeResult{OutputPath: target.OutputPath, Width: outW, Height: outH, Mode: req.mode}, nil
}

func resizeImageForMode(src image.Image, targetW, targetH int, mode resizeMode) (image.Image, int, int, error) {
	if targetW <= 0 || targetH <= 0 {
		return nil, 0, 0, errors.New("target size must be positive")
	}
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, 0, 0, errors.New("source image has no drawable area")
	}

	switch mode {
	case resizeModeFit:
		scaleW := float64(targetW) / float64(srcW)
		scaleH := float64(targetH) / float64(srcH)
		scale := scaleW
		if scaleH < scale {
			scale = scaleH
		}
		outW := maxResizeInt(1, int(float64(srcW)*scale))
		outH := maxResizeInt(1, int(float64(srcH)*scale))
		return resizeImageBilinear(src, outW, outH), outW, outH, nil
	case resizeModeFill:
		scaleW := float64(targetW) / float64(srcW)
		scaleH := float64(targetH) / float64(srcH)
		scale := scaleW
		if scaleH > scale {
			scale = scaleH
		}
		coverW := maxResizeInt(1, int(float64(srcW)*scale+0.999999))
		coverH := maxResizeInt(1, int(float64(srcH)*scale+0.999999))
		scaled := resizeImageBilinear(src, coverW, coverH)
		return cropResizeCenter(scaled, targetW, targetH), targetW, targetH, nil
	case resizeModeStretch:
		return resizeImageBilinear(src, targetW, targetH), targetW, targetH, nil
	default:
		return nil, 0, 0, fmt.Errorf("unsupported resize mode %q", mode)
	}
}

func cropResizeCenter(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	offsetX := maxResizeInt(0, (bounds.Dx()-width)/2)
	offsetY := maxResizeInt(0, (bounds.Dy()-height)/2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y, src.At(bounds.Min.X+offsetX+x, bounds.Min.Y+offsetY+y))
		}
	}
	return dst
}

func writeResizeOutput(path string, content []byte, force, inPlace bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if !force && !inPlace {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; rerun with --force or choose another output path", path)
		}
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".jot-resize-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if _, err := temp.Write(content); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if inPlace || force {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func promptResizeSource(reader *bufio.Reader, w io.Writer, ui termUI, dir string, candidates []resizeCandidate) (string, error) {
	label := "Source"
	hint := ""
	if len(candidates) > 0 {
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		if len(candidates) == 1 {
			return candidates[0].Path, nil
		}
		return "", errors.New("source path must be provided")
	}
	if idx, err := strconv.Atoi(selection); err == nil {
		if idx < 1 || idx > len(candidates) {
			return "", fmt.Errorf("source selection must be between 1 and %d", len(candidates))
		}
		return candidates[idx-1].Path, nil
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return selection, nil
}

func promptResizeMode(reader *bufio.Reader, w io.Writer, ui termUI) (resizeMode, error) {
	if _, err := fmt.Fprint(w, ui.sectionLabel("mode")); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "fit", "Preserve aspect ratio and fit inside the target box", "")); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "fill", "Preserve aspect ratio and crop to the target box", "")); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "stretch", "Ignore aspect ratio and force the target dimensions", "")); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return "", err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select mode", "fit"))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "", "1", "fit":
		return resizeModeFit, nil
	case "2", "fill":
		return resizeModeFill, nil
	case "3", "stretch":
		return resizeModeStretch, nil
	default:
		return "", fmt.Errorf("unknown resize mode %q", selection)
	}
}

type resizeCandidate struct {
	Path        string
	DisplayName string
	SizeLabel   string
}

func listResizeCandidates(dir string) ([]resizeCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var candidates []resizeCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isSupportedRasterPath(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, resizeCandidate{
			Path:        filepath.Join(dir, entry.Name()),
			DisplayName: entry.Name(),
			SizeLabel:   formatResizeSizeLabel(info.Size()),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].DisplayName) < strings.ToLower(candidates[j].DisplayName)
	})
	return candidates, nil
}

func formatResizeSizeLabel(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%d KB", size/1024)
}

func containsResizeGlob(text string) bool {
	return strings.ContainsAny(text, "*?[")
}

func maxResizeInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
