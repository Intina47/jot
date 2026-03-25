package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type paletteOptions struct {
	InputPath   string
	Count       int
	Format      string
	IgnoreAlpha bool
	SortMode    string
}

type paletteColor struct {
	Hex   string `json:"hex"`
	Count int    `json:"count"`
	r     uint8
	g     uint8
	b     uint8
	hue   float64
	luma  float64
	key   int
}

type paletteOutput struct {
	Colors   []paletteColor `json:"colors"`
	Format   string         `json:"format"`
	SortMode string         `json:"sort"`
	Image    string         `json:"image"`
}

func paletteCommandEntry(args []string, stdin io.Reader, stdout, stderr io.Writer, getwd func() (string, error)) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "palette":
		if len(args) > 1 && isHelpFlag(args[1]) {
			if _, err := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
		code, err := runPaletteCLI(stdin, stdout, args[1:], getwd)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return true, 1
		}
		return true, code
	case "task":
		if len(args) == 1 {
			code, err := runPaletteTaskMenu(stdin, stdout, getwd)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, code
		}
		if isHelpFlag(args[1]) {
			if _, err := io.WriteString(stdout, renderTaskPaletteHelp(isTTY(stdout))); err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
		if strings.EqualFold(args[1], "palette") {
			if len(args) > 2 && isHelpFlag(args[2]) {
				if _, err := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); err != nil {
					fmt.Fprintln(stderr, err)
					return true, 1
				}
				return true, 0
			}
			if getwd == nil {
				getwd = os.Getwd
			}
			cwd, err := getwd()
			if err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			err = runPaletteTask(stdin, stdout, cwd)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
		return false, 0
	case "help":
		if len(args) == 2 && strings.EqualFold(args[1], "palette") {
			if _, err := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
		if len(args) == 2 && strings.EqualFold(args[1], "task") {
			if _, err := io.WriteString(stdout, renderTaskPaletteHelp(isTTY(stdout))); err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
		if len(args) == 3 && strings.EqualFold(args[1], "task") && strings.EqualFold(args[2], "palette") {
			if _, err := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); err != nil {
				fmt.Fprintln(stderr, err)
				return true, 1
			}
			return true, 0
		}
	}
	return false, 0
}

func runPaletteCLI(stdin io.Reader, stdout io.Writer, args []string, getwd func() (string, error)) (int, error) {
	opts, help, err := parsePaletteArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) || help {
			if _, writeErr := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); writeErr != nil {
				return 1, writeErr
			}
			return 0, nil
		}
		return 1, err
	}
	if help {
		if _, writeErr := io.WriteString(stdout, renderPaletteHelp(isTTY(stdout))); writeErr != nil {
			return 1, writeErr
		}
		return 0, nil
	}
	if getwd == nil {
		getwd = os.Getwd
	}
	cwd, err := getwd()
	if err != nil {
		return 1, err
	}
	_ = stdin
	if err := executePaletteCommand(stdout, cwd, opts); err != nil {
		return 1, err
	}
	return 0, nil
}

func jotPalette(w io.Writer, args []string) error {
	opts, help, err := parsePaletteArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) || help {
			_, writeErr := io.WriteString(w, renderPaletteHelp(isTTY(w)))
			return writeErr
		}
		return err
	}
	if help {
		_, writeErr := io.WriteString(w, renderPaletteHelp(isTTY(w)))
		return writeErr
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return executePaletteCommand(w, cwd, opts)
}

func runPaletteTask(stdin io.Reader, w io.Writer, dir string) error {
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprint(w, ui.header("Palette")); err != nil {
		return err
	}

	images, err := listConvertibleImages(dir)
	if err != nil {
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
	}

	sourcePath, err := promptPaletteImagePath(reader, w, ui, dir, images)
	if err != nil {
		return err
	}
	opts := paletteOptions{InputPath: sourcePath, Count: 5, Format: "hex", SortMode: "dominant"}

	if _, err := fmt.Fprint(w, ui.sectionLabel("palette")); err != nil {
		return err
	}
	countText, err := promptLine(reader, w, ui.styledPrompt("Count", "5"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(countText) != "" {
		count, parseErr := strconv.Atoi(strings.TrimSpace(countText))
		if parseErr != nil || count <= 0 {
			return fmt.Errorf("count must be a positive integer")
		}
		opts.Count = count
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("format")); err != nil {
		return err
	}
	for i, row := range []struct {
		key  string
		name string
		desc string
	}{
		{"hex", "hex", "Copyable color list"},
		{"swatch", "swatch", "Terminal swatches"},
		{"json", "json", "Structured output"},
	} {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	formatText, err := promptLine(reader, w, ui.styledPrompt("Select format", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(formatText)) {
	case "", "1", "hex":
		opts.Format = "hex"
	case "2", "swatch":
		opts.Format = "swatch"
	case "3", "json":
		opts.Format = "json"
	default:
		return fmt.Errorf("unknown format %q", formatText)
	}

	ignoreText, err := promptLine(reader, w, ui.styledPrompt("Ignore alpha", "n"))
	if err != nil {
		return err
	}
	opts.IgnoreAlpha = !isNoAnswer(ignoreText)

	if _, err := fmt.Fprint(w, ui.sectionLabel("sort")); err != nil {
		return err
	}
	for i, row := range []struct {
		name string
		desc string
	}{
		{"dominant", "Most common colors first"},
		{"hue", "Hue order"},
		{"luma", "Dark to light"},
	} {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	sortText, err := promptLine(reader, w, ui.styledPrompt("Select sort", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sortText)) {
	case "", "1", "dominant":
		opts.SortMode = "dominant"
	case "2", "hue":
		opts.SortMode = "hue"
	case "3", "luma":
		opts.SortMode = "luma"
	default:
		return fmt.Errorf("unknown sort mode %q", sortText)
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	colors, err := generatePalette(opts, dir)
	if err != nil {
		return err
	}
	if err := writePaletteOutput(w, colors, opts); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(renderPaletteTip(opts))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func runPaletteTaskMenu(stdin io.Reader, w io.Writer, getwd func() (string, error)) (int, error) {
	if getwd == nil {
		getwd = os.Getwd
	}
	dir, err := getwd()
	if err != nil {
		return 1, err
	}
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprint(w, ui.header("jot task")); err != nil {
		return 1, err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("tasks")); err != nil {
		return 1, err
	}
	rows := []struct {
		name string
		desc string
	}{
		{"convert image", "Turn raster images into png, jpg, gif, ico, or svg"},
		{"minify json", "Minify or pretty-print local JSON from files, text, or stdin"},
		{"encode base64", "Base64 encode or decode local files, text, or stdin"},
		{"hash content", "Compute or verify md5, sha1, sha256, and sha512 digests"},
		{"compress files", "Create zip, tar, or tar.gz archives from local files and folders"},
		{"convert timestamp", "Convert unix timestamps and human-readable dates"},
		{"generate ids", "Generate uuid, nanoid, and random string values"},
		{"palette extraction", "Extract a hex, swatch, or JSON palette from an image"},
	}
	for i, row := range rows {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return 1, err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return 1, err
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select task", "1"))
	if err != nil {
		return 1, err
	}
	switch strings.ToLower(strings.TrimSpace(selection)) {
	case "", "1", "convert", "convert image":
		if err := runConvertTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "2", "minify", "minify json":
		if err := runMinifyTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "3", "encode", "encode base64":
		if err := runEncodeTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "4", "hash", "hash content":
		if err := runHashTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "5", "compress", "compress files":
		if err := runCompressTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "6", "timestamp", "convert timestamp":
		if err := runTimestampTask(reader, w, dir, time.Now); err != nil {
			return 1, err
		}
		return 0, nil
	case "7", "uuid", "generate ids":
		if err := runUUIDTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	case "8", "palette", "palette extraction":
		if err := runPaletteTask(reader, w, dir); err != nil {
			return 1, err
		}
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown task selection %q", selection)
	}
}

func renderPaletteHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot palette", "Extract a terminal-friendly color palette from a local image.")
	writeUsageSection(&b, style, []string{
		"jot palette image.png",
		"jot palette image.png --count 8 --format json",
		"jot task palette",
	}, []string{
		"Default output is a compact list of hex colors.",
		"Palette extraction is local and deterministic.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--count N", description: "Number of colors to emit."},
		{name: "--format hex|swatch|json", description: "Choose plain hex, terminal swatches, or JSON output."},
		{name: "--ignore-alpha", description: "Drop translucent pixels while counting colors."},
		{name: "--sort dominant|hue|luma", description: "Choose how to order the selected colors."},
	})
	writeExamplesSection(&b, style, []string{
		"jot palette logo.png",
		"jot palette screenshot.png --count 6 --sort hue",
		"jot task palette",
	})
	return b.String()
}

func renderTaskPaletteHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot task", "Discover and run terminal-first tasks without leaving the current folder.")
	writeUsageSection(&b, style, []string{
		"jot task",
		"jot task palette",
	}, []string{
		"`jot task` opens the local task menu, including palette extraction.",
		"`jot task palette` jumps straight into the guided palette flow.",
	})
	writeExamplesSection(&b, style, []string{
		"jot task",
		"jot task palette",
		"jot palette logo.png --count 5",
	})
	return b.String()
}

func parsePaletteArgs(args []string) (paletteOptions, bool, error) {
	opts := paletteOptions{Count: 5, Format: "hex", SortMode: "dominant"}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) || arg == "help" {
			return opts, true, nil
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--count":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, false, fmt.Errorf("missing value after %q", arg)
				}
				value = args[i+1]
				i++
			}
			count, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || count <= 0 {
				return opts, false, fmt.Errorf("count must be a positive integer")
			}
			opts.Count = count
		case "--format":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, false, fmt.Errorf("missing value after %q", arg)
				}
				value = args[i+1]
				i++
			}
			opts.Format = normalizePaletteFormat(value)
		case "--ignore-alpha":
			opts.IgnoreAlpha = true
		case "--sort":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, false, fmt.Errorf("missing value after %q", arg)
				}
				value = args[i+1]
				i++
			}
			opts.SortMode = normalizePaletteSort(value)
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, false, fmt.Errorf("unsupported flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) == 0 {
		return opts, false, errors.New("image path must be provided")
	}
	if len(positional) > 1 {
		return opts, false, errors.New("provide exactly one image path")
	}
	opts.InputPath = positional[0]
	opts.Format = normalizePaletteFormat(opts.Format)
	opts.SortMode = normalizePaletteSort(opts.SortMode)
	if !isSupportedPaletteFormat(opts.Format) {
		return opts, false, fmt.Errorf("unsupported format %q; use `hex`, `swatch`, or `json`", opts.Format)
	}
	if !isSupportedPaletteSort(opts.SortMode) {
		return opts, false, fmt.Errorf("unsupported sort mode %q; use `dominant`, `hue`, or `luma`", opts.SortMode)
	}
	return opts, false, nil
}

func executePaletteCommand(w io.Writer, cwd string, opts paletteOptions) error {
	colors, err := generatePalette(opts, cwd)
	if err != nil {
		return err
	}
	return writePaletteOutput(w, colors, opts)
}

func generatePalette(opts paletteOptions, cwd string) ([]paletteColor, error) {
	inputPath := opts.InputPath
	if !filepath.IsAbs(inputPath) {
		inputPath = filepath.Join(cwd, inputPath)
	}
	img, err := loadPaletteImage(inputPath)
	if err != nil {
		return nil, err
	}
	return extractPalette(img, opts)
}

func writePaletteOutput(w io.Writer, colors []paletteColor, opts paletteOptions) error {
	switch opts.Format {
	case "hex":
		for _, c := range colors {
			if _, err := fmt.Fprintln(w, c.Hex); err != nil {
				return err
			}
		}
		return nil
	case "swatch":
		ui := newTermUI(w)
		for _, c := range colors {
			if _, err := fmt.Fprintln(w, renderPaletteSwatch(ui, c)); err != nil {
				return err
			}
		}
		return nil
	case "json":
		payload := paletteOutput{
			Colors:   colors,
			Format:   opts.Format,
			SortMode: opts.SortMode,
			Image:    filepath.Base(opts.InputPath),
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	default:
		return fmt.Errorf("unsupported format %q", opts.Format)
	}
}

func loadPaletteImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func extractPalette(img image.Image, opts paletteOptions) ([]paletteColor, error) {
	bounds := img.Bounds()
	if bounds.Empty() {
		return nil, errors.New("image is empty")
	}
	type bin struct {
		count int
		sumR  int
		sumG  int
		sumB  int
	}
	bins := make(map[int]*bin)
	opaquePixels := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			a := uint8(a16 >> 8)
			if a == 0 {
				continue
			}
			if opts.IgnoreAlpha && a < 255 {
				continue
			}
			r := uint8(r16 >> 8)
			g := uint8(g16 >> 8)
			b := uint8(b16 >> 8)
			key := quantizePaletteKey(r, g, b)
			entry := bins[key]
			if entry == nil {
				entry = &bin{}
				bins[key] = entry
			}
			entry.count++
			entry.sumR += int(r)
			entry.sumG += int(g)
			entry.sumB += int(b)
			opaquePixels++
		}
	}
	if opaquePixels == 0 {
		return nil, errors.New("image does not contain visible pixels")
	}

	colors := make([]paletteColor, 0, len(bins))
	for key, entry := range bins {
		r := uint8(entry.sumR / entry.count)
		g := uint8(entry.sumG / entry.count)
		b := uint8(entry.sumB / entry.count)
		hue, luma := colorMetrics(r, g, b)
		colors = append(colors, paletteColor{
			Hex:   fmt.Sprintf("#%02x%02x%02x", r, g, b),
			Count: entry.count,
			r:     r,
			g:     g,
			b:     b,
			hue:   hue,
			luma:  luma,
			key:   key,
		})
	}
	sort.SliceStable(colors, func(i, j int) bool {
		if colors[i].Count != colors[j].Count {
			return colors[i].Count > colors[j].Count
		}
		return colors[i].Hex < colors[j].Hex
	})
	if opts.Count < len(colors) {
		colors = append([]paletteColor(nil), colors[:opts.Count]...)
	}
	switch opts.SortMode {
	case "dominant":
		// already sorted
	case "hue":
		sort.SliceStable(colors, func(i, j int) bool {
			if colors[i].hue != colors[j].hue {
				return colors[i].hue < colors[j].hue
			}
			if colors[i].Count != colors[j].Count {
				return colors[i].Count > colors[j].Count
			}
			return colors[i].Hex < colors[j].Hex
		})
	case "luma":
		sort.SliceStable(colors, func(i, j int) bool {
			if colors[i].luma != colors[j].luma {
				return colors[i].luma < colors[j].luma
			}
			if colors[i].Count != colors[j].Count {
				return colors[i].Count > colors[j].Count
			}
			return colors[i].Hex < colors[j].Hex
		})
	}
	return colors, nil
}

func quantizePaletteKey(r, g, b uint8) int {
	return int(r>>3)<<10 | int(g>>3)<<5 | int(b>>3)
}

func colorMetrics(r, g, b uint8) (float64, float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	luma := 0.2126*rf + 0.7152*gf + 0.0722*bf
	hue := 0.0
	if max != min {
		delta := max - min
		switch max {
		case rf:
			hue = math.Mod((gf-bf)/delta, 6)
		case gf:
			hue = ((bf - rf) / delta) + 2
		default:
			hue = ((rf - gf) / delta) + 4
		}
		hue *= 60
		if hue < 0 {
			hue += 360
		}
	}
	return hue, luma
}

func renderPaletteSwatch(ui termUI, c paletteColor) string {
	if !ui.color {
		return fmt.Sprintf("%s  %s", strings.Repeat("#", 8), c.Hex)
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm    \x1b[0m  %s", c.r, c.g, c.b, ui.tbold(c.Hex))
}

func promptPaletteImagePath(reader *bufio.Reader, w io.Writer, ui termUI, dir string, images []string) (string, error) {
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

func renderPaletteTip(opts paletteOptions) string {
	var b strings.Builder
	b.WriteString("next time: jot palette ")
	b.WriteString(filepath.Base(opts.InputPath))
	if opts.Count != 5 {
		b.WriteString(fmt.Sprintf(" --count %d", opts.Count))
	}
	if opts.Format != "hex" {
		b.WriteString(" --format ")
		b.WriteString(opts.Format)
	}
	if opts.IgnoreAlpha {
		b.WriteString(" --ignore-alpha")
	}
	if opts.SortMode != "dominant" {
		b.WriteString(" --sort ")
		b.WriteString(opts.SortMode)
	}
	return b.String()
}

func normalizePaletteFormat(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizePaletteSort(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedPaletteFormat(value string) bool {
	switch normalizePaletteFormat(value) {
	case "hex", "swatch", "json":
		return true
	default:
		return false
	}
}

func isSupportedPaletteSort(value string) bool {
	switch normalizePaletteSort(value) {
	case "dominant", "hue", "luma":
		return true
	default:
		return false
	}
}

func isNoAnswer(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "n", "no", "false", "0":
		return true
	default:
		return false
	}
}
