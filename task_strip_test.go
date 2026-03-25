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

func TestRenderStripHelp(t *testing.T) {
	help := renderStripHelp(false)
	for _, snippet := range []string{
		"jot strip",
		"--out-dir PATH",
		"--in-place",
		"--recursive",
		"GIF animation frames are preserved when possible",
		"JPEG output is re-encoded",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotStripWritesSiblingOutput(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "photo.jpg")
	writeStripTestJPEG(t, inputPath, 48, 32, true)

	var out bytes.Buffer
	if err := jotStrip(&out, []string{inputPath}); err != nil {
		t.Fatalf("jotStrip returned error: %v", err)
	}

	outputPath := filepath.Join(dir, "photo-stripped.jpg")
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	in, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read input failed: %v", err)
	}
	if len(got) >= len(in) {
		t.Fatalf("expected stripped file to be smaller than source, input=%d output=%d", len(in), len(got))
	}
	if bytes.Contains(got, []byte("Exif")) {
		t.Fatalf("expected metadata to be stripped, got %q", string(got[:min(32, len(got))]))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode output failed: %v", err)
	}
	if cfg.Width != 48 || cfg.Height != 32 {
		t.Fatalf("expected 48x32 output, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestJotStripFolderRecursivePreservesTree(t *testing.T) {
	workdir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.MkdirAll(filepath.Join(workdir, "images", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	writeStripTestPNG(t, filepath.Join(workdir, "images", "nested", "photo.png"), 24, 24)

	var out bytes.Buffer
	if err := jotStrip(&out, []string{"images", "--recursive"}); err != nil {
		t.Fatalf("jotStrip returned error: %v", err)
	}

	outputPath := filepath.Join(workdir, "stripped", "images", "nested", "photo-stripped.png")
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output at %s, got err=%v", outputPath, err)
	}
}

func TestJotStripDryRunReportsPlan(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "dryrun.png")
	writeStripTestPNG(t, inputPath, 16, 16)

	var out bytes.Buffer
	if err := jotStrip(&out, []string{inputPath, "--dry-run"}); err != nil {
		t.Fatalf("jotStrip returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dryrun-stripped.png")); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run not to write output, stat err=%v", err)
	}
	if !strings.Contains(out.String(), "DRY RUN") || !strings.Contains(out.String(), "->") {
		t.Fatalf("expected dry-run plan output, got %q", out.String())
	}
}

func TestJotStripInPlaceRequiresForce(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "photo.jpg")
	writeStripTestJPEG(t, inputPath, 32, 32, true)

	var out bytes.Buffer
	if err := jotStrip(&out, []string{inputPath, "--in-place"}); err == nil {
		t.Fatal("expected in-place without force to fail")
	}

	if err := jotStrip(&out, []string{inputPath, "--in-place", "--force"}); err != nil {
		t.Fatalf("jotStrip returned error: %v", err)
	}
	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("expected input file to still exist: %v", err)
	}
}

func TestJotStripGifPreservesFrames(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "anim.gif")
	writeStripTestGIF(t, inputPath)

	var out bytes.Buffer
	if err := jotStrip(&out, []string{inputPath}); err != nil {
		t.Fatalf("jotStrip returned error: %v", err)
	}

	outputPath := filepath.Join(dir, "anim-stripped.gif")
	f, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open output failed: %v", err)
	}
	defer f.Close()
	decoded, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatalf("decode gif failed: %v", err)
	}
	if len(decoded.Image) != 2 {
		t.Fatalf("expected two GIF frames, got %d", len(decoded.Image))
	}
}

func TestRunStripTaskGuidedFlow(t *testing.T) {
	workdir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	writeStripTestPNG(t, filepath.Join(workdir, "guided.png"), 20, 20)
	var out bytes.Buffer
	if err := runStripTask(strings.NewReader("\n"), &out, workdir); err != nil {
		t.Fatalf("runStripTask returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "guided-stripped.png")); err != nil {
		t.Fatalf("expected guided output, got err=%v", err)
	}
	for _, snippet := range []string{
		"Strip Metadata",
		"Source path [1]:",
		"next time: jot strip guided.png",
	} {
		if !strings.Contains(out.String(), snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, out.String())
		}
	}
}

func writeStripTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 180, A: 255})
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

func writeStripTestJPEG(t *testing.T, path string, w, h int, withMetadata bool) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 220, G: uint8(x * 3), B: uint8(y * 3), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg failed: %v", err)
	}
	data := buf.Bytes()
	if withMetadata {
		data = injectJPEGAPP1Metadata(t, data)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write jpeg failed: %v", err)
	}
}

func injectJPEGAPP1Metadata(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("unexpected jpeg header")
	}
	payload := append([]byte("Exif\x00\x00"), bytes.Repeat([]byte("jot"), 128)...)
	segmentLen := len(payload) + 2
	var out bytes.Buffer
	out.Write(data[:2])
	out.Write([]byte{0xFF, 0xE1, byte(segmentLen >> 8), byte(segmentLen)})
	out.Write(payload)
	out.Write(data[2:])
	return out.Bytes()
}

func writeStripTestGIF(t *testing.T, path string) {
	t.Helper()
	pal := []color.Color{color.Black, color.White, color.RGBA{R: 255, G: 0, B: 0, A: 255}, color.RGBA{R: 0, G: 255, B: 0, A: 255}}
	frame1 := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
	frame2 := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			frame1.SetColorIndex(x, y, uint8((x+y)%2))
			frame2.SetColorIndex(x, y, uint8((x/4+y/4)%4))
		}
	}
	anim := &gif.GIF{
		Image: []*image.Paletted{frame1, frame2},
		Delay: []int{5, 5},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create gif failed: %v", err)
	}
	defer file.Close()
	if err := gif.EncodeAll(file, anim); err != nil {
		t.Fatalf("encode gif failed: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
