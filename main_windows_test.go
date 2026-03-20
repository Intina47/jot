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

func TestIsGoTestBinary(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "empty", args: nil, want: false},
		{name: "regular exe", args: []string{`C:\Tools\jot.exe`}, want: false},
		{name: "go test exe", args: []string{`C:\Temp\jot.test.exe`}, want: true},
		{name: "go test binary without exe suffix", args: []string{`/tmp/jot.test`}, want: true},
	}

	for _, tt := range tests {
		if got := isGoTestBinary(tt.args); got != tt.want {
			t.Fatalf("%s: expected %v, got %v", tt.name, tt.want, got)
		}
	}
}
