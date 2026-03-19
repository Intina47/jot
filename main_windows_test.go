//go:build windows

package main

import "testing"

func TestJournalPathsWindows(t *testing.T) {
	home := `C:\Users\jot`
	dir, txtPath, jsonlPath := journalPaths(home)

	if dir != `C:\Users\jot\.jot` {
		t.Fatalf("expected dir %q, got %q", `C:\Users\jot\.jot`, dir)
	}
	if txtPath != `C:\Users\jot\.jot\journal.txt` {
		t.Fatalf("expected path %q, got %q", `C:\Users\jot\.jot\journal.txt`, txtPath)
	}
	if jsonlPath != `C:\Users\jot\.jot\journal.jsonl` {
		t.Fatalf("expected path %q, got %q", `C:\Users\jot\.jot\journal.jsonl`, jsonlPath)
	}
}
