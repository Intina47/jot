package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDiffHelpContainsCoreSurface(t *testing.T) {
	help := renderDiffHelp(false)
	for _, snippet := range []string{
		"jot diff",
		"--viewer",
		"--summary-only",
		"--context",
		"--ignore-whitespace",
		"--word-diff",
		"jot task diff",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotDiffSummaryReportsChanges(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "left.txt")
	right := filepath.Join(dir, "right.txt")
	if err := os.WriteFile(left, []byte("alpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatalf("write left failed: %v", err)
	}
	if err := os.WriteFile(right, []byte("alpha\ndelta\ngamma\n"), 0o600); err != nil {
		t.Fatalf("write right failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotDiffWithInput(strings.NewReader(""), &out, []string{left, right}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotDiffWithInput returned error: %v", err)
	}

	text := out.String()
	for _, snippet := range []string{
		"left.txt -> right.txt",
		"+1",
		"-1",
		"hunk(s)",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected summary to contain %q, got %q", snippet, text)
		}
	}
}

func TestJotDiffIgnoresEOLAndWhitespaceWhenAsked(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "left.txt")
	right := filepath.Join(dir, "right.txt")
	if err := os.WriteFile(left, []byte("alpha  beta\r\ngamma\r\n"), 0o600); err != nil {
		t.Fatalf("write left failed: %v", err)
	}
	if err := os.WriteFile(right, []byte("alpha beta\ngamma\n"), 0o600); err != nil {
		t.Fatalf("write right failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotDiffWithInput(strings.NewReader(""), &out, []string{left, right, "--ignore-whitespace"}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotDiffWithInput returned error: %v", err)
	}

	if !strings.Contains(out.String(), "are identical") {
		t.Fatalf("expected identical summary, got %q", out.String())
	}
}

func TestJotDiffRejectsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "left.bin")
	right := filepath.Join(dir, "right.txt")
	if err := os.WriteFile(left, []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatalf("write left failed: %v", err)
	}
	if err := os.WriteFile(right, []byte("text"), 0o600); err != nil {
		t.Fatalf("write right failed: %v", err)
	}

	var out bytes.Buffer
	err := jotDiffWithInput(strings.NewReader(""), &out, []string{left, right}, func() (string, error) {
		return dir, nil
	})
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary file error, got err=%v output=%q", err, out.String())
	}
}

func TestJotDiffViewerRendersWordDiffLocally(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "left.txt")
	right := filepath.Join(dir, "right.txt")
	if err := os.WriteFile(left, []byte("alpha beta gamma\n"), 0o600); err != nil {
		t.Fatalf("write left failed: %v", err)
	}
	if err := os.WriteFile(right, []byte("alpha delta gamma\n"), 0o600); err != nil {
		t.Fatalf("write right failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotDiffWithInput(strings.NewReader(""), &out, []string{left, right, "--viewer", "--word-diff", "--no-color"}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotDiffWithInput returned error: %v", err)
	}

	text := out.String()
	for _, snippet := range []string{
		"Diff Viewer",
		"@@",
		"[-beta-]",
		"{+delta+}",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected viewer output to contain %q, got %q", snippet, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("expected no-color output, got %q", text)
	}
}

func TestRunDiffTaskGuidedFlow(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "left.txt")
	right := filepath.Join(dir, "right.txt")
	if err := os.WriteFile(left, []byte("alpha beta gamma\n"), 0o600); err != nil {
		t.Fatalf("write left failed: %v", err)
	}
	if err := os.WriteFile(right, []byte("alpha delta gamma\n"), 0o600); err != nil {
		t.Fatalf("write right failed: %v", err)
	}

	var out bytes.Buffer
	input := strings.Join([]string{
		"left.txt",
		"right.txt",
		"y",
		"y",
		"y",
		"2",
	}, "\n") + "\n"
	if err := runDiffTask(strings.NewReader(input), &out, dir); err != nil {
		t.Fatalf("runDiffTask returned error: %v", err)
	}

	text := out.String()
	for _, snippet := range []string{
		"Diff Viewer",
		"next time: jot diff left.txt right.txt --viewer --word-diff --context 2",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected guided flow output to contain %q, got %q", snippet, text)
		}
	}
}
