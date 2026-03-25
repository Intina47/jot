package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderResizeHelp(t *testing.T) {
	help := renderResizeHelp(false)
	for _, snippet := range []string{
		"jot resize",
		"WIDTHxHEIGHT",
		"--mode fit|fill|stretch",
		"jot task resize",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotResizeFitWritesSiblingOutput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	writeTestResizePNG(t, source, 40, 20)

	var out bytes.Buffer
	if err := jotResize(&out, []string{source, "20x20"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	output := filepath.Join(dir, "logo-20x20.png")
	assertResizeImageBounds(t, output, 20, 10)
	if !strings.Contains(out.String(), output) {
		t.Fatalf("expected output summary to mention %s, got %q", output, out.String())
	}
}

func TestJotResizeFillProducesExactDimensions(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "poster.png")
	writeTestResizePNG(t, source, 40, 20)

	var out bytes.Buffer
	if err := jotResize(&out, []string{source, "10x10", "--mode", "fill"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	output := filepath.Join(dir, "poster-10x10.png")
	assertResizeImageBounds(t, output, 10, 10)
}

func TestJotResizeStretchProducesExactDimensions(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "photo.jpg")
	writeTestResizeJPG(t, source, 30, 18)

	var out bytes.Buffer
	if err := jotResize(&out, []string{source, "12x7", "--mode", "stretch"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	output := filepath.Join(dir, "photo-12x7.jpg")
	assertResizeImageBounds(t, output, 12, 7)
}

func TestJotResizeRecursiveOutDirPreservesStructure(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "images")
	nestedDir := filepath.Join(sourceDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	source := filepath.Join(nestedDir, "shot.gif")
	writeTestResizeGIF(t, source, 50, 30)

	outDir := filepath.Join(dir, "resized")
	var out bytes.Buffer
	if err := jotResize(&out, []string{sourceDir, "16x16", "--recursive", "--out-dir", outDir, "--mode", "stretch"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	output := filepath.Join(outDir, "nested", "shot-16x16.gif")
	assertResizeImageBounds(t, output, 16, 16)
}

func TestJotResizeDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	writeTestResizePNG(t, source, 40, 20)

	var out bytes.Buffer
	if err := jotResize(&out, []string{source, "24x24", "--dry-run"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	output := filepath.Join(dir, "logo-24x24.png")
	if _, err := os.Stat(output); err == nil {
		t.Fatalf("expected dry-run not to write %s", output)
	}
	if !strings.Contains(out.String(), "would write") {
		t.Fatalf("expected dry-run output, got %q", out.String())
	}
}

func TestJotResizeForceAllowsOverwrite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	writeTestResizePNG(t, source, 40, 20)
	output := filepath.Join(dir, "logo-20x20.png")
	if err := os.WriteFile(output, []byte("collision"), 0o600); err != nil {
		t.Fatalf("write collision failed: %v", err)
	}

	if err := jotResize(&bytes.Buffer{}, []string{source, "20x20"}); err == nil {
		t.Fatal("expected collision error without --force")
	}

	if err := jotResize(&bytes.Buffer{}, []string{source, "20x20", "--force"}); err != nil {
		t.Fatalf("jotResize returned error with --force: %v", err)
	}
	assertResizeImageBounds(t, output, 20, 10)
}

func TestJotResizeInPlaceRewritesSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "logo.png")
	writeTestResizePNG(t, source, 40, 20)

	var out bytes.Buffer
	if err := jotResize(&out, []string{source, "10x10", "--in-place", "--mode", "stretch"}); err != nil {
		t.Fatalf("jotResize returned error: %v", err)
	}

	assertResizeImageBounds(t, source, 10, 10)
}

func TestJotResizeRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(source, []byte("not an image"), 0o600); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	if err := jotResize(&bytes.Buffer{}, []string{source, "10x10"}); err == nil {
		t.Fatal("expected unsupported input error")
	}
	if err := jotResize(&bytes.Buffer{}, []string{filepath.Join(dir, "logo.png"), "bad-size"}); err == nil {
		t.Fatal("expected invalid size error")
	}
}

func TestRunResizeTaskGuidedFlow(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "guide.png")
	writeTestResizePNG(t, source, 40, 20)

	var out bytes.Buffer
	input := strings.NewReader("\n20x10\n\n")
	if err := runResizeTask(input, &out, dir); err != nil {
		t.Fatalf("runResizeTask returned error: %v", err)
	}

	output := filepath.Join(dir, "guide-20x10.png")
	assertResizeImageBounds(t, output, 20, 10)
	text := out.String()
	for _, snippet := range []string{
		"Resize",
		"Source [1]:",
		"Target size [WIDTHxHEIGHT]:",
		"Select mode [fit]:",
		"next time: jot resize guide.png 20x10 --mode fit",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected guided flow output to contain %q, got %q", snippet, text)
		}
	}
}

func writeTestResizePNG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 7), B: 120, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png failed: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png failed: %v", err)
	}
}

func writeTestResizeJPG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: uint8(x * 3), B: uint8(y * 4), A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jpg failed: %v", err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode jpg failed: %v", err)
	}
}

func writeTestResizeGIF(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, width, height), []color.Color{
		color.RGBA{0, 0, 0, 0},
		color.RGBA{80, 160, 40, 255},
		color.RGBA{240, 40, 120, 255},
	})
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create gif failed: %v", err)
	}
	defer file.Close()
	if err := gif.Encode(file, img, nil); err != nil {
		t.Fatalf("encode gif failed: %v", err)
	}
}

func assertResizeImageBounds(t *testing.T, path string, wantW, wantH int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open output failed: %v", err)
	}
	defer file.Close()
	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decode config failed: %v", err)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Fatalf("expected %s to be %dx%d, got %dx%d", path, wantW, wantH, cfg.Width, cfg.Height)
	}
	if format == "" {
		t.Fatalf("expected decoded format for %s", path)
	}
}
