package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

	if len(args) >= 1 && args[0] == "new" {
		if err := jotNew(os.Stdout, time.Now, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(args) >= 1 && args[0] == "list" {
		if len(args) == 2 && (args[1] == "templates" || args[1] == "--templates" || args[1] == "-t") {
			if err := jotTemplates(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: jot list [templates]")
			os.Exit(1)
		}
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

	if len(args) == 1 && args[0] == "templates" {
		if err := jotTemplates(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: jot [init|list|new|patterns|templates]")
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

func jotNew(w io.Writer, now func() time.Time, args []string) error {
	set := flag.NewFlagSet("new", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var templateName string
	var noteName string
	set.StringVar(&templateName, "template", "daily", "template to use")
	set.StringVar(&noteName, "name", "", "note name")
	set.StringVar(&noteName, "n", "", "note name")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", set.Args())
	}

	templates, err := loadTemplates()
	if err != nil {
		return err
	}
	content, ok := templates[templateName]
	if !ok {
		return fmt.Errorf("template %q not found", templateName)
	}

	currentTime := now()
	repo := repoName()
	rendered := renderTemplate(content, currentTime, repo)
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}

	filename := templateName
	if noteName != "" {
		slug := slugifyName(noteName)
		if slug == "" {
			return fmt.Errorf("note name must contain letters or numbers")
		}
		filename = fmt.Sprintf("%s-%s", templateName, slug)
	}
	filename = fmt.Sprintf("%s-%s.md", currentTime.Format("2006-01-02"), filename)
	path := filepath.Join(mustGetwd(), filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("note already exists: %s", path)
		}
		return err
	}
	if _, err := io.WriteString(file, rendered); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, path)
	return err
}

func jotTemplates(w io.Writer) error {
	templates, err := loadTemplates()
	if err != nil {
		return err
	}

	var names []string
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, err := fmt.Fprintln(w, name); err != nil {
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

func templateDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err == nil && configDir != "" {
		return filepath.Join(configDir, "jot", "templates"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jot", "templates"), nil
}

func loadTemplates() (map[string]string, error) {
	templates := builtinTemplates()
	custom, err := loadCustomTemplates()
	if err != nil {
		return nil, err
	}
	for name, content := range custom {
		templates[name] = content
	}
	return templates, nil
}

func builtinTemplates() map[string]string {
	return map[string]string{
		"daily": strings.Join([]string{
			"# Daily Log — {{date}}",
			"",
			"## Focus",
			"- ",
			"",
			"## Notes",
			"- ",
			"",
			"## Closing",
			"- What moved?",
		}, "\n"),
		"meeting": strings.Join([]string{
			"# Meeting — {{date}} {{time}}",
			"",
			"## Attendees",
			"- ",
			"",
			"## Agenda",
			"- ",
			"",
			"## Notes",
			"- ",
			"",
			"## Next Steps",
			"- ",
		}, "\n"),
		"rfc": strings.Join([]string{
			"# RFC — {{repo}} — {{date}}",
			"",
			"## Problem",
			"- ",
			"",
			"## Proposal",
			"- ",
			"",
			"## Alternatives",
			"- ",
			"",
			"## Risks",
			"- ",
		}, "\n"),
	}
}

func loadCustomTemplates() (map[string]string, error) {
	dir, err := templateDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	custom := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.TrimSpace(name) == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		custom[name] = string(data)
	}
	return custom, nil
}

func renderTemplate(content string, now time.Time, repo string) string {
	replacements := strings.NewReplacer(
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04"),
		"{{datetime}}", now.Format("2006-01-02 15:04"),
		"{{repo}}", repo,
	)
	return replacements.Replace(content)
}

func slugifyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune(' ')
		}
	}
	parts := strings.Fields(builder.String())
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.Join(parts, "-"))
}

func repoName() string {
	wd := mustGetwd()
	for {
		if info, err := os.Stat(filepath.Join(wd, ".git")); err == nil && info != nil {
			return filepath.Base(wd)
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return ""
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
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
