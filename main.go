package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
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

	if len(args) == 2 && args[0] == "link" {
		if err := jotLink(args[1], time.Now); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: jot [init|list|patterns|link <url>]")
	os.Exit(1)
}

type LinkMetadata struct {
	NoteTimestamp string `json:"note_timestamp"`
	URL           string `json:"url"`
	Host          string `json:"host"`
	Owner         string `json:"owner,omitempty"`
	Repo          string `json:"repo,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Number        int    `json:"number,omitempty"`
	AddedAt       string `json:"added_at"`
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

func jotLink(rawURL string, now func() time.Time) error {
	info, err := parseLinkURL(rawURL)
	if err != nil {
		return err
	}

	journalPath, err := ensureJournal()
	if err != nil {
		return err
	}

	lastTimestamp, err := latestNoteTimestamp(journalPath)
	if err != nil {
		return err
	}

	info.NoteTimestamp = lastTimestamp
	info.AddedAt = now().UTC().Format(time.RFC3339)

	linksFile, err := ensureLinksFile()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(linksFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	_, err = file.Write(append(data, '\n'))
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

	linksByTimestamp, err := loadLinksByTimestamp()
	if err != nil {
		return err
	}

	lastIdx := len(lines) - 1
	for lastIdx >= 0 && strings.TrimSpace(lines[lastIdx]) == "" {
		lastIdx--
	}

	prevDate := ""
	sep := "\x1b[90m" + "----------------" + "\x1b[0m"
	for i, line := range lines {
		timestamp, hasTimestamp := parseTimestamp(line)
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

		if hasTimestamp {
			if links, ok := linksByTimestamp[timestamp]; ok {
				for _, link := range links {
					line := fmt.Sprintf("    ↳ %s", link.URL)
					line = "\x1b[90m" + line + "\x1b[0m"
					if _, err := fmt.Fprintln(w, line); err != nil {
						return err
					}
				}
			}
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

func linksPath(home string) string {
	return filepath.Join(home, ".jot", "links.jsonl")
}

func ensureLinksFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	journalDir, _ := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return "", err
	}

	path := linksPath(home)
	file, err := os.OpenFile(path, os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	return path, nil
}

func parseLinkURL(raw string) (LinkMetadata, error) {
	if strings.TrimSpace(raw) == "" {
		return LinkMetadata{}, errors.New("link url is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return LinkMetadata{}, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return LinkMetadata{}, fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}

	info := LinkMetadata{
		URL:  raw,
		Host: parsed.Hostname(),
	}

	if strings.EqualFold(info.Host, "github.com") {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) >= 4 {
			owner, repo, kind, numberStr := segments[0], segments[1], segments[2], segments[3]
			if kind == "pull" || kind == "issues" {
				number, err := strconv.Atoi(numberStr)
				if err == nil {
					info.Owner = owner
					info.Repo = repo
					if kind == "issues" {
						info.Kind = "issue"
					} else {
						info.Kind = "pull"
					}
					info.Number = number
				}
			}
		}
	}

	return info, nil
}

func latestNoteTimestamp(journalPath string) (string, error) {
	file, err := os.Open(journalPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lastTimestamp := ""
	for scanner.Scan() {
		line := scanner.Text()
		if ts, ok := parseTimestamp(line); ok {
			lastTimestamp = ts
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastTimestamp == "" {
		return "", errors.New("no notes available to link")
	}

	return lastTimestamp, nil
}

func parseTimestamp(line string) (string, bool) {
	if !strings.HasPrefix(line, "[") {
		return "", false
	}
	end := strings.IndexByte(line, ']')
	if end <= 1 {
		return "", false
	}
	return line[1:end], true
}

func loadLinksByTimestamp() (map[string][]LinkMetadata, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := linksPath(home)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]LinkMetadata{}, nil
		}
		return nil, err
	}
	defer file.Close()

	links := make(map[string][]LinkMetadata)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry LinkMetadata
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		if entry.NoteTimestamp == "" {
			continue
		}
		links[entry.NoteTimestamp] = append(links[entry.NoteTimestamp], entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return links, nil
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
