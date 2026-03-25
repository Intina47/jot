package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func withChdir(t *testing.T, dir string) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func readZipEntries(t *testing.T, path string) []string {
	t.Helper()

	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader failed: %v", err)
	}
	defer r.Close()

	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func readTarEntries(t *testing.T, path string) []string {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	reader := io.Reader(file)
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			t.Fatalf("gzip.NewReader failed: %v", err)
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar reader failed: %v", err)
		}
		names = append(names, hdr.Name)
	}
	sort.Strings(names)
	return names
}

func readArchiveEntries(t *testing.T, path string) []string {
	t.Helper()

	switch {
	case strings.HasSuffix(strings.ToLower(path), ".zip"):
		return readZipEntries(t, path)
	case strings.HasSuffix(strings.ToLower(path), ".tar.gz"):
		return readTarEntries(t, path)
	case strings.HasSuffix(strings.ToLower(path), ".tar"):
		return readTarEntries(t, path)
	default:
		t.Fatalf("unsupported archive type for %s", path)
		return nil
	}
}

func TestRenderCompressHelpIncludesCoreSurface(t *testing.T) {
	help := renderCompressHelp(false)
	for _, snippet := range []string{
		"jot compress",
		"<path...> [zip|tar|tar.gz]",
		"--format FORMAT",
		"--include-hidden",
		"--exclude PATTERN",
		"jot compress ./project zip",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
	if strings.Contains(help, "\x1b[") {
		t.Fatalf("expected plain help output without ANSI escapes, got %q", help)
	}
}

func TestParseCompressArgsDefaultAndPositionalFormat(t *testing.T) {
	opts, err := resolveCompressArgs([]string{"./project"})
	if err != nil {
		t.Fatalf("resolveCompressArgs returned error: %v", err)
	}
	if opts.Format != "zip" {
		t.Fatalf("expected default zip format, got %q", opts.Format)
	}

	opts, err = resolveCompressArgs([]string{"./project", "tar.gz"})
	if err != nil {
		t.Fatalf("resolveCompressArgs returned error: %v", err)
	}
	if opts.Format != "tar.gz" {
		t.Fatalf("expected positional format tar.gz, got %q", opts.Format)
	}
	if !reflect.DeepEqual(opts.Inputs, []string{"./project"}) {
		t.Fatalf("unexpected inputs: %#v", opts.Inputs)
	}
}

func TestJotCompressWritesZipSiblingByDefault(t *testing.T) {
	workdir := t.TempDir()
	sourcePath := filepath.Join(workdir, "notes.txt")
	writeTestFile(t, sourcePath, "alpha\nbeta\n")

	var out bytes.Buffer
	if err := jotCompress(&out, []string{sourcePath}); err != nil {
		t.Fatalf("jotCompress returned error: %v", err)
	}

	wantArchive := filepath.Join(workdir, "notes.zip")
	if _, err := os.Stat(wantArchive); err != nil {
		t.Fatalf("expected archive %q: %v", wantArchive, err)
	}
	gotEntries := readArchiveEntries(t, wantArchive)
	wantEntries := []string{"notes.txt"}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("unexpected archive entries: got %v want %v", gotEntries, wantEntries)
	}
	if !strings.Contains(out.String(), "notes.zip") {
		t.Fatalf("expected success output to mention notes.zip, got %q", out.String())
	}
}

func TestJotCompressTarGzFolderKeepsStructureAndSkipsHidden(t *testing.T) {
	workdir := t.TempDir()
	projectDir := filepath.Join(workdir, "project")
	writeTestFile(t, filepath.Join(projectDir, "README.md"), "project\n")
	writeTestFile(t, filepath.Join(projectDir, "sub", "app.txt"), "hello\n")
	writeTestFile(t, filepath.Join(projectDir, ".secret"), "hidden\n")

	var out bytes.Buffer
	if err := jotCompress(&out, []string{projectDir, "--format", "tar.gz", "--name", "bundle"}); err != nil {
		t.Fatalf("jotCompress returned error: %v", err)
	}

	wantArchive := filepath.Join(workdir, "bundle.tar.gz")
	if _, err := os.Stat(wantArchive); err != nil {
		t.Fatalf("expected archive %q: %v", wantArchive, err)
	}
	gotEntries := readArchiveEntries(t, wantArchive)
	wantEntries := []string{
		"project/",
		"project/README.md",
		"project/sub/",
		"project/sub/app.txt",
	}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("unexpected archive entries: got %v want %v", gotEntries, wantEntries)
	}
	if strings.Contains(strings.Join(gotEntries, ","), ".secret") {
		t.Fatalf("hidden file should not have been archived: %v", gotEntries)
	}
}

func TestJotCompressIncludeHiddenAndExcludePattern(t *testing.T) {
	workdir := t.TempDir()
	projectDir := filepath.Join(workdir, "project")
	writeTestFile(t, filepath.Join(projectDir, "visible.txt"), "visible\n")
	writeTestFile(t, filepath.Join(projectDir, ".hidden.txt"), "hidden\n")
	writeTestFile(t, filepath.Join(projectDir, "skip.log"), "skip\n")

	var out bytes.Buffer
	if err := jotCompress(&out, []string{projectDir, "--format", "zip", "--include-hidden", "--exclude", "*.log"}); err != nil {
		t.Fatalf("jotCompress returned error: %v", err)
	}

	wantArchive := filepath.Join(workdir, "project.zip")
	gotEntries := readArchiveEntries(t, wantArchive)
	wantEntries := []string{
		"project/",
		"project/.hidden.txt",
		"project/visible.txt",
	}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("unexpected archive entries: got %v want %v", gotEntries, wantEntries)
	}
}

func TestJotCompressDryRunDoesNotWriteArchive(t *testing.T) {
	workdir := t.TempDir()
	projectDir := filepath.Join(workdir, "project")
	writeTestFile(t, filepath.Join(projectDir, "visible.txt"), "visible\n")

	var out bytes.Buffer
	if err := jotCompress(&out, []string{projectDir, "--dry-run"}); err != nil {
		t.Fatalf("jotCompress returned error: %v", err)
	}
	if !strings.Contains(out.String(), "dry run: would create project.zip") {
		t.Fatalf("expected dry-run output, got %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(workdir, "project.zip")); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run to avoid writing archive, stat err=%v", err)
	}
}

func TestJotCompressForceOverwritesExistingArchive(t *testing.T) {
	workdir := t.TempDir()
	withChdir(t, workdir)

	writeTestFile(t, filepath.Join(workdir, "alpha.txt"), "alpha\n")
	writeTestFile(t, filepath.Join(workdir, "archive.tar"), "old data")

	var out bytes.Buffer
	if err := jotCompress(&out, []string{"alpha.txt", "--format", "tar", "--out", "archive.tar", "--force"}); err != nil {
		t.Fatalf("jotCompress returned error: %v", err)
	}

	gotEntries := readArchiveEntries(t, filepath.Join(workdir, "archive.tar"))
	wantEntries := []string{"alpha.txt"}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("unexpected archive entries: got %v want %v", gotEntries, wantEntries)
	}
}

func TestRunCompressTaskShowsTipAndCreatesArchive(t *testing.T) {
	workdir := t.TempDir()
	writeTestFile(t, filepath.Join(workdir, "one.txt"), "one\n")

	var out bytes.Buffer
	input := strings.NewReader("\n\n\nn\n")
	if err := runCompressTask(input, &out, workdir); err != nil {
		t.Fatalf("runCompressTask returned error: %v", err)
	}

	wantArchive := filepath.Join(workdir, "one.zip")
	if _, err := os.Stat(wantArchive); err != nil {
		t.Fatalf("expected archive %q: %v", wantArchive, err)
	}
	if !strings.Contains(out.String(), "jot compress one.txt") {
		t.Fatalf("expected direct command tip, got %q", out.String())
	}
}
