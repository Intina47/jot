package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnvTool(t *testing.T) {
	cases := map[string]string{
		"git":     "",
		"node":    "nodejs",
		"nodejs":  "nodejs",
		"npm":     "nodejs",
		"go":      "golang",
		"golang":  "golang",
		"python3": "python",
	}
	for input, wantPlugin := range cases {
		spec, ok := resolveEnvTool(input)
		if !ok {
			t.Fatalf("expected tool %q to resolve", input)
		}
		if spec.Plugin != wantPlugin {
			t.Fatalf("resolveEnvTool(%q) plugin = %q, want %q", input, spec.Plugin, wantPlugin)
		}
	}
}

func TestEnvVersionScope(t *testing.T) {
	dir := t.TempDir()
	scope, gotDir := envVersionScope(dir)
	if scope != "global" || gotDir != dir {
		t.Fatalf("expected global scope for empty dir, got (%q, %q)", scope, gotDir)
	}

	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatalf("write go.mod failed: %v", err)
	}
	scope, gotDir = envVersionScope(goDir)
	if scope != "local" || gotDir != goDir {
		t.Fatalf("expected local scope for project dir, got (%q, %q)", scope, gotDir)
	}
}

func TestEnvRecommendations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatalf("write go.mod failed: %v", err)
	}
	items := envRecommendations(dir)
	if len(items) == 0 {
		t.Fatal("expected recommendations")
	}
	foundGo := false
	for _, item := range items {
		joined := item.Title + " " + item.Command + " " + item.Reason
		if strings.Contains(joined, "Go") || strings.Contains(joined, "go") {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Fatalf("expected go recommendation, got %#v", items)
	}
}

func TestRenderEnvHelp(t *testing.T) {
	text := renderEnvHelp(false)
	for _, fragment := range []string{
		"jot env",
		"jot env install <tool>",
		"jot env run <tool> [args...]",
		"jot env setup web",
		"jot env setup go",
		"jot env recommend",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected help to contain %q", fragment)
		}
	}
}

func TestEnvProjectToolNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatalf("write go.mod failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write package.json failed: %v", err)
	}
	names := envProjectToolNames(dir)
	joined := strings.Join(names, ",")
	for _, expected := range []string{"git", "go", "node"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in project tool names, got %q", expected, joined)
		}
	}
}

func TestEnvASDFInitSnippet(t *testing.T) {
	snippet := envASDFInitSnippet()
	for _, fragment := range []string{
		"# >>> jot asdf >>>",
		"export ASDF_DATA_DIR=\"${ASDF_DATA_DIR:-$HOME/.asdf}\"",
		"export PATH=\"$HOME/.jot/bin:$ASDF_DATA_DIR/shims:$PATH\"",
		". \"$HOME/.asdf/asdf.sh\"",
	} {
		if !strings.Contains(snippet, fragment) {
			t.Fatalf("expected snippet to contain %q", fragment)
		}
	}
}

func TestEnvASDFShellScript(t *testing.T) {
	script := envASDFShellScript("C:/Users/test/.asdf", "asdf", "install", "golang", "1.24.0")
	for _, fragment := range []string{
		"export ASDF_DIR='C:/Users/test/.asdf'",
		"export PATH=\"$ASDF_DIR/bin:$ASDF_DIR/shims:$PATH\"",
		". \"$ASDF_DIR/asdf.sh\"",
		"'asdf' 'install' 'golang' '1.24.0'",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected script to contain %q", fragment)
		}
	}
}

func TestEnvEnsureShellInitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := envEnsureShellInitFile(path); err != nil {
		t.Fatalf("envEnsureShellInitFile returned error: %v", err)
	}
	if err := envEnsureShellInitFile(path); err != nil {
		t.Fatalf("envEnsureShellInitFile returned error on second write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bashrc failed: %v", err)
	}
	content := string(data)
	if strings.Count(content, "# >>> jot asdf >>>") != 1 {
		t.Fatalf("expected exactly one jot asdf block, got %q", content)
	}
}

func TestEnvPreferredASDFBinaryPath(t *testing.T) {
	got := envPreferredASDFBinaryPath("C:/Users/test")
	if !strings.Contains(strings.ReplaceAll(got, "\\", "/"), "/.jot/bin/") {
		t.Fatalf("expected jot-managed bin path, got %q", got)
	}
}

func TestEnvASDFAssetMatchers(t *testing.T) {
	matchers := envASDFAssetMatchers()
	if len(matchers) == 0 {
		t.Fatal("expected asset matchers")
	}
}
