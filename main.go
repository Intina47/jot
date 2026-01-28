package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const version = "1.5.1"

func main() {
	_ = version

	args := os.Args[1:]
	if len(args) == 0 || (len(args) == 1 && args[0] == "init") {
		if err := jotInit(os.Stdin, os.Stdout, time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) == 1 && args[0] == "list" {
		if err := jotList(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) == 1 && args[0] == "patterns" {
		fmt.Fprintln(os.Stdout, "patterns are coming. keep noticing.")
		return
	}

	if len(args) >= 1 && args[0] == "capture" {
		if err := jotCapture(os.Stdout, args[1:], time.Now, launchEditor); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: jot [init|list|patterns|capture]")
	os.Exit(1)
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

	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	stamp := now().Format("2006-01-02 15:04")
	_, err = fmt.Fprintf(file, "[%s] %s\n", stamp, entry)
	return err
}

func jotList(w io.Writer) error {
	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	file, err := os.Open(journalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if !isTTY(w) {
		_, err = io.Copy(w, file)
		return err
	}

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
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

func ensureJournal() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	journalDir, journalPath := journalPaths(home)

	// Create the directory and file lazily so jot stays zero-config.
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return "", err
	}

	file, err := os.OpenFile(journalPath, os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	return journalPath, nil
}

func journalPaths(home string) (string, string) {
	journalDir := filepath.Join(home, ".jot")
	journalPath := filepath.Join(journalDir, "journal.txt")
	return journalDir, journalPath
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

const captureUsage = `usage: jot capture [content] [--title TITLE] [--tag TAG] [--project PROJECT] [--repo REPO]

Capture a note quickly. If no content is provided, jot opens your editor.
`

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
			_, helpErr := fmt.Fprint(w, captureUsage)
			return helpErr
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

	entry := formatCaptureEntry(options)
	if strings.TrimSpace(entry) == "" {
		return nil
	}

	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	stamp := now().Format("2006-01-02 15:04")
	_, err = fmt.Fprintf(file, "[%s] %s\n", stamp, entry)
	return err
}

func formatCaptureEntry(options captureOptions) string {
	content := strings.TrimSpace(options.Content)
	var builder strings.Builder

	if options.Title != "" {
		builder.WriteString(options.Title)
		if content != "" {
			builder.WriteString(" — ")
		}
	}
	if content != "" {
		builder.WriteString(content)
	}

	metadata := []string{}
	if len(options.Tags) > 0 {
		metadata = append(metadata, "tags: "+strings.Join(options.Tags, ", "))
	}
	if options.Project != "" {
		metadata = append(metadata, "project: "+options.Project)
	}
	if options.Repo != "" {
		metadata = append(metadata, "repo: "+options.Repo)
	}
	if len(metadata) > 0 {
		builder.WriteString(" (")
		builder.WriteString(strings.Join(metadata, "; "))
		builder.WriteString(")")
	}

	return builder.String()
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
