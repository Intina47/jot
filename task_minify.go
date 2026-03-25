package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type minifyInputKind int

const (
	minifyInputFile minifyInputKind = iota + 1
	minifyInputText
	minifyInputStdin
)

type minifyOptions struct {
	SourcePath   string
	SourceSet    bool
	InlineText   string
	TextSet      bool
	UseStdin     bool
	Pretty       bool
	Indent       int
	OutputPath   string
	Stdout       bool
	Overwrite    bool
	ExplicitMode bool
}

type minifyResult struct {
	OutputPath string
	WroteFile  bool
}

func jotMinify(w io.Writer, args []string) error {
	return jotMinifyWithInput(os.Stdin, w, args)
}

func jotMinifyWithInput(stdin io.Reader, w io.Writer, args []string) error {
	options, err := parseMinifyArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return err
	}
	_, err = executeMinify(stdin, w, options)
	return err
}

func parseMinifyArgs(args []string) (minifyOptions, error) {
	options := minifyOptions{Indent: 2}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			return options, flag.ErrHelp
		}
		if strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "-") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--pretty":
				options.Pretty = true
				options.ExplicitMode = true
			case "--stdin":
				options.UseStdin = true
			case "--stdout":
				options.Stdout = true
			case "--overwrite":
				options.Overwrite = true
			case "--text":
				if hasValue {
					options.InlineText = value
					options.TextSet = true
					options.ExplicitMode = true
					continue
				}
				if i+1 >= len(args) {
					return options, fmt.Errorf("missing value for %s", name)
				}
				i++
				options.InlineText = args[i]
				options.TextSet = true
				options.ExplicitMode = true
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
			case "--indent":
				if hasValue {
					value = strings.TrimSpace(value)
				} else {
					if i+1 >= len(args) {
						return options, fmt.Errorf("missing value for %s", name)
					}
					i++
					value = args[i]
				}
				indent, parseErr := strconv.Atoi(strings.TrimSpace(value))
				if parseErr != nil || indent <= 0 {
					return options, fmt.Errorf("indent must be a positive integer")
				}
				options.Indent = indent
			default:
				return options, fmt.Errorf("unknown flag: %s", arg)
			}
			continue
		}
		positional = append(positional, arg)
	}

	if len(positional) > 1 {
		return options, fmt.Errorf("usage: jot minify <file> [--pretty]")
	}
	if len(positional) == 1 {
		options.SourcePath = strings.TrimSpace(positional[0])
		options.SourceSet = true
	}
	if options.SourceSet {
		options.ExplicitMode = true
	}

	inputKinds := 0
	if options.SourceSet {
		inputKinds++
	}
	if options.TextSet {
		inputKinds++
	}
	if options.UseStdin {
		inputKinds++
	}
	if inputKinds == 0 {
		return options, errors.New("choose one input source: <file>, --text, or --stdin")
	}
	if inputKinds > 1 {
		return options, errors.New("choose one input source: <file>, --text, or --stdin")
	}
	if options.Stdout && options.OutputPath != "" {
		return options, errors.New("--stdout and --out cannot be used together")
	}
	if options.Stdout && !options.SourceSet && !options.UseStdin && !options.TextSet {
		return options, errors.New("choose one input source: <file>, --text, or --stdin")
	}
	return options, nil
}

func executeMinify(stdin io.Reader, w io.Writer, options minifyOptions) (minifyResult, error) {
	input, err := readMinifyInput(stdin, options)
	if err != nil {
		return minifyResult{}, err
	}

	output, err := minifyJSON(input, options.Pretty, options.Indent)
	if err != nil {
		return minifyResult{}, err
	}

	dest, err := resolveMinifyOutputPath(options)
	if err != nil {
		return minifyResult{}, err
	}

	if dest == "" || options.Stdout {
		_, err = w.Write(output)
		return minifyResult{}, err
	}

	if err := writeAtomicFile(dest, output, 0o644, options.Overwrite); err != nil {
		return minifyResult{}, err
	}

	ui := newTermUI(w)
	line := filepath.Base(dest)
	if info, statErr := os.Stat(dest); statErr == nil {
		line = fmt.Sprintf("%s  %s", line, ui.tdim(fmt.Sprintf("%.1f KB", float64(info.Size())/1024.0)))
	}
	if _, err := fmt.Fprintln(w, ui.success(line)); err != nil {
		return minifyResult{}, err
	}
	return minifyResult{OutputPath: dest, WroteFile: true}, nil
}

func readMinifyInput(stdin io.Reader, options minifyOptions) ([]byte, error) {
	switch {
	case options.SourceSet:
		path := options.SourcePath
		if !filepath.IsAbs(path) {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			path = absPath
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a directory, expected a JSON file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []byte(stripUTF8BOM(string(data))), nil
	case options.TextSet:
		return []byte(stripUTF8BOM(options.InlineText)), nil
	case options.UseStdin:
		if stdin == nil {
			return nil, errors.New("stdin is unavailable")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, errors.New("stdin is empty")
		}
		return []byte(stripUTF8BOM(string(data))), nil
	default:
		return nil, errors.New("choose one input source: <file>, --text, or --stdin")
	}
}

func minifyJSON(input []byte, pretty bool, indentWidth int) ([]byte, error) {
	var out bytes.Buffer
	clean := []byte(stripUTF8BOM(string(input)))
	if pretty {
		if indentWidth <= 0 {
			indentWidth = 2
		}
		if err := json.Indent(&out, clean, "", strings.Repeat(" ", indentWidth)); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	if err := json.Compact(&out, clean); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func resolveMinifyOutputPath(options minifyOptions) (string, error) {
	if options.Stdout {
		return "", nil
	}

	if strings.TrimSpace(options.OutputPath) != "" {
		outputPath := strings.TrimSpace(options.OutputPath)
		if !filepath.IsAbs(outputPath) {
			absPath, err := filepath.Abs(outputPath)
			if err != nil {
				return "", err
			}
			outputPath = absPath
		}
		if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
			base := defaultMinifyOutputName(options.SourcePath, options.Pretty)
			return filepath.Join(outputPath, base), nil
		}
		return outputPath, nil
	}

	if options.SourcePath == "" {
		return "", nil
	}

	path := options.SourcePath
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = absPath
	}
	return filepath.Join(filepath.Dir(path), defaultMinifyOutputName(path, options.Pretty)), nil
}

func defaultMinifyOutputName(sourcePath string, pretty bool) string {
	base := filepath.Base(sourcePath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if pretty {
		return stem + ".pretty.json"
	}
	return stem + ".min.json"
}

func writeAtomicFile(path string, data []byte, perm os.FileMode, overwrite bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; rerun with --overwrite or choose --out", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if overwrite {
		_ = os.Remove(path)
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func renderMinifyHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot minify", "Minify or pretty-print JSON from a local file, inline text, or stdin.")
	writeUsageSection(&b, style, []string{
		"jot minify <file>",
		"jot minify <file> --pretty",
		"jot minify --text '{\"name\":\"jot\"}'",
		"jot minify --stdin --pretty",
	}, []string{
		"File input writes a sibling file by default.",
		"Text and stdin input write to stdout unless --out is provided.",
		"Use `jot task minify` when you want a guided terminal flow.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--pretty", description: "Pretty-print JSON instead of minifying it."},
		{name: "--indent N", description: "Set the indent width used for pretty-printing."},
		{name: "--text VALUE", description: "Read JSON from an inline string."},
		{name: "--stdin", description: "Read JSON from stdin."},
		{name: "--out PATH, -o PATH", description: "Write the result to a specific file."},
		{name: "--stdout", description: "Force stdout output for file input."},
		{name: "--overwrite", description: "Allow replacing an existing output file."},
	})
	writeExamplesSection(&b, style, []string{
		"jot minify data.json",
		"jot minify data.json --pretty",
		"jot minify --text '{\"name\":\"jot\"}'",
		"jot minify --stdin",
		"jot task minify",
	})
	return b.String()
}

func runMinifyTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("Minify JSON")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("input source")); err != nil {
		return err
	}
	rows := []struct {
		key  string
		name string
		desc string
	}{
		{key: "file", name: "file", desc: "Choose a JSON file from this folder"},
		{key: "text", name: "text", desc: "Paste inline JSON"},
		{key: "stdin", name: "stdin", desc: "Read from stdin"},
	}
	for i, row := range rows {
		if _, err := fmt.Fprintln(w, ui.listItem(i+1, row.name, row.desc, "")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	sourceSelection, err := promptLine(reader, w, ui.styledPrompt("Select source", "1"))
	if err != nil {
		return err
	}
	sourceKind := minifyInputFile
	switch strings.ToLower(strings.TrimSpace(sourceSelection)) {
	case "", "1", "file":
		sourceKind = minifyInputFile
	case "2", "text":
		sourceKind = minifyInputText
	case "3", "stdin":
		sourceKind = minifyInputStdin
	default:
		return fmt.Errorf("unknown source selection %q", sourceSelection)
	}

	options := minifyOptions{Indent: 2}
	var sourceHint string
	switch sourceKind {
	case minifyInputFile:
		files, err := listMinifiableJSONFiles(dir)
		if err != nil {
			return err
		}
		sourcePath, err := promptTaskJSONPath(reader, w, ui, dir, files)
		if err != nil {
			return err
		}
		options.SourcePath = sourcePath
		options.SourceSet = true
		sourceHint = filepath.Base(sourcePath)
	case minifyInputText:
		if _, err := fmt.Fprint(w, ui.sectionLabel("inline json")); err != nil {
			return err
		}
		text, err := promptLine(reader, w, ui.styledPrompt("Paste JSON", "enter a single line"))
		if err != nil {
			return err
		}
		if text == "" {
			return errors.New("inline JSON must be provided")
		}
		options.InlineText = text
		options.TextSet = true
		sourceHint = "inline JSON"
	case minifyInputStdin:
		options.UseStdin = true
		sourceHint = "stdin"
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("mode")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "minify", "Remove insignificant whitespace", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "pretty-print", "Format JSON with stable indentation", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	modeSelection, err := promptLine(reader, w, ui.styledPrompt("Select mode", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(modeSelection)) {
	case "", "1", "minify":
		options.Pretty = false
	case "2", "pretty", "pretty-print", "prettyprint":
		options.Pretty = true
	default:
		return fmt.Errorf("unknown mode %q", modeSelection)
	}

	if options.Pretty {
		if _, err := fmt.Fprint(w, ui.sectionLabel("indentation")); err != nil {
			return err
		}
		indentSelection, err := promptLine(reader, w, ui.styledPrompt("Indent spaces", "2"))
		if err != nil {
			return err
		}
		if indentSelection != "" {
			indent, parseErr := strconv.Atoi(strings.TrimSpace(indentSelection))
			if parseErr != nil || indent <= 0 {
				return fmt.Errorf("indent must be a positive integer")
			}
			options.Indent = indent
		}
	}

	if options.SourcePath != "" {
		options.OutputPath = filepath.Join(filepath.Dir(options.SourcePath), defaultMinifyOutputName(options.SourcePath, options.Pretty))
	}
	if options.SourcePath == "" {
		options.Stdout = true
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	result, err := executeMinify(stdin, w, options)
	if err != nil {
		return err
	}

	tip := minifyTaskTip(sourceHint, options.Pretty, options.SourcePath != "")
	if tip != "" {
		if _, err := fmt.Fprintln(w, ui.tip(tip)); err != nil {
			return err
		}
	}
	if result.WroteFile {
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return err
		}
	}
	return nil
}

func promptTaskJSONPath(reader *bufio.Reader, w io.Writer, ui termUI, dir string, files []string) (string, error) {
	label := "JSON file"
	hint := ""
	if len(files) == 1 {
		hint = "1"
	}
	if len(files) > 0 {
		if _, err := fmt.Fprint(w, ui.sectionLabel("json files in this folder")); err != nil {
			return "", err
		}
		for i, file := range files {
			meta := ""
			if info, statErr := os.Stat(file); statErr == nil {
				meta = humanFileSize(info.Size())
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(file), "", meta)); err != nil {
				return "", err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return "", err
		}
	}

	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		if len(files) == 1 {
			return files[0], nil
		}
		return "", errors.New("JSON file must be provided")
	}
	if idx, err := strconv.Atoi(selection); err == nil {
		if idx < 1 || idx > len(files) {
			return "", fmt.Errorf("JSON selection must be between 1 and %d", len(files))
		}
		return files[idx-1], nil
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return selection, nil
}

func listMinifiableJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func humanFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("< 1 KB")
	}
	return fmt.Sprintf("%.0f KB", float64(size)/1024.0)
}

func minifyTaskTip(sourceHint string, pretty bool, fileInput bool) string {
	mode := ""
	if pretty {
		mode = " --pretty"
	}
	switch {
	case sourceHint == "inline JSON":
		return "next time: jot minify --text <json>" + mode
	case sourceHint == "stdin":
		return "next time: jot minify --stdin" + mode
	case fileInput:
		return "next time: jot minify " + sourceHint + mode
	default:
		return ""
	}
}
