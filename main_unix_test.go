//go:build !windows

package main

import "testing"

func TestJournalPathsUnix(t *testing.T) {
	home := "/home/jot"
	dir, path := journalPaths(home)

	if dir != "/home/jot/.jot" {
		t.Fatalf("expected dir %q, got %q", "/home/jot/.jot", dir)
	}
	if path != "/home/jot/.jot/journal.txt" {
		t.Fatalf("expected path %q, got %q", "/home/jot/.jot/journal.txt", path)
	}
}
