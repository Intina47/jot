//go:build !windows

package main

import "testing"

func TestJournalPathsUnix(t *testing.T) {
	home := "/home/jot"
	dir, txtPath, jsonlPath := journalPaths(home)

	if dir != "/home/jot/.jot" {
		t.Fatalf("expected dir %q, got %q", "/home/jot/.jot", dir)
	}
	if txtPath != "/home/jot/.jot/journal.txt" {
		t.Fatalf("expected path %q, got %q", "/home/jot/.jot/journal.txt", txtPath)
	}
	if jsonlPath != "/home/jot/.jot/journal.jsonl" {
		t.Fatalf("expected path %q, got %q", "/home/jot/.jot/journal.jsonl", jsonlPath)
	}
}
