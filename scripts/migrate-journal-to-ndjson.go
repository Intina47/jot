package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Entry struct {
	ID        string  `json:"id"`
	Text      string  `json:"text"`
	CreatedAt *string `json:"created_at,omitempty"`
	Source    *Source `json:"source,omitempty"`
}

type Source struct {
	Journal string `json:"journal"`
	Line    int    `json:"line"`
}

func main() {
	inPath := flag.String("in", "", "path to journal.txt")
	outPath := flag.String("out", "", "path to journal.ndjson")
	force := flag.Bool("force", false, "overwrite output file if it exists")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		exitErr(err)
	}

	defaultDir := filepath.Join(home, ".jot")
	input := filepath.Join(defaultDir, "journal.txt")
	output := filepath.Join(defaultDir, "journal.ndjson")
	if *inPath != "" {
		input = *inPath
	}
	if *outPath != "" {
		output = *outPath
	}

	if !*force {
		if _, err := os.Stat(output); err == nil {
			exitErr(fmt.Errorf("output file exists: %s (use -force to overwrite)", output))
		}
	}

	file, err := os.Open(input)
	if err != nil {
		exitErr(err)
	}
	defer file.Close()

	outFile, err := os.Create(output)
	if err != nil {
		exitErr(err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	pattern := regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\]\s*(.*)$`)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var createdAt *string
		text := line
		if matches := pattern.FindStringSubmatch(line); len(matches) == 3 {
			if parsed, err := time.ParseInLocation("2006-01-02 15:04", matches[1], time.Local); err == nil {
				stamp := parsed.Format(time.RFC3339)
				createdAt = &stamp
				text = matches[2]
			}
		}

		entry := Entry{
			ID:   newID(),
			Text: text,
			Source: &Source{
				Journal: filepath.Base(input),
				Line:    lineNum,
			},
			CreatedAt: createdAt,
		}

		payload, err := json.Marshal(entry)
		if err != nil {
			exitErr(err)
		}
		if _, err := writer.Write(append(payload, '\n')); err != nil {
			exitErr(err)
		}
	}

	if err := scanner.Err(); err != nil {
		exitErr(err)
	}
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
