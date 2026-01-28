package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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

	if len(args) > 0 && args[0] == "search" {
		if err := runJotSearch(args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			printSearchUsage(os.Stderr)
			os.Exit(1)
		}
		return
	}

	printUsage(os.Stderr)
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

	reader := bufio.NewReader(file)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		lines = append(lines, strings.TrimRight(line, "\r\n"))
		if errors.Is(err, io.EOF) {
			break
		}
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

type multiString []string

func (m *multiString) String() string {
	return strings.Join(*m, ",")
}

func (m *multiString) Set(value string) error {
	if value == "" {
		return errors.New("tag cannot be empty")
	}
	*m = append(*m, value)
	return nil
}

type searchOptions struct {
	tags    []string
	since   *time.Time
	until   *time.Time
	project string
}

func runJotSearch(args []string, w io.Writer) error {
	var tags multiString
	var sinceInput string
	var untilInput string
	var project string

	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(&tags, "tag", "filter by tag (repeatable)")
	flags.StringVar(&sinceInput, "since", "", "filter entries on or after YYYY-MM-DD")
	flags.StringVar(&untilInput, "until", "", "filter entries on or before YYYY-MM-DD")
	flags.StringVar(&project, "project", "", "filter entries by project/repo context")

	if err := flags.Parse(args); err != nil {
		return err
	}

	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return errors.New("search query required")
	}

	var opts searchOptions
	opts.tags = tags
	opts.project = strings.TrimSpace(project)

	if sinceInput != "" {
		since, err := parseDateInput(sinceInput)
		if err != nil {
			return fmt.Errorf("invalid --since date: %w", err)
		}
		opts.since = &since
	}

	if untilInput != "" {
		until, err := parseDateInput(untilInput)
		if err != nil {
			return fmt.Errorf("invalid --until date: %w", err)
		}
		opts.until = &until
	}

	return jotSearch(w, query, opts)
}

func parseDateInput(input string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", input)
	if err != nil {
		return time.Time{}, err
	}
	return dateOnly(parsed), nil
}

type journalEntry struct {
	timestamp time.Time
	text      string
}

func jotSearch(w io.Writer, query string, opts searchOptions) error {
	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	file, err := os.Open(journalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	queryLower := strings.ToLower(query)
	normalizedTags := normalizeSlice(opts.tags)
	projectLower := strings.ToLower(strings.TrimSpace(opts.project))

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, ok := parseEntry(line)
		if !ok {
			continue
		}
		if queryLower != "" && !strings.Contains(strings.ToLower(entry.text), queryLower) {
			continue
		}
		if len(normalizedTags) > 0 && !entryHasTags(entry.text, normalizedTags) {
			continue
		}
		if projectLower != "" && !entryHasProject(entry.text, projectLower) {
			continue
		}
		if opts.since != nil || opts.until != nil {
			entryDate := dateOnly(entry.timestamp)
			if opts.since != nil && entryDate.Before(*opts.since) {
				continue
			}
			if opts.until != nil && entryDate.After(*opts.until) {
				continue
			}
		}

		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func normalizeSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, strings.ToLower(trimmed))
	}
	return normalized
}

var tagRegex = regexp.MustCompile(`#([A-Za-z0-9_-]+)`)
var projectRegex = regexp.MustCompile(`(?i)\b(?:project|repo):([A-Za-z0-9_-]+)\b`)

func entryHasTags(text string, tags []string) bool {
	tagSet := map[string]struct{}{}
	for _, match := range tagRegex.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			tagSet[strings.ToLower(match[1])] = struct{}{}
		}
	}
	for _, tag := range tags {
		if _, ok := tagSet[tag]; !ok {
			return false
		}
	}
	return true
}

func entryHasProject(text string, project string) bool {
	for _, match := range projectRegex.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 && strings.EqualFold(match[1], project) {
			return true
		}
	}
	return false
}

func parseEntry(line string) (journalEntry, bool) {
	if !strings.HasPrefix(line, "[") {
		return journalEntry{}, false
	}
	end := strings.IndexByte(line, ']')
	if end == -1 {
		return journalEntry{}, false
	}
	timestampText := line[1:end]
	timestamp, err := time.Parse("2006-01-02 15:04", timestampText)
	if err != nil {
		return journalEntry{}, false
	}
	text := strings.TrimSpace(line[end+1:])
	return journalEntry{timestamp: timestamp, text: text}, true
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
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

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: jot [init|list|patterns|search]")
}

func printSearchUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: jot search <query> [--tag <tag>] [--since YYYY-MM-DD] [--until YYYY-MM-DD] [--project <name>]")
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
