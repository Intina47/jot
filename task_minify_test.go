package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJotMinifyFileWritesSiblingOutput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "data.json")
	if err := os.WriteFile(source, []byte("{\"name\":\"jot\",\"items\":[1,2,3]}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var out bytes.Buffer
	if err := jotMinifyWithInput(strings.NewReader(""), &out, []string{source}); err != nil {
		t.Fatalf("jotMinifyWithInput returned error: %v", err)
	}

	wantPath := filepath.Join(dir, "data.min.json")
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "{\"name\":\"jot\",\"items\":[1,2,3]}" {
		t.Fatalf("unexpected output %q", string(got))
	}
	if !strings.Contains(out.String(), "data.min.json") {
		t.Fatalf("expected summary to mention output file, got %q", out.String())
	}
}

func TestJotMinifyPrettyTextToStdout(t *testing.T) {
	var out bytes.Buffer
	err := jotMinifyWithInput(strings.NewReader(""), &out, []string{"--text", "{\"a\":1,\"b\":[2,3]}", "--pretty", "--indent", "4"})
	if err != nil {
		t.Fatalf("jotMinifyWithInput returned error: %v", err)
	}

	want := "{\n    \"a\": 1,\n    \"b\": [\n        2,\n        3\n    ]\n}"
	if out.String() != want {
		t.Fatalf("unexpected stdout %q", out.String())
	}
}

func TestJotMinifyStdinToStdout(t *testing.T) {
	var out bytes.Buffer
	err := jotMinifyWithInput(strings.NewReader("{\"a\":1}"), &out, []string{"--stdin"})
	if err != nil {
		t.Fatalf("jotMinifyWithInput returned error: %v", err)
	}

	if out.String() != "{\"a\":1}" {
		t.Fatalf("unexpected stdout %q", out.String())
	}
}

func TestJotMinifyRejectsConflictingInputs(t *testing.T) {
	var out bytes.Buffer
	err := jotMinifyWithInput(strings.NewReader(""), &out, []string{"data.json", "--text", "{\"a\":1}"})
	if err == nil {
		t.Fatal("expected error for conflicting inputs")
	}
	if got := err.Error(); !strings.Contains(got, "choose one input source") {
		t.Fatalf("unexpected error %q", got)
	}
}

func TestJotMinifyRejectsOverwriteConflict(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "data.json")
	outPath := filepath.Join(dir, "result.json")
	if err := os.WriteFile(source, []byte("{\"a\":1}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(outPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	var out bytes.Buffer
	err := jotMinifyWithInput(strings.NewReader(""), &out, []string{source, "--out", outPath})
	if err == nil {
		t.Fatal("expected overwrite error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error %q", err)
	}

	out.Reset()
	err = jotMinifyWithInput(strings.NewReader(""), &out, []string{source, "--out", outPath, "--overwrite"})
	if err != nil {
		t.Fatalf("jotMinifyWithInput returned error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "{\"a\":1}" {
		t.Fatalf("unexpected overwrite output %q", string(got))
	}
}

func TestJotMinifyFileStdout(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "data.json")
	if err := os.WriteFile(source, []byte("{\"a\":1}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var out bytes.Buffer
	if err := jotMinifyWithInput(strings.NewReader(""), &out, []string{source, "--stdout"}); err != nil {
		t.Fatalf("jotMinifyWithInput returned error: %v", err)
	}

	if out.String() != "{\"a\":1}" {
		t.Fatalf("unexpected stdout %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "data.min.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no sibling output file, got err=%v", err)
	}
}

func TestRenderMinifyHelpIncludesTaskFlow(t *testing.T) {
	help := renderMinifyHelp(false)
	for _, snippet := range []string{
		"jot minify",
		"--pretty",
		"--stdin",
		"jot task minify",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestRunMinifyTaskGuidedFlowWritesSiblingOutput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(source, []byte("{\"z\":true,\"a\":1}"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var out bytes.Buffer
	input := strings.Join([]string{
		"1",
		"payload.json",
		"",
	}, "\n") + "\n"
	if err := runMinifyTask(strings.NewReader(input), &out, dir); err != nil {
		t.Fatalf("runMinifyTask returned error: %v", err)
	}

	wantPath := filepath.Join(dir, "payload.min.json")
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "{\"z\":true,\"a\":1}" {
		t.Fatalf("unexpected guided output %q", string(got))
	}
	if !strings.Contains(out.String(), "next time: jot minify payload.json") {
		t.Fatalf("expected direct-command tip, got %q", out.String())
	}
}

func TestJotMinifyHelpFlag(t *testing.T) {
	var out bytes.Buffer
	err := jotMinifyWithInput(strings.NewReader(""), &out, []string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}
