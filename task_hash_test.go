package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJotHashFileWritesSiblingOutput(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	content := []byte("hello world")
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotHashWithInput(strings.NewReader(""), &out, []string{inputPath}); err != nil {
		t.Fatalf("jotHashWithInput returned error: %v", err)
	}

	outputPath := inputPath + ".sha256.hash.txt"
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	sum := sha256.Sum256(content)
	wantLine := fmt.Sprintf("SHA256  sample.txt  %x\n", sum)
	if string(got) != wantLine {
		t.Fatalf("expected output %q, got %q", wantLine, string(got))
	}
	if !strings.Contains(out.String(), "wrote "+outputPath) {
		t.Fatalf("expected success summary, got %q", out.String())
	}
}

func TestJotHashTextWritesDigestToStdout(t *testing.T) {
	var out bytes.Buffer
	if err := jotHashWithInput(strings.NewReader(""), &out, []string{"--text", "hello", "--algo", "sha1"}); err != nil {
		t.Fatalf("jotHashWithInput returned error: %v", err)
	}

	if !strings.Contains(out.String(), "SHA1  text  ") {
		t.Fatalf("expected digest line, got %q", out.String())
	}
}

func TestJotHashStdinWritesDigestToStdout(t *testing.T) {
	var out bytes.Buffer
	if err := jotHashWithInput(strings.NewReader("hello\n"), &out, []string{"--stdin", "--algo", "md5"}); err != nil {
		t.Fatalf("jotHashWithInput returned error: %v", err)
	}

	if !strings.Contains(out.String(), "MD5  stdin  ") {
		t.Fatalf("expected digest line, got %q", out.String())
	}
}

func TestJotHashAllWritesAllAlgorithms(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(inputPath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	var out bytes.Buffer
	if err := jotHashWithInput(strings.NewReader(""), &out, []string{inputPath, "--all"}); err != nil {
		t.Fatalf("jotHashWithInput returned error: %v", err)
	}

	outputPath := inputPath + ".all.hash.txt"
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	wantOrder := []string{"MD5", "SHA1", "SHA256", "SHA512"}
	if len(lines) != len(wantOrder) {
		t.Fatalf("expected %d lines, got %d: %q", len(wantOrder), len(lines), string(got))
	}
	for i, prefix := range wantOrder {
		if !strings.HasPrefix(lines[i], prefix+"  sample.txt  ") {
			t.Fatalf("expected line %d to start with %q, got %q", i, prefix+"  sample.txt  ", lines[i])
		}
	}
}

func TestJotHashVerifySuccess(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	content := []byte("hello world")
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}
	sum := sha256.Sum256(content)

	var out bytes.Buffer
	err := jotHashWithInput(strings.NewReader(""), &out, []string{inputPath, "--verify", fmt.Sprintf("SHA256: %x", sum)})
	if err != nil {
		t.Fatalf("jotHashWithInput returned error: %v", err)
	}
	if !strings.Contains(out.String(), "verified SHA256 sample.txt") {
		t.Fatalf("expected verified output, got %q", out.String())
	}
}

func TestJotHashVerifyMismatch(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(inputPath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	var out bytes.Buffer
	err := jotHashWithInput(strings.NewReader(""), &out, []string{inputPath, "--verify", "SHA256: 0000"})
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected verification error, got %v", err)
	}
}

func TestJotHashRejectsConflictingInputs(t *testing.T) {
	var out bytes.Buffer
	err := jotHashWithInput(strings.NewReader(""), &out, []string{"sample.txt", "--text", "hello"})
	if err == nil {
		t.Fatal("expected conflicting input error")
	}
}

func TestJotHashHelpRendering(t *testing.T) {
	help := renderHashHelp(false)
	for _, snippet := range []string{
		"jot hash",
		"Compute or verify common digests",
		"--verify DIGEST",
		"jot hash package.zip",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestRenderTaskHashHelpRendering(t *testing.T) {
	help := renderTaskHashHelp(false)
	for _, snippet := range []string{
		"jot task",
		"jot task convert",
		"jot task hash",
		"guided front door for jot's task layer",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestRunHashTaskComputeFlow(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	content := []byte("hello world")
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	var out bytes.Buffer
	// mode=compute, input=file, explicit path, algo=sha256
	input := strings.NewReader("1\n1\nsample.txt\n1\n\n")
	if err := runHashTask(input, &out, dir); err != nil {
		t.Fatalf("runHashTask returned error: %v", err)
	}

	if !strings.Contains(out.String(), "wrote "+inputPath+".sha256.hash.txt") {
		t.Fatalf("expected output summary, got %q", out.String())
	}
	if !strings.Contains(out.String(), "next time: jot hash sample.txt") {
		t.Fatalf("expected direct command tip, got %q", out.String())
	}
}

func TestRunHashTaskVerifyFlow(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "sample.txt")
	content := []byte("hello world")
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}
	sum := sha256.Sum256(content)

	var out bytes.Buffer
	// mode=verify, input=file, explicit path, default algo sha256, expected digest
	input := strings.NewReader("2\n1\nsample.txt\n1\nSHA256: " + fmt.Sprintf("%x", sum) + "\n")
	if err := runHashTask(input, &out, dir); err != nil {
		t.Fatalf("runHashTask returned error: %v", err)
	}

	if !strings.Contains(out.String(), "verified SHA256 sample.txt") {
		t.Fatalf("expected verify output, got %q", out.String())
	}
}
