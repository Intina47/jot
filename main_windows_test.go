//go:build windows

package main

import "testing"

func TestJournalPathsWindows(t *testing.T) {
	home := `C:\Users\jot`
	dir, path := journalPaths(home)

	if dir != `C:\Users\jot\.jot` {
		t.Fatalf("expected dir %q, got %q", `C:\Users\jot\.jot`, dir)
	}
	if path != `C:\Users\jot\.jot\journal.txt` {
		t.Fatalf("expected path %q, got %q", `C:\Users\jot\.jot\journal.txt`, path)
	}
}
