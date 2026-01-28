package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const indexVersion = 1

var wordPattern = regexp.MustCompile(`[a-z0-9]+`)

type Index struct {
	Version     int          `json:"version"`
	JournalPath string       `json:"journal_path"`
	Entries     []IndexEntry `json:"entries"`
	Terms       map[string][]int
}

type IndexEntry struct {
	Line       int      `json:"line"`
	Text       string   `json:"text"`
	Hash       string   `json:"hash"`
	Terms      []string `json:"terms"`
	Normalized string   `json:"normalized"`
}

type UpdateStats struct {
	TotalLines     int
	ReindexedLines int
	ReusedLines    int
}

func defaultIndexPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jot", "index.json"), nil
}

func UpdateIndex(journalPath, indexPath string) (*Index, UpdateStats, error) {
	existing, err := loadIndex(indexPath)
	if err != nil {
		return nil, UpdateStats{}, err
	}

	file, err := os.Open(journalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Index{Version: indexVersion, JournalPath: journalPath}, UpdateStats{}, nil
		}
		return nil, UpdateStats{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	entries := make([]IndexEntry, 0)
	stats := UpdateStats{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		hash := hashLine(line)
		stats.TotalLines++

		if existing != nil && lineNo <= len(existing.Entries) {
			prev := existing.Entries[lineNo-1]
			if prev.Hash == hash {
				prev.Line = lineNo
				entries = append(entries, prev)
				stats.ReusedLines++
				continue
			}
		}

		normalized := strings.ToLower(line)
		terms := uniqueTerms(normalized)
		entries = append(entries, IndexEntry{
			Line:       lineNo,
			Text:       line,
			Hash:       hash,
			Terms:      terms,
			Normalized: normalized,
		})
		stats.ReindexedLines++
	}
	if err := scanner.Err(); err != nil {
		return nil, UpdateStats{}, err
	}

	index := &Index{
		Version:     indexVersion,
		JournalPath: journalPath,
		Entries:     entries,
	}
	index.Terms = buildTerms(entries)

	if err := writeIndex(indexPath, index); err != nil {
		return nil, UpdateStats{}, err
	}

	return index, stats, nil
}

func SearchIndex(index *Index, query string) ([]IndexEntry, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("empty query")
	}

	tokens, err := parseQuery(query)
	if err != nil {
		return nil, err
	}

	rpn, err := toRPN(tokens)
	if err != nil {
		return nil, err
	}

	results, err := evalRPN(rpn, index)
	if err != nil {
		return nil, err
	}

	entries := make([]IndexEntry, 0, len(results))
	for _, entry := range index.Entries {
		if _, ok := results[entry.Line-1]; ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func loadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.Version != indexVersion {
		return nil, nil
	}

	return &idx, nil
}

func writeIndex(path string, index *Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func hashLine(line string) string {
	sum := sha1.Sum([]byte(line))
	return hex.EncodeToString(sum[:])
}

func uniqueTerms(line string) []string {
	matches := wordPattern.FindAllString(line, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	terms := make([]string, 0, len(matches))
	for _, term := range matches {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

func buildTerms(entries []IndexEntry) map[string][]int {
	terms := make(map[string][]int)
	for i, entry := range entries {
		for _, term := range entry.Terms {
			terms[term] = append(terms[term], i)
		}
	}
	for term, ids := range terms {
		sort.Ints(ids)
		terms[term] = ids
	}
	return terms
}

func evalRPN(tokens []token, index *Index) (map[int]struct{}, error) {
	var stack []map[int]struct{}
	all := allEntrySet(index)

	for _, tok := range tokens {
		switch tok.kind {
		case tokenTerm:
			stack = append(stack, termSet(index, tok.value))
		case tokenPhrase:
			stack = append(stack, phraseSet(index, tok.value))
		case tokenNot:
			if len(stack) < 1 {
				return nil, fmt.Errorf("invalid query")
			}
			operand := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			stack = append(stack, difference(all, operand))
		case tokenAnd, tokenOr:
			if len(stack) < 2 {
				return nil, fmt.Errorf("invalid query")
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			if tok.kind == tokenAnd {
				stack = append(stack, intersection(left, right))
			} else {
				stack = append(stack, union(left, right))
			}
		default:
			return nil, fmt.Errorf("invalid query")
		}
	}

	if len(stack) != 1 {
		return nil, fmt.Errorf("invalid query")
	}

	return stack[0], nil
}

func allEntrySet(index *Index) map[int]struct{} {
	all := make(map[int]struct{}, len(index.Entries))
	for i := range index.Entries {
		all[i] = struct{}{}
	}
	return all
}

func termSet(index *Index, term string) map[int]struct{} {
	ids := index.Terms[term]
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func phraseSet(index *Index, phrase string) map[int]struct{} {
	set := make(map[int]struct{})
	for i, entry := range index.Entries {
		if strings.Contains(entry.Normalized, phrase) {
			set[i] = struct{}{}
		}
	}
	return set
}

func union(a, b map[int]struct{}) map[int]struct{} {
	out := make(map[int]struct{}, len(a)+len(b))
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}

func intersection(a, b map[int]struct{}) map[int]struct{} {
	if len(a) > len(b) {
		a, b = b, a
	}
	out := make(map[int]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			out[k] = struct{}{}
		}
	}
	return out
}

func difference(all, remove map[int]struct{}) map[int]struct{} {
	out := make(map[int]struct{}, len(all))
	for k := range all {
		if _, ok := remove[k]; !ok {
			out[k] = struct{}{}
		}
	}
	return out
}
