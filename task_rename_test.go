package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJotRenamePreviewPrefixLeavesSourceUntouched(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(source, []byte("image"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotRename(&out, []string{source, "--prefix", "icon-", "--dry-run"}); err != nil {
		t.Fatalf("jotRename returned error: %v", err)
	}

	if _, err := os.Stat(source); err != nil {
		t.Fatalf("expected source to remain, got err=%v", err)
	}
	if strings.Contains(out.String(), "renamed 1 file(s)") == false {
		t.Fatalf("expected preview summary, got %q", out.String())
	}
	if !strings.Contains(out.String(), "logo.png -> icon-logo.png") {
		t.Fatalf("expected preview mapping, got %q", out.String())
	}
}

func TestJotRenameApplyPrefixRenamesFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(source, []byte("image"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotRename(&out, []string{source, "--prefix", "icon-", "--apply"}); err != nil {
		t.Fatalf("jotRename returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "icon-logo.png")); err != nil {
		t.Fatalf("expected renamed file, got err=%v", err)
	}
	if !strings.Contains(out.String(), "renamed 1 file(s)") {
		t.Fatalf("expected success summary, got %q", out.String())
	}
}

func TestJotRenameRecursiveFolderTouchesNestedFiles(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	top := filepath.Join(dir, "one.txt")
	deep := filepath.Join(subdir, "two.txt")
	if err := os.WriteFile(top, []byte("one"), 0o600); err != nil {
		t.Fatalf("write top failed: %v", err)
	}
	if err := os.WriteFile(deep, []byte("two"), 0o600); err != nil {
		t.Fatalf("write deep failed: %v", err)
	}

	if err := jotRename(&bytes.Buffer{}, []string{dir, "--prefix", "x-", "--recursive", "--apply"}); err != nil {
		t.Fatalf("jotRename returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "x-one.txt")); err != nil {
		t.Fatalf("expected top-level rename, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(subdir, "x-two.txt")); err != nil {
		t.Fatalf("expected recursive rename, got err=%v", err)
	}
}

func TestJotRenameTemplateAndConflictSuffix(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "alpha.txt")
	second := filepath.Join(dir, "beta.txt")
	if err := os.WriteFile(first, []byte("alpha"), 0o600); err != nil {
		t.Fatalf("write first failed: %v", err)
	}
	if err := os.WriteFile(second, []byte("beta"), 0o600); err != nil {
		t.Fatalf("write second failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotRename(&out, []string{filepath.Join(dir, "*.txt"), "--template", "{n:02}-{stem}{ext}", "--on-conflict", "suffix", "--apply"}); err != nil {
		t.Fatalf("jotRename returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "01-alpha.txt")); err != nil {
		t.Fatalf("expected first rename, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "02-beta.txt")); err != nil {
		t.Fatalf("expected second rename, got err=%v", err)
	}
}

func TestJotRenameGuidedFlowPrintsTip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(source, []byte("photo"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	input := strings.NewReader("\n2\nicon-\ny\n")
	if err := runRenameTask(input, &out, dir); err != nil {
		t.Fatalf("runRenameTask returned error: %v", err)
	}

	text := out.String()
	for _, snippet := range []string{
		"Rename",
		"Select source [1]:",
		"strategy",
		"Apply these renames [y/N]:",
		"next time: jot rename <selector> --prefix TEXT --apply",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected guided flow output to contain %q, got %q", snippet, text)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "icon-photo.jpg")); err != nil {
		t.Fatalf("expected renamed file, got err=%v", err)
	}
}

