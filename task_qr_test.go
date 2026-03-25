package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJotQRWritesPNGFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "qr.png")

	var out bytes.Buffer
	if err := jotQRWithInput(strings.NewReader(""), &out, []string{"--text", "https://example.com", "--png", "--out", outPath}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotQRWithInput returned error: %v", err)
	}

	if !strings.Contains(out.String(), "wrote "+outPath) {
		t.Fatalf("expected output summary, got %q", out.String())
	}

	file, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open png failed: %v", err)
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode png failed: %v", err)
	}
	if img.Bounds().Dx() != qrDefaultSize || img.Bounds().Dy() != qrDefaultSize {
		t.Fatalf("expected %dx%d png, got %v", qrDefaultSize, qrDefaultSize, img.Bounds())
	}

	blackSeen := false
	for y := 0; y < img.Bounds().Dy() && !blackSeen; y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r == 0 && g == 0 && b == 0 {
				blackSeen = true
				break
			}
		}
	}
	if !blackSeen {
		t.Fatal("expected at least one black QR module")
	}
}

func TestJotQRSVGContainsPayloadTitle(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "qr.svg")

	var out bytes.Buffer
	if err := jotQRWithInput(strings.NewReader(""), &out, []string{"--text", "hello world", "--svg", "--out", outPath}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotQRWithInput returned error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read svg failed: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "<svg") {
		t.Fatalf("expected svg markup, got %q", text)
	}
	if !strings.Contains(text, "<title>hello world</title>") {
		t.Fatalf("expected payload title in svg, got %q", text)
	}
}

func TestJotQRASCII(t *testing.T) {
	var out bytes.Buffer
	if err := jotQRWithInput(strings.NewReader(""), &out, []string{"--text", "hello world", "--ascii"}, func() (string, error) {
		return t.TempDir(), nil
	}); err != nil {
		t.Fatalf("jotQRWithInput returned error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "##") {
		t.Fatalf("expected ASCII QR output, got %q", text)
	}
	if lines := strings.Count(text, "\n"); lines < 10 {
		t.Fatalf("expected multi-line ASCII output, got %q", text)
	}
}

func TestJotQRReadsStdin(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "stdin.png")

	var out bytes.Buffer
	if err := jotQRWithInput(strings.NewReader("https://example.com"), &out, []string{"--stdin", "--out", outPath}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("jotQRWithInput returned error: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file, got %v", err)
	}
}

func TestJotQREmptyPayloadFails(t *testing.T) {
	var out bytes.Buffer
	err := jotQRWithInput(strings.NewReader(""), &out, []string{"--ascii"}, func() (string, error) {
		return t.TempDir(), nil
	})
	if err == nil {
		t.Fatal("expected empty payload error")
	}
}

func TestJotQROverwriteProtection(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "qr.png")

	if err := jotQRWithInput(strings.NewReader(""), &bytes.Buffer{}, []string{"--text", "hello", "--out", outPath}, func() (string, error) {
		return dir, nil
	}); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	err := jotQRWithInput(strings.NewReader(""), &bytes.Buffer{}, []string{"--text", "hello", "--out", outPath}, func() (string, error) {
		return dir, nil
	})
	if err == nil || !strings.Contains(err.Error(), "output already exists") {
		t.Fatalf("expected overwrite protection, got %v", err)
	}
}

func TestRenderQRHelp(t *testing.T) {
	help := renderQRHelp(false)
	for _, snippet := range []string{
		"jot qr",
		"--ascii",
		"--level L|M|Q|H",
		"jot task qr",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestRunQRTaskGuidedFlow(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	input := strings.NewReader("1\nhttps://example.com\n1\n\n")
	if err := runQRTask(input, &out, dir); err != nil {
		t.Fatalf("runQRTask returned error: %v", err)
	}

	if !strings.Contains(out.String(), "Select payload type [1]:") {
		t.Fatalf("expected guided prompt, got %q", out.String())
	}
	if !strings.Contains(out.String(), "next time: jot qr --text \"https://example.com\" --png --out \"qr.png\"") {
		t.Fatalf("expected direct command tip, got %q", out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "qr.png")); err != nil {
		t.Fatalf("expected qr.png to be written, got %v", err)
	}
}
