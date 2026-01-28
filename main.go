package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

	if len(args) >= 1 && (args[0] == "switch" || args[0] == "open") {
		if err := jotSwitch(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: jot [init|list|patterns|switch|open]")
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

type note struct {
	Line      int
	Timestamp string
	Text      string
	Tags      []string
}

type noteMatch struct {
	Note  note
	Score int
}

func jotSwitch(r io.Reader, w io.Writer) error {
	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	notes, err := loadNotes(journalPath)
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		fmt.Fprintln(w, "no notes yet")
		return nil
	}

	reader := bufio.NewReader(r)
	if _, err := fmt.Fprint(w, "search: "); err != nil {
		return err
	}
	query, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	query = strings.TrimSpace(query)

	matches := searchNotes(notes, query)
	if len(matches) == 0 {
		fmt.Fprintln(w, "no matches")
		return nil
	}

	for i, match := range matches {
		if _, err := fmt.Fprintf(w, "%d) %s\n", i+1, formatNote(match.Note)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "open: "); err != nil {
		return err
	}
	selection, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	selection = strings.TrimSpace(selection)
	if selection == "" || selection == "q" {
		return nil
	}

	idx, err := strconv.Atoi(selection)
	if err != nil {
		return fmt.Errorf("invalid selection: %s", selection)
	}
	if idx < 1 || idx > len(matches) {
		return fmt.Errorf("selection out of range: %d", idx)
	}

	editor := resolveEditor()
	if editor == "" {
		return errors.New("no editor configured")
	}

	return openInEditor(editor, journalPath, matches[idx-1].Note.Line)
}

func loadNotes(journalPath string) ([]note, error) {
	file, err := os.Open(journalPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var notes []note
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parsed, ok := parseNote(line, lineNumber)
		if ok {
			notes = append(notes, parsed)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func parseNote(line string, lineNumber int) (note, bool) {
	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 0 {
			timestamp := line[1:end]
			text := strings.TrimSpace(line[end+1:])
			return note{
				Line:      lineNumber,
				Timestamp: timestamp,
				Text:      text,
				Tags:      extractTags(text),
			}, true
		}
	}

	return note{}, false
}

func extractTags(text string) []string {
	fields := strings.Fields(text)
	var tags []string
	for _, field := range fields {
		if strings.HasPrefix(field, "#") && len(field) > 1 {
			tag := strings.TrimRight(field, ",.!?;:")
			tags = append(tags, strings.TrimPrefix(tag, "#"))
		}
	}
	return tags
}

func searchNotes(notes []note, query string) []noteMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		matches := make([]noteMatch, 0, len(notes))
		for _, note := range notes {
			matches = append(matches, noteMatch{Note: note, Score: 0})
		}
		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].Note.Line > matches[j].Note.Line
		})
		return matches
	}

	tokens := strings.Fields(strings.ToLower(query))
	var matches []noteMatch
	for _, note := range notes {
		score, ok := matchScore(note, tokens)
		if ok {
			matches = append(matches, noteMatch{Note: note, Score: score})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].Note.Line > matches[j].Note.Line
		}
		return matches[i].Score > matches[j].Score
	})
	return matches
}

func matchScore(note note, tokens []string) (int, bool) {
	text := strings.ToLower(note.Text)
	tagMatches := make([]string, len(note.Tags))
	for i, tag := range note.Tags {
		tagMatches[i] = strings.ToLower(tag)
	}

	score := 0
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "#") {
			token = strings.TrimPrefix(token, "#")
		}

		best := 0
		if token != "" {
			if tokenScore, ok := fuzzyScore(token, text); ok {
				best = tokenScore
			}
			for _, tag := range tagMatches {
				if tokenScore, ok := fuzzyScore(token, tag); ok && tokenScore > best {
					best = tokenScore + 50
				}
			}
		}

		if best == 0 {
			return 0, false
		}
		score += best
	}
	return score, true
}

func fuzzyScore(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}
	if target == "" {
		return 0, false
	}

	if idx := strings.Index(target, query); idx >= 0 {
		return 1000 - idx, true
	}

	q := []rune(query)
	t := []rune(target)
	pos := 0
	consecutive := 0
	score := 0
	for _, qc := range q {
		found := false
		for pos < len(t) {
			if t[pos] == qc {
				found = true
				break
			}
			pos++
			consecutive = 0
		}
		if !found {
			return 0, false
		}
		score += 10
		if consecutive > 0 {
			score += 5
		}
		if pos == 0 || t[pos-1] == ' ' || t[pos-1] == '#' {
			score += 3
		}
		consecutive++
		pos++
	}
	return score, true
}

func formatNote(note note) string {
	if note.Timestamp == "" {
		return note.Text
	}
	return fmt.Sprintf("[%s] %s", note.Timestamp, note.Text)
}

func resolveEditor() string {
	if editor := strings.TrimSpace(os.Getenv("JOT_EDITOR")); editor != "" {
		return editor
	}
	if editor := strings.TrimSpace(os.Getenv("EDITOR")); editor != "" {
		return editor
	}
	if editor := strings.TrimSpace(os.Getenv("VISUAL")); editor != "" {
		return editor
	}
	return "vi"
}

func openInEditor(editor string, path string, line int) error {
	cmd, err := editorCommand(editor, path, line)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func editorCommand(editor string, path string, line int) (*exec.Cmd, error) {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return nil, errors.New("editor command is empty")
	}
	cmdName := fields[0]
	args := append([]string{}, fields[1:]...)
	if line > 0 && supportsLineArg(cmdName) {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, path)
	return exec.Command(cmdName, args...), nil
}

func supportsLineArg(editor string) bool {
	base := filepath.Base(editor)
	return base == "vim" || base == "nvim" || base == "vi"
}
