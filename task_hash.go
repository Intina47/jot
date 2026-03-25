package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type hashOptions struct {
	inputPath string
	text      string
	stdin     bool
	algo      string
	algoSet   bool
	all       bool
	verify    string
	out       string
	overwrite bool
	quiet     bool
}

type hashDigest struct {
	algo  string
	value string
}

type parsedHashOptions struct {
	hashOptions
	help bool
}

func jotHash(w io.Writer, args []string) error {
	return jotHashWithInput(os.Stdin, w, args)
}

func jotHashWithInput(stdin io.Reader, w io.Writer, args []string) error {
	options, err := parseHashArgs(args)
	if err != nil {
		return err
	}
	if options.help {
		_, err := io.WriteString(w, renderHashHelp(false))
		return err
	}
	return executeHashCommand(stdin, w, options)
}

func runHashTask(stdin io.Reader, w io.Writer, dir string) error {
	ui := newTermUI(w)
	reader := bufio.NewReader(stdin)

	if _, err := fmt.Fprint(w, ui.header("Hash")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("mode")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "compute", "Generate one or more digests", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "verify", "Compare a digest against the input", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	modeSel, err := promptLine(reader, w, ui.styledPrompt("Select mode", "1"))
	if err != nil {
		return err
	}
	mode := "compute"
	if strings.EqualFold(modeSel, "2") || strings.EqualFold(modeSel, "verify") {
		mode = "verify"
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("input")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "file", "Hash a local file from this folder", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "text", "Hash pasted inline text", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "stdin", "Hash piped input", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	inputSel, err := promptLine(reader, w, ui.styledPrompt("Select input", "1"))
	if err != nil {
		return err
	}

	options := hashOptions{algo: "sha256"}
	switch strings.ToLower(inputSel) {
	case "", "1", "file":
		options.inputPath, err = promptHashFilePath(reader, w, ui, dir)
		if err != nil {
			return err
		}
	case "2", "text":
		if _, err := fmt.Fprint(w, ui.sectionLabel("text")); err != nil {
			return err
		}
		options.text, err = promptLine(reader, w, ui.styledPrompt("Enter text", "value"))
		if err != nil {
			return err
		}
		if options.text == "" {
			return errors.New("text value must be provided")
		}
	case "3", "stdin":
		options.stdin = true
	default:
		return fmt.Errorf("unknown input selection %q", inputSel)
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("algorithm")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "sha256", "Default developer hash", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "sha1", "Legacy compatibility", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "md5", "Fast checksum", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(4, "sha512", "Longer digest", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	algoSel, err := promptLine(reader, w, ui.styledPrompt("Select algorithm", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(algoSel) {
	case "", "1", "sha256":
		options.algo = "sha256"
	case "2", "sha1":
		options.algo = "sha1"
	case "3", "md5":
		options.algo = "md5"
	case "4", "sha512":
		options.algo = "sha512"
	default:
		return fmt.Errorf("unknown algorithm %q", algoSel)
	}

	if mode == "verify" {
		if _, err := fmt.Fprint(w, ui.sectionLabel("verification")); err != nil {
			return err
		}
		options.verify, err = promptLine(reader, w, ui.styledPrompt("Expected digest", "ALGO: HEX"))
		if err != nil {
			return err
		}
		if options.verify == "" {
			return errors.New("expected digest must be provided")
		}
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	if err := executeHashCommand(reader, w, parsedHashOptions{hashOptions: options}); err != nil {
		return err
	}

	if tip := hashTip(options); tip != "" {
		if _, err := fmt.Fprintln(w, ui.tip(tip)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func renderHashHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot hash", "Compute or verify common digests from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot hash <file>",
		"jot hash --text <value>",
		"jot hash --stdin",
		"jot hash <file> --verify \"SHA256: ...\"",
	}, []string{
		"Default algorithm: `sha256`.",
		"`jot hash` writes sibling digest files for file input and prints digest lines for text or stdin input.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--algo NAME", description: "Choose `md5`, `sha1`, `sha256`, or `sha512`."},
		{name: "--all", description: "Compute every supported algorithm in a stable order."},
		{name: "--verify DIGEST", description: "Compare the computed digest against `ALGO: HEX` or a bare hex digest."},
		{name: "--text VALUE", description: "Hash inline text instead of a file."},
		{name: "--stdin", description: "Read bytes from stdin."},
		{name: "--out PATH", description: "Write digest output to a specific path."},
		{name: "--overwrite", description: "Replace an existing digest file."},
		{name: "--quiet", description: "Suppress success summaries."},
	})
	writeExamplesSection(&b, style, []string{
		"jot hash package.zip",
		"jot hash package.zip --algo sha1",
		`jot hash --text "hello world" --algo sha256`,
		"jot hash --stdin --algo sha512",
		`jot hash package.zip --verify "SHA256: 7c3f..."`,
	})
	return b.String()
}

func renderTaskHashHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot task", "Discover and run terminal-first tasks without leaving the current folder.")
	writeUsageSection(&b, style, []string{
		"jot task",
		"jot task convert",
		"jot task hash",
	}, []string{
		"`jot task` is the guided front door for jot's task layer.",
		"The current task menu includes image conversion and hashing, and the surface is meant to grow from there.",
		"After a task runs, jot prints the equivalent direct command so the terminal shortcut becomes the habit.",
	})
	writeExamplesSection(&b, style, []string{
		"jot task",
		"jot task convert",
		"jot task hash",
		"jot hash package.zip",
	})
	return b.String()
}

func parseHashArgs(args []string) (parsedHashOptions, error) {
	var options parsedHashOptions
	options.algo = "sha256"

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help", "help":
			options.help = true
		case "--algo":
			value, next, err := takeHashValue(args, i)
			if err != nil {
				return options, err
			}
			options.algo = canonicalHashAlgo(value)
			options.algoSet = true
			i = next
		case "--text":
			value, next, err := takeHashValue(args, i)
			if err != nil {
				return options, err
			}
			options.text = value
			i = next
		case "--stdin":
			options.stdin = true
		case "--verify":
			value, next, err := takeHashValue(args, i)
			if err != nil {
				return options, err
			}
			options.verify = value
			i = next
		case "--out":
			value, next, err := takeHashValue(args, i)
			if err != nil {
				return options, err
			}
			options.out = value
			i = next
		case "--overwrite":
			options.overwrite = true
		case "--quiet":
			options.quiet = true
		case "--all":
			options.all = true
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unsupported flag %q", arg)
			}
			if options.inputPath != "" {
				return options, errors.New("choose one input source: <path>, --text, or --stdin")
			}
			options.inputPath = arg
		}
	}

	if options.inputPath != "" && (options.text != "" || options.stdin) {
		return options, errors.New("choose one input source: <path>, --text, or --stdin")
	}
	if options.text != "" && options.stdin {
		return options, errors.New("choose one input source: <path>, --text, or --stdin")
	}
	if options.all && options.verify != "" {
		return options, errors.New("--all cannot be combined with --verify")
	}
	if options.all && options.algoSet {
		return options, errors.New("--all cannot be combined with --algo")
	}
	if options.verify != "" && options.out != "" {
		return options, errors.New("--out cannot be combined with --verify")
	}
	if options.inputPath == "" && options.text == "" && !options.stdin {
		return options, errors.New("input file path, --text, or --stdin must be provided")
	}

	if options.algo = canonicalHashAlgo(options.algo); !isSupportedHashAlgo(options.algo) {
		return options, fmt.Errorf("unsupported algorithm %q", options.algo)
	}
	return options, nil
}

func takeHashValue(args []string, index int) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("missing value after %q", args[index])
	}
	return args[index+1], index + 1, nil
}

func canonicalHashAlgo(algo string) string {
	return strings.ToLower(strings.TrimSpace(algo))
}

func isSupportedHashAlgo(algo string) bool {
	switch canonicalHashAlgo(algo) {
	case "md5", "sha1", "sha256", "sha512":
		return true
	default:
		return false
	}
}

func executeHashCommand(stdin io.Reader, w io.Writer, options parsedHashOptions) error {
	sourceBytes, sourceLabel, err := readHashSource(stdin, options.hashOptions)
	if err != nil {
		return err
	}

	verifyAlgo, verifyDigest, hasVerify, err := parseVerifyDigest(options.verify, options.algo, options.algoSet)
	if err != nil {
		return err
	}
	if hasVerify {
		options.algo = verifyAlgo
	}

	if options.all {
		if hasVerify {
			return errors.New("--verify cannot be combined with --all")
		}
		digests, err := hashAllDigests(sourceBytes)
		if err != nil {
			return err
		}
		return emitHashDigests(w, sourceLabel, digests, options)
	}

	digest, err := hashDigestForAlgo(options.algo, bytes.NewReader(sourceBytes))
	if err != nil {
		return err
	}

	if hasVerify {
		actual := strings.ToLower(digest)
		if actual != verifyDigest {
			return fmt.Errorf("verification failed: expected %s %s, got %s", strings.ToUpper(options.algo), verifyDigest, actual)
		}
		if !options.quiet {
			_, err = fmt.Fprintf(w, "verified %s %s\n", strings.ToUpper(options.algo), sourceLabel)
		}
		return err
	}

	return emitHashDigests(w, sourceLabel, []hashDigest{{algo: options.algo, value: digest}}, options)
}

func promptHashFilePath(reader *bufio.Reader, w io.Writer, ui termUI, dir string) (string, error) {
	label := "File path"
	hint := ""
	if dir != "" {
		label = "Hash file"
		hint = "1"
	}
	selection, err := promptLine(reader, w, ui.styledPrompt(label, hint))
	if err != nil {
		return "", err
	}
	if selection == "" {
		return "", errors.New("file path must be provided")
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(dir, selection)
	}
	return selection, nil
}

func hashTip(options hashOptions) string {
	if options.verify != "" {
		if options.inputPath != "" {
			return fmt.Sprintf("next time: jot hash %s --verify %q", filepath.Base(options.inputPath), options.verify)
		}
		if options.text != "" {
			return fmt.Sprintf("next time: jot hash --text %q --verify %q", options.text, options.verify)
		}
		if options.stdin {
			return fmt.Sprintf("next time: jot hash --stdin --verify %q", options.verify)
		}
		return "next time: jot hash --verify \"ALGO: HEX\""
	}
	if options.text != "" {
		return fmt.Sprintf("next time: jot hash --text %q --algo %s", options.text, options.algo)
	}
	if options.stdin {
		return fmt.Sprintf("next time: jot hash --stdin --algo %s", options.algo)
	}
	if options.inputPath != "" {
		return fmt.Sprintf("next time: jot hash %s --algo %s", filepath.Base(options.inputPath), options.algo)
	}
	return ""
}

func readHashSource(stdin io.Reader, options hashOptions) ([]byte, string, error) {
	switch {
	case options.inputPath != "":
		data, err := os.ReadFile(options.inputPath)
		if err != nil {
			return nil, "", err
		}
		return data, filepath.Base(options.inputPath), nil
	case options.text != "":
		return []byte(options.text), "text", nil
	case options.stdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", err
		}
		if len(data) == 0 {
			return nil, "", errors.New("stdin input is empty")
		}
		return data, "stdin", nil
	default:
		return nil, "", errors.New("input file path, --text, or --stdin must be provided")
	}
}

func parseVerifyDigest(raw, defaultAlgo string, explicitAlgo bool) (string, string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false, nil
	}

	algo := canonicalHashAlgo(defaultAlgo)
	digest := raw
	if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 {
		prefix := canonicalHashAlgo(parts[0])
		if explicitAlgo && prefix != "" && prefix != canonicalHashAlgo(defaultAlgo) {
			return "", "", false, fmt.Errorf("verify digest algorithm %q does not match --algo %q", prefix, defaultAlgo)
		}
		algo = prefix
		digest = parts[1]
	}
	digest = strings.ToLower(strings.TrimSpace(digest))
	if algo == "" {
		algo = canonicalHashAlgo(defaultAlgo)
	}
	if !isSupportedHashAlgo(algo) {
		return "", "", false, fmt.Errorf("unsupported algorithm %q", algo)
	}
	if digest == "" {
		return "", "", false, errors.New("verify digest must be provided")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", "", false, fmt.Errorf("invalid verify digest %q", raw)
	}
	return algo, digest, true, nil
}

func hashAllDigests(content []byte) ([]hashDigest, error) {
	algos := []string{"md5", "sha1", "sha256", "sha512"}
	digests := make([]hashDigest, 0, len(algos))
	for _, algo := range algos {
		sum, err := hashDigestForAlgo(algo, bytes.NewReader(content))
		if err != nil {
			return nil, err
		}
		digests = append(digests, hashDigest{algo: algo, value: sum})
	}
	return digests, nil
}

func hashDigestForAlgo(algo string, r io.Reader) (string, error) {
	h, err := newHashForAlgo(algo)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newHashForAlgo(algo string) (hash.Hash, error) {
	switch canonicalHashAlgo(algo) {
	case "md5":
		return md5.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "sha256":
		return sha256.New(), nil
	case "sha512":
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algo)
	}
}

func emitHashDigests(w io.Writer, sourceLabel string, digests []hashDigest, options parsedHashOptions) error {
	lines := make([]string, 0, len(digests))
	for _, digest := range digests {
		lines = append(lines, fmt.Sprintf("%s  %s  %s", strings.ToUpper(digest.algo), sourceLabel, digest.value))
	}

	if options.out != "" || options.inputPath != "" {
		outputPath, err := resolveHashOutputPath(options, len(digests) > 1)
		if err != nil {
			return err
		}
		if err := writeHashOutputFile(outputPath, strings.Join(lines, "\n")+"\n", options.overwrite); err != nil {
			return err
		}
		if !options.quiet {
			_, err := fmt.Fprintf(w, "wrote %s\n", outputPath)
			return err
		}
		return nil
	}

	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func resolveHashOutputPath(options parsedHashOptions, multiple bool) (string, error) {
	if options.out != "" {
		if options.inputPath != "" && sameHashPath(options.out, options.inputPath) {
			return "", errors.New("output path must not match the input file path")
		}
		return options.out, nil
	}
	if options.inputPath == "" {
		return "", errors.New("output path must be provided for file writes")
	}
	if multiple {
		return options.inputPath + ".all.hash.txt", nil
	}
	return options.inputPath + "." + options.algo + ".hash.txt", nil
}

func sameHashPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func writeHashOutputFile(path, content string, overwrite bool) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("output path is a directory: %s", path)
		}
		if !overwrite {
			return fmt.Errorf("output already exists: %s", path)
		}
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
