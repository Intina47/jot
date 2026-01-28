package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestUpdateIndexReusesUnchangedLines(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	content := "[2024-01-01 10:00] first line\n[2024-01-01 11:00] second line\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	indexPath, err := defaultIndexPath()
	if err != nil {
		t.Fatalf("defaultIndexPath failed: %v", err)
	}

	index, stats, err := UpdateIndex(journalPath, indexPath)
	if err != nil {
		t.Fatalf("UpdateIndex failed: %v", err)
	}
	if stats.ReindexedLines != 2 || stats.ReusedLines != 0 {
		t.Fatalf("expected 2 reindexed lines, got %+v", stats)
	}
	if len(index.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(index.Entries))
	}

	updated := "[2024-01-01 10:00] first line\n[2024-01-01 11:00] changed line\n"
	if err := os.WriteFile(journalPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, stats, err = UpdateIndex(journalPath, indexPath)
	if err != nil {
		t.Fatalf("UpdateIndex failed: %v", err)
	}
	if stats.ReusedLines != 1 || stats.ReindexedLines != 1 {
		t.Fatalf("expected 1 reused and 1 reindexed line, got %+v", stats)
	}
}

func TestSearchIndexSupportsBooleanAndPhrase(t *testing.T) {
	home := withTempHome(t)
	journalDir, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	content := strings.Join([]string{
		"[2024-01-01 10:00] quick brown fox",
		"[2024-01-01 11:00] lazy dog jumps",
		"[2024-01-01 12:00] quick blue hare",
		"",
	}, "\n")
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	indexPath, err := defaultIndexPath()
	if err != nil {
		t.Fatalf("defaultIndexPath failed: %v", err)
	}

	index, _, err := UpdateIndex(journalPath, indexPath)
	if err != nil {
		t.Fatalf("UpdateIndex failed: %v", err)
	}

	results, err := SearchIndex(index, "\"quick brown\" OR hare")
	if err != nil {
		t.Fatalf("SearchIndex failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	results, err = SearchIndex(index, "quick AND NOT blue")
	if err != nil {
		t.Fatalf("SearchIndex failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func BenchmarkIndexing1k(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("USERPROFILE", home)
	journalDir, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		b.Fatalf("mkdir failed: %v", err)
	}
	lines := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("[2024-01-01 10:%02d] note %d", i%60, i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		b.Fatalf("write failed: %v", err)
	}

	indexPath, err := defaultIndexPath()
	if err != nil {
		b.Fatalf("defaultIndexPath failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := UpdateIndex(journalPath, indexPath); err != nil {
			b.Fatalf("UpdateIndex failed: %v", err)
		}
	}
}

func BenchmarkSearch1k(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("USERPROFILE", home)
	journalDir, journalPath := journalPaths(home)
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		b.Fatalf("mkdir failed: %v", err)
	}
	lines := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("[2024-01-01 10:%02d] note %d", i%60, i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(journalPath, []byte(content), 0o600); err != nil {
		b.Fatalf("write failed: %v", err)
	}

	indexPath, err := defaultIndexPath()
	if err != nil {
		b.Fatalf("defaultIndexPath failed: %v", err)
	}

	index, _, err := UpdateIndex(journalPath, indexPath)
	if err != nil {
		b.Fatalf("UpdateIndex failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := SearchIndex(index, "note AND 9"); err != nil {
			b.Fatalf("SearchIndex failed: %v", err)
		}
	}
}
