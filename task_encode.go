package main

import (
	"bufio"
	"encoding/base64"
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

type encodeSourceKind int

const (
	encodeSourceNone encodeSourceKind = iota
	encodeSourceFile
	encodeSourceText
	encodeSourceStdin
)

type encodeRequest struct {
	decode     bool
	sourceKind encodeSourceKind
	sourcePath string
	text       string
	outPath    string
	stdout     bool
	overwrite  bool
	quiet      bool
	forceText  bool
}

// jotEncode is the direct command implementation. It is intentionally
// self-contained so main.go can wire it in later without moving logic around.
func jotEncode(w io.Writer, args []string) error {
	return encodeCLI(os.Stdin, w, args, os.Getwd)
}

func encodeCLI(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	req, helpRequested, err := parseEncodeArgs(args)
	if err != nil {
		return err
	}
	if helpRequested {
		_, writeErr := io.WriteString(w, renderEncodeHelp(isTTY(w)))
		return writeErr
	}
	cwd, err := getwd()
	if err != nil {
		return err
	}
	return executeEncodeRequest(stdin, w, cwd, req)
}

func renderEncodeHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot encode", "Base64 encode and decode local content from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot encode <path>",
		"jot encode --text \"hello world\"",
		"jot encode --stdin",
		"jot encode <path> --decode",
	}, []string{
		"`jot encode` defaults to encode mode.",
		"File input writes a sibling output file by default.",
		"`jot task encode` is the guided path that will reuse the same command logic when wired in.",
	})
	writeCommandSection(&b, style, []helpCommand{
		{name: "--decode", description: "Decode base64 instead of encoding bytes."},
		{name: "--text VALUE", description: "Use inline text instead of a file."},
		{name: "--stdin", description: "Read bytes from stdin."},
		{name: "--out PATH", description: "Write to a specific output file."},
		{name: "--stdout", description: "Write the transformed bytes to stdout."},
		{name: "--overwrite", description: "Replace an existing output file."},
		{name: "--quiet", description: "Suppress the success summary for file output."},
		{name: "--force-text", description: "Allow decoded bytes to be written to stdout even when they are not UTF-8."},
	})
	writeExamplesSection(&b, style, []string{
		`jot encode logo.png`,
		`jot encode logo.png --decode`,
		`jot encode --text "hello world"`,
		`jot encode --stdin --decode --stdout`,
	})
	return b.String()
}

func parseEncodeArgs(args []string) (encodeRequest, bool, error) {
	var req encodeRequest
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			return encodeRequest{}, true, nil
		case arg == "--decode":
			req.decode = true
		case arg == "--stdout":
			req.stdout = true
		case arg == "--overwrite":
			req.overwrite = true
		case arg == "--quiet":
			req.quiet = true
		case arg == "--force-text":
			req.forceText = true
		case arg == "--stdin":
			if req.sourceKind != encodeSourceNone {
				return encodeRequest{}, false, errors.New("choose one input source: <path>, --text, or --stdin")
			}
			req.sourceKind = encodeSourceStdin
		case arg == "--text":
			if req.sourceKind != encodeSourceNone {
				return encodeRequest{}, false, errors.New("choose one input source: <path>, --text, or --stdin")
			}
			i++
			if i >= len(args) {
				return encodeRequest{}, false, errors.New("--text requires a value")
			}
			req.sourceKind = encodeSourceText
			req.text = args[i]
		case strings.HasPrefix(arg, "--text="):
			if req.sourceKind != encodeSourceNone {
				return encodeRequest{}, false, errors.New("choose one input source: <path>, --text, or --stdin")
			}
			req.sourceKind = encodeSourceText
			req.text = strings.TrimPrefix(arg, "--text=")
		case arg == "--out":
			i++
			if i >= len(args) {
				return encodeRequest{}, false, errors.New("--out requires a path")
			}
			req.outPath = args[i]
		case strings.HasPrefix(arg, "--out="):
			req.outPath = strings.TrimPrefix(arg, "--out=")
		case strings.HasPrefix(arg, "-"):
			return encodeRequest{}, false, fmt.Errorf("unknown flag %q", arg)
		default:
			if req.sourceKind != encodeSourceNone {
				return encodeRequest{}, false, errors.New("choose one input source: <path>, --text, or --stdin")
			}
			req.sourceKind = encodeSourceFile
			req.sourcePath = arg
		}
	}

	if req.sourceKind == encodeSourceNone {
		return encodeRequest{}, false, errors.New("choose one input source: <path>, --text, or --stdin")
	}
	if req.stdout && req.outPath != "" {
		return encodeRequest{}, false, errors.New("--stdout and --out cannot be used together")
	}
	return req, false, nil
}

func executeEncodeRequest(stdin io.Reader, w io.Writer, cwd string, req encodeRequest) error {
	input, sourceAbs, sourceLabel, err := readEncodeInput(stdin, cwd, req)
	if err != nil {
		return err
	}

	outputPath, writeToStdout, err := resolveEncodeOutputPath(cwd, req, sourceAbs)
	if err != nil {
		return err
	}

	var result []byte
	if req.decode {
		result, err = decodeBase64Payload(input)
		if err != nil {
			return err
		}
	} else {
		result = []byte(base64.StdEncoding.EncodeToString(input))
	}

	if writeToStdout {
		if req.decode && !req.forceText && !utf8.Valid(result) {
			return errors.New("decoded output is binary; use --out <path> or --force-text")
		}
		if req.decode && req.forceText {
			_, err = w.Write(result)
			return err
		}
		_, err = fmt.Fprintln(w, string(result))
		return err
	}

	if err := writeEncodeFile(outputPath, result, req.overwrite); err != nil {
		return err
	}
	if req.quiet {
		return nil
	}

	ui := newTermUI(w)
	action := "encoded"
	if req.decode {
		action = "decoded"
	}
	if _, err := fmt.Fprintln(w, ui.success(fmt.Sprintf("%s %s -> %s", action, sourceLabel, outputPath))); err != nil {
		return err
	}
	return nil
}

func readEncodeInput(stdin io.Reader, cwd string, req encodeRequest) ([]byte, string, string, error) {
	switch req.sourceKind {
	case encodeSourceFile:
		sourceAbs := resolvePath(cwd, req.sourcePath)
		info, err := os.Stat(sourceAbs)
		if err != nil {
			return nil, "", "", err
		}
		if info.IsDir() {
			return nil, "", "", fmt.Errorf("%s is a directory, expected a file", req.sourcePath)
		}
		data, err := os.ReadFile(sourceAbs)
		if err != nil {
			return nil, "", "", err
		}
		return data, sourceAbs, req.sourcePath, nil
	case encodeSourceText:
		return []byte(req.text), "", "--text", nil
	case encodeSourceStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", "", err
		}
		return data, "", "--stdin", nil
	default:
		return nil, "", "", errors.New("choose one input source: <path>, --text, or --stdin")
	}
}

func resolveEncodeOutputPath(cwd string, req encodeRequest, sourceAbs string) (string, bool, error) {
	if req.stdout {
		return "", true, nil
	}

	if req.outPath != "" {
		outputAbs := resolvePath(cwd, req.outPath)
		return outputAbs, false, nil
	}

	if req.sourceKind != encodeSourceFile {
		return "", true, nil
	}

	base := filepath.Base(sourceAbs)
	if req.decode {
		base = decodeOutputName(base)
	} else {
		base = base + ".b64.txt"
	}
	return filepath.Join(filepath.Dir(sourceAbs), base), false, nil
}

func decodeOutputName(base string) string {
	switch {
	case strings.HasSuffix(base, ".b64.txt"):
		return strings.TrimSuffix(base, ".b64.txt")
	case strings.HasSuffix(base, ".b64"):
		return strings.TrimSuffix(base, ".b64")
	default:
		return base + ".decoded"
	}
}

func writeEncodeFile(path string, data []byte, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; rerun with --overwrite or choose --out", path)
		}
	}

	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".jot-encode-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if overwrite {
		_ = os.Remove(path)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

func decodeBase64Payload(data []byte) ([]byte, error) {
	cleaned := strings.Join(strings.Fields(string(data)), "")
	if cleaned == "" {
		return []byte{}, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, errors.New("input is not valid base64")
	}
	return decoded, nil
}

func runEncodeTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("jot encode")); err != nil {
		return err
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("mode")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "encode", "Base64 encode bytes", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "decode", "Base64 decode bytes", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	modeSelection, err := promptLine(reader, w, ui.styledPrompt("Select mode", "1"))
	if err != nil {
		return err
	}
	req := encodeRequest{}
	switch strings.ToLower(modeSelection) {
	case "", "1", "encode":
	case "2", "decode":
		req.decode = true
	default:
		return fmt.Errorf("unknown mode %q", modeSelection)
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("input")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "file", "Select a local file in this folder", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "text", "Paste inline text", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	sourceSelection, err := promptLine(reader, w, ui.styledPrompt("Select input", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(sourceSelection) {
	case "", "1", "file":
		req.sourceKind = encodeSourceFile
		req.sourcePath, err = promptEncodeFilePath(reader, w, ui, dir)
		if err != nil {
			return err
		}
	case "2", "text":
		req.sourceKind = encodeSourceText
		if _, err := fmt.Fprint(w, ui.sectionLabel("text")); err != nil {
			return err
		}
		text, err := promptLine(reader, w, ui.styledPrompt("Enter text", "paste"))
		if err != nil {
			return err
		}
		req.text = text
	default:
		return fmt.Errorf("unknown input selection %q", sourceSelection)
	}

	if err := executeEncodeRequest(stdin, w, dir, req); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	tip := "next time: jot encode"
	switch req.sourceKind {
	case encodeSourceFile:
		tip = fmt.Sprintf("next time: jot encode %q", req.sourcePath)
		if req.decode {
			tip += " --decode"
		}
	case encodeSourceText:
		tip = fmt.Sprintf("next time: jot encode --text %q", req.text)
		if req.decode {
			tip += " --decode"
		}
	}
	if _, err := fmt.Fprintln(w, ui.tip(tip)); err != nil {
		return err
	}
	return nil
}

func promptEncodeFilePath(reader *bufio.Reader, w io.Writer, ui termUI, dir string) (string, error) {
	files, err := listEncodeFiles(dir)
	if err != nil {
		return "", err
	}
	if len(files) > 0 {
		if _, err := fmt.Fprint(w, ui.sectionLabel("files in this folder")); err != nil {
			return "", err
		}
		for i, file := range files {
			meta := ""
			if info, statErr := os.Stat(file); statErr == nil {
				if info.Size() < 1024 {
					meta = "< 1 KB"
				} else {
					meta = fmt.Sprintf("%d KB", int(info.Size()/1024))
				}
			}
			if _, err := fmt.Fprintln(w, ui.listItem(i+1, filepath.Base(file), "", meta)); err != nil {
				return "", err
			}
		}
		if _, err := fmt.Fprintln(w, ""); err != nil {
			return "", err
		}
	}

	hint := "path"
	if len(files) > 0 {
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt("Select file", hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		if len(files) == 1 {
			return files[0], nil
		}
		if len(files) == 0 {
			return "", errors.New("file path must be provided")
		}
		return "", errors.New("select a file by number or enter a path")
	}
	if idx, convErr := strconv.Atoi(selection); convErr == nil {
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

func listEncodeFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func resolvePath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}
