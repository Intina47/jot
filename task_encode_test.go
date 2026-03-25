package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderEncodeHelpContainsCoreSurface(t *testing.T) {
	help := renderEncodeHelp(false)
	for _, snippet := range []string{
		"jot encode",
		"--decode",
		"--text VALUE",
		"--stdin",
		"--force-text",
		"jot encode --stdin --decode --stdout",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestEncodeFileCreatesSiblingBase64File(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "logo.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(""), &out, []string{sourcePath}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}

	outputPath := filepath.Join(dir, "logo.txt.b64.txt")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if strings.TrimSpace(string(got)) != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("unexpected output contents: %q", string(got))
	}
	if !strings.Contains(out.String(), "encoded ") || !strings.Contains(out.String(), "logo.txt ->") {
		t.Fatalf("expected success summary, got %q", out.String())
	}
}

func TestDecodeFileRestoresSiblingName(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "payload.b64.txt")
	if err := os.WriteFile(sourcePath, []byte(base64.StdEncoding.EncodeToString([]byte("hello"))), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(""), &out, []string{sourcePath, "--decode"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}

	outputPath := filepath.Join(dir, "payload")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected decoded contents: %q", string(got))
	}
	if !strings.Contains(out.String(), "decoded ") || !strings.Contains(out.String(), "payload.b64.txt ->") {
		t.Fatalf("expected decode summary, got %q", out.String())
	}
}

func TestEncodeTextToStdout(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(""), &out, []string{"--text", "hello world"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != base64.StdEncoding.EncodeToString([]byte("hello world")) {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
}

func TestDecodeTextToStdout(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(""), &out, []string{"--text", base64.StdEncoding.EncodeToString([]byte("hello")), "--decode"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "hello" {
		t.Fatalf("unexpected decoded stdout output: %q", out.String())
	}
}

func TestEncodeStdinToStdout(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := encodeCLI(strings.NewReader("hello"), &out, []string{"--stdin"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("unexpected stdin encode output: %q", out.String())
	}
}

func TestDecodeStdinToStdout(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(base64.StdEncoding.EncodeToString([]byte("hello"))), &out, []string{"--stdin", "--decode"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "hello" {
		t.Fatalf("unexpected stdin decode output: %q", out.String())
	}
}

func TestDecodeBinaryStdoutRequiresForceText(t *testing.T) {
	dir := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})

	var out bytes.Buffer
	err := encodeCLI(strings.NewReader(""), &out, []string{"--text", encoded, "--decode", "--stdout"}, func() (string, error) {
		return dir, nil
	})
	if err == nil || !strings.Contains(err.Error(), "decoded output is binary") {
		t.Fatalf("expected binary stdout error, got err=%v output=%q", err, out.String())
	}

	out.Reset()
	err = encodeCLI(strings.NewReader(""), &out, []string{"--text", encoded, "--decode", "--stdout", "--force-text"}, func() (string, error) {
		return dir, nil
	})
	if err != nil {
		t.Fatalf("encodeCLI returned error: %v", err)
	}
	if !bytes.Equal(out.Bytes(), []byte{0xff, 0xfe}) {
		t.Fatalf("unexpected raw stdout bytes: %v", out.Bytes())
	}
}

func TestEncodeRefusesOverwriteWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "note.txt")
	outputPath := filepath.Join(dir, "note.txt.b64.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write output failed: %v", err)
	}

	err := encodeCLI(strings.NewReader(""), ioDiscard{}, []string{sourcePath}, func() (string, error) {
		return dir, nil
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite error, got %v", err)
	}
}

func TestRunEncodeTaskFileFlow(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "alpha.txt")
	if err := os.WriteFile(sourcePath, []byte("alpha"), 0o600); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var out bytes.Buffer
	err := runEncodeTask(strings.NewReader("1\n1\n1\n"), &out, dir)
	if err != nil {
		t.Fatalf("runEncodeTask returned error: %v", err)
	}

	outputPath := filepath.Join(dir, "alpha.txt.b64.txt")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if strings.TrimSpace(string(got)) != base64.StdEncoding.EncodeToString([]byte("alpha")) {
		t.Fatalf("unexpected encoded contents: %q", string(got))
	}
	if !strings.Contains(out.String(), "next time: jot encode \"") {
		t.Fatalf("expected direct command tip, got %q", out.String())
	}
}

func TestRunEncodeTaskTextFlow(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := runEncodeTask(strings.NewReader("1\n2\nhello world\n"), &out, dir)
	if err != nil {
		t.Fatalf("runEncodeTask returned error: %v", err)
	}
	if !strings.Contains(out.String(), base64.StdEncoding.EncodeToString([]byte("hello world"))) {
		t.Fatalf("expected encoded text in output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "next time: jot encode --text") {
		t.Fatalf("expected tip in output, got %q", out.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
