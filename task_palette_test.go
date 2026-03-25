package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePaletteTestImage(t *testing.T, path string, width, height int, pixels []color.RGBA) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height && i < len(pixels); i++ {
		x := i % width
		y := i / width
		img.SetRGBA(x, y, pixels[i])
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create palette image failed: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode palette image failed: %v", err)
	}
}

func TestPaletteCommandEntryDirectHex(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "palette.png")
	writePaletteTestImage(t, imgPath, 3, 2, []color.RGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	handled, code := paletteCommandEntry([]string{"palette", imgPath, "--count", "2", "--format", "hex"}, strings.NewReader(""), &out, &errOut, func() (string, error) {
		return dir, nil
	})
	if !handled || code != 0 {
		t.Fatalf("expected palette entrypoint to handle command successfully, handled=%v code=%d stderr=%q", handled, code, errOut.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 palette lines, got %q", out.String())
	}
	if lines[0] != "#ff0000" || lines[1] != "#00ff00" {
		t.Fatalf("expected dominant palette order red then green, got %q", out.String())
	}
}

func TestPaletteCommandEntryIgnoresAlphaWhenRequested(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "alpha.png")
	writePaletteTestImage(t, imgPath, 2, 2, []color.RGBA{
		{R: 0, G: 255, B: 0, A: 128},
		{R: 0, G: 255, B: 0, A: 128},
		{R: 0, G: 255, B: 0, A: 128},
		{R: 255, G: 0, B: 0, A: 255},
	})

	var without bytes.Buffer
	handled, code := paletteCommandEntry([]string{"palette", imgPath, "--count", "1", "--format", "hex"}, strings.NewReader(""), &without, &bytes.Buffer{}, func() (string, error) {
		return dir, nil
	})
	if !handled || code != 0 {
		t.Fatalf("expected direct palette command to succeed, code=%d", code)
	}
	if !strings.Contains(without.String(), "#007f00") {
		t.Fatalf("expected translucent green to participate without --ignore-alpha, got %q", without.String())
	}

	var with bytes.Buffer
	handled, code = paletteCommandEntry([]string{"palette", imgPath, "--count", "1", "--format", "hex", "--ignore-alpha"}, strings.NewReader(""), &with, &bytes.Buffer{}, func() (string, error) {
		return dir, nil
	})
	if !handled || code != 0 {
		t.Fatalf("expected direct palette command to succeed with ignore-alpha, code=%d", code)
	}
	if !strings.Contains(with.String(), "#ff0000") {
		t.Fatalf("expected opaque red to remain when ignoring alpha, got %q", with.String())
	}
}

func TestPaletteCommandEntryJsonAndSort(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "luma.png")
	writePaletteTestImage(t, imgPath, 2, 2, []color.RGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 0, G: 0, B: 0, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
	})

	var out bytes.Buffer
	handled, code := paletteCommandEntry([]string{"palette", imgPath, "--count", "2", "--format", "json", "--sort", "luma"}, strings.NewReader(""), &out, &bytes.Buffer{}, func() (string, error) {
		return dir, nil
	})
	if !handled || code != 0 {
		t.Fatalf("expected palette entrypoint to succeed, code=%d", code)
	}

	var payload paletteOutput
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, out.String())
	}
	if len(payload.Colors) != 2 {
		t.Fatalf("expected 2 colors, got %+v", payload)
	}
	if payload.Colors[0].Hex != "#000000" || payload.Colors[1].Hex != "#ffffff" {
		t.Fatalf("expected luma ordering black then white, got %+v", payload.Colors)
	}
}

func TestPaletteTaskMenuListsPalette(t *testing.T) {
	var out bytes.Buffer
	handled, code := paletteCommandEntry([]string{"task"}, strings.NewReader("99\n"), &out, &bytes.Buffer{}, func() (string, error) {
		return t.TempDir(), nil
	})
	if !handled {
		t.Fatal("expected task entrypoint to handle the command")
	}
	if code == 0 {
		t.Fatal("expected task menu to reject unknown selection")
	}
	if !strings.Contains(out.String(), "palette extraction") {
		t.Fatalf("expected task menu to list palette extraction, got %q", out.String())
	}
}

func TestPaletteTaskFlow(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "palette.png")
	writePaletteTestImage(t, imgPath, 3, 2, []color.RGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
	})

	var out bytes.Buffer
	handled, code := paletteCommandEntry([]string{"task", "palette"}, strings.NewReader("\n2\n1\nn\n1\n"), &out, &bytes.Buffer{}, func() (string, error) {
		return dir, nil
	})
	if !handled || code != 0 {
		t.Fatalf("expected task palette flow to succeed, code=%d", code)
	}
	text := out.String()
	for _, snippet := range []string{
		"Palette",
		"Select image",
		"Select format",
		"Select sort",
		"#ff0000",
		"#00ff00",
		"next time: jot palette palette.png --count 2",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected task flow output to contain %q, got %q", snippet, text)
		}
	}
}

func TestPaletteHelpRendering(t *testing.T) {
	help := renderPaletteHelp(false)
	for _, snippet := range []string{
		"jot palette",
		"--format hex|swatch|json",
		"jot task palette",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
	taskHelp := renderTaskPaletteHelp(false)
	for _, snippet := range []string{
		"jot task",
		"palette extraction",
		"jot palette logo.png --count 5",
	} {
		if !strings.Contains(taskHelp, snippet) {
			t.Fatalf("expected task help to contain %q, got %q", snippet, taskHelp)
		}
	}
}
