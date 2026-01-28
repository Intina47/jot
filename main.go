package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
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

	if len(args) == 2 && args[0] == "related" {
		id, err := strconv.Atoi(args[1])
		if err != nil || id <= 0 {
			fmt.Fprintln(os.Stderr, "note-id must be a positive integer")
			os.Exit(1)
		}
		if err := jotRelated(os.Stdout, id); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: jot [init|list|patterns|related <note-id>]")
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
	ID   int
	Text string
	Raw  string
}

func jotRelated(w io.Writer, id int) error {
	notes, err := loadNotes()
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return errors.New("no notes found")
	}
	if id < 1 || id > len(notes) {
		return fmt.Errorf("note-id must be between 1 and %d", len(notes))
	}

	vectors := tfidfVectors(notes)
	base := vectors[id-1]
	baseNorm := vectorNorm(base)
	if baseNorm == 0 {
		return errors.New("note has no searchable content")
	}

	type scoredNote struct {
		Note  note
		Score float64
	}

	var scored []scoredNote
	for i, n := range notes {
		if n.ID == id {
			continue
		}
		score := cosineSimilarity(base, baseNorm, vectors[i])
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredNote{Note: n, Score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Note.ID < scored[j].Note.ID
		}
		return scored[i].Score > scored[j].Score
	})

	limit := 5
	if len(scored) < limit {
		limit = len(scored)
	}
	for i := 0; i < limit; i++ {
		if _, err := fmt.Fprintf(w, "%d\t%s\n", scored[i].Note.ID, scored[i].Note.Raw); err != nil {
			return err
		}
	}

	return nil
}

func loadNotes() ([]note, error) {
	journalPath, err := ensureJournal()
	if err != nil {
		return nil, err
	}

	file, err := os.Open(journalPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var notes []note
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		text := extractNoteText(line)
		notes = append(notes, note{ID: len(notes) + 1, Text: text, Raw: line})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return notes, nil
}

func extractNoteText(line string) string {
	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 0 {
			return strings.TrimSpace(line[end+1:])
		}
	}
	return strings.TrimSpace(line)
}

func tfidfVectors(notes []note) []map[string]float64 {
	termCounts := make([]map[string]int, len(notes))
	docFreq := make(map[string]int)

	for i, n := range notes {
		tokens := tokenize(n.Text)
		counts := make(map[string]int)
		for _, token := range tokens {
			counts[token]++
		}
		termCounts[i] = counts
		for term := range counts {
			docFreq[term]++
		}
	}

	N := float64(len(notes))
	vectors := make([]map[string]float64, len(notes))
	for i, counts := range termCounts {
		total := 0
		for _, count := range counts {
			total += count
		}
		vector := make(map[string]float64)
		if total == 0 {
			vectors[i] = vector
			continue
		}
		for term, count := range counts {
			tf := float64(count) / float64(total)
			idf := math.Log((1+N)/(1+float64(docFreq[term]))) + 1
			vector[term] = tf * idf
		}
		vectors[i] = vector
	}

	return vectors
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func vectorNorm(vector map[string]float64) float64 {
	var sum float64
	for _, v := range vector {
		sum += v * v
	}
	return math.Sqrt(sum)
}

func cosineSimilarity(base map[string]float64, baseNorm float64, other map[string]float64) float64 {
	otherNorm := vectorNorm(other)
	if baseNorm == 0 || otherNorm == 0 {
		return 0
	}
	var dot float64
	for term, weight := range base {
		if otherWeight, ok := other[term]; ok {
			dot += weight * otherWeight
		}
	}
	return dot / (baseNorm * otherNorm)
}
