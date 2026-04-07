package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type envToolSpec struct {
	Name       string
	Plugin     string
	Executable string
	Aliases    []string
}

type envRecommendation struct {
	Title   string
	Command string
	Reason  string
}

var envToolCatalog = []envToolSpec{
	{Name: "git", Executable: "git"},
	{Name: "node", Plugin: "nodejs", Executable: "node", Aliases: []string{"nodejs", "npm", "npx"}},
	{Name: "go", Plugin: "golang", Executable: "go", Aliases: []string{"golang"}},
	{Name: "python", Plugin: "python", Executable: "python", Aliases: []string{"python3", "py"}},
}

type envASDF struct {
	path        string
	dir         string
	shellInit   bool
	commandName string
}

func jotEnv(stdin io.Reader, stdout io.Writer, args []string) error {
	if runtime.GOOS != "windows" {
		return errors.New("jot env currently supports Windows only")
	}
	if len(args) == 0 || (len(args) == 1 && isHelpFlag(args[0])) {
		return writeHelp(stdout, "env")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help":
		return writeHelp(stdout, "env")
	case "install":
		return jotEnvInstall(stdout, args[1:])
	case "run":
		return jotEnvRun(stdin, stdout, args[1:])
	case "setup":
		return jotEnvSetup(stdin, stdout, args[1:])
	case "recommend":
		return jotEnvRecommend(stdout, args[1:])
	default:
		return fmt.Errorf("unknown env subcommand %q", args[0])
	}
}

func jotEnvInstall(stdout io.Writer, args []string) error {
	if len(args) != 1 || isHelpFlag(args[0]) {
		return writeHelp(stdout, "env")
	}
	spec, ok := resolveEnvTool(args[0])
	if !ok {
		return fmt.Errorf("unsupported tool %q", args[0])
	}
	ui := newTermUI(stdout)
	if runtime.GOOS == "windows" {
		if err := envEnsureWindowsToolInstalled(stdout, ui, spec); err != nil {
			return err
		}
		path, err := envLocateToolExecutable(spec)
		if err == nil && path != "" {
			if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("%s executable: %s", strings.Title(spec.Name), path))); err != nil {
				return err
			}
		}
		return nil
	}
	asdf, err := newEnvASDF(stdout)
	if err != nil {
		return err
	}
	scope, dir := envVersionScope(mustGetwd())
	version, err := envEnsureToolInstalled(stdout, ui, asdf, spec, scope, dir)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("%s %s is ready", strings.Title(spec.Name), version))); err != nil {
		return err
	}
	return nil
}

func jotEnvRun(stdin io.Reader, stdout io.Writer, args []string) error {
	if len(args) == 0 || isHelpFlag(args[0]) {
		return writeHelp(stdout, "env")
	}
	spec, ok := resolveEnvTool(args[0])
	if !ok {
		return fmt.Errorf("unsupported tool %q", args[0])
	}
	if runtime.GOOS == "windows" {
		if err := envEnsureWindowsToolInstalled(stdout, newTermUI(stdout), spec); err != nil {
			return err
		}
		path, err := envLocateToolExecutable(spec)
		if err != nil {
			return err
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stdout
		return cmd.Run()
	}
	asdf, err := newEnvASDF(stdout)
	if err != nil {
		return err
	}
	ui := newTermUI(stdout)
	scope, dir := envVersionScope(mustGetwd())
	if _, err := envEnsureToolInstalled(stdout, ui, asdf, spec, scope, dir); err != nil {
		return err
	}
	cmdArgs := append([]string{"exec", spec.Executable}, args[1:]...)
	cmd := exec.Command(asdf.path, cmdArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}

func jotEnvSetup(stdin io.Reader, stdout io.Writer, args []string) error {
	if len(args) != 1 || isHelpFlag(args[0]) {
		return writeHelp(stdout, "env")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "web":
		return jotEnvSetupWeb(stdin, stdout)
	case "go", "golang":
		return jotEnvSetupGo(stdout)
	default:
		return fmt.Errorf("unknown setup target %q", args[0])
	}
}

func jotEnvSetupWeb(_ io.Reader, stdout io.Writer) error {
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, ui.header("Web Setup")); err != nil {
		return err
	}
	if err := envEnsureWindowsToolInstalled(stdout, ui, mustResolveEnvTool("git")); err != nil {
		return err
	}
	if err := envEnsureWindowsToolInstalled(stdout, ui, mustResolveEnvTool("python")); err != nil {
		return err
	}
	if err := envEnsureWindowsToolInstalled(stdout, ui, mustResolveEnvTool("node")); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(stdout, ui.success("Web environment is ready")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip("next step: run `jot env run node --version`")); err != nil {
		return err
	}
	return nil
}

func jotEnvSetupGo(stdout io.Writer) error {
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, ui.header("Go Setup")); err != nil {
		return err
	}
	if err := envEnsureWindowsToolInstalled(stdout, ui, mustResolveEnvTool("git")); err != nil {
		return err
	}
	if err := envEnsureWindowsToolInstalled(stdout, ui, mustResolveEnvTool("go")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip("next step: run `jot env run go version` or build your project")); err != nil {
		return err
	}
	return nil
}

func jotEnvRecommend(stdout io.Writer, args []string) error {
	if len(args) != 0 {
		return writeHelp(stdout, "env")
	}
	ui := newTermUI(stdout)
	recommendations := envRecommendations(mustGetwd())
	if len(recommendations) == 0 {
		_, err := fmt.Fprintln(stdout, "no environment suggestions right now")
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.header("Recommendations")); err != nil {
		return err
	}
	for i, item := range recommendations {
		if _, err := fmt.Fprintln(stdout, ui.listItem(i+1, item.Title, item.Reason, "")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout, ui.tip(item.Command)); err != nil {
			return err
		}
	}
	return nil
}

func newEnvASDF(stdout io.Writer) (*envASDF, error) {
	path, err := exec.LookPath("asdf")
	if err == nil {
		return &envASDF{path: path, commandName: "asdf"}, nil
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return nil, errors.New("could not resolve the user home directory for asdf setup")
	}
	if path, ok := envLocateASDFBinary(home); ok {
		return &envASDF{path: path, commandName: "asdf"}, nil
	}
	if _, ok := envLocateASDFInstall(home); ok && stdout != nil {
		ui := newTermUI(stdout)
		_, _ = fmt.Fprintln(stdout, ui.tip("found a legacy ~/.asdf install; preferring the modern asdf binary"))
	}
	if err := envBootstrapASDF(stdout, home); err != nil {
		if installDir, ok := envLocateASDFInstall(home); ok {
			bashPath, bashErr := exec.LookPath("bash")
			if bashErr == nil {
				if stdout != nil {
					ui := newTermUI(stdout)
					_, _ = fmt.Fprintln(stdout, ui.tip("modern asdf bootstrap failed; falling back to the legacy local install"))
				}
				return &envASDF{path: bashPath, dir: installDir, shellInit: true, commandName: "asdf"}, nil
			}
		}
		return nil, err
	}
	if path, ok := envLocateASDFBinary(home); ok {
		return &envASDF{path: path, commandName: "asdf"}, nil
	}
	installDir, ok := envLocateASDFInstall(home)
	if !ok {
		return nil, errors.New("asdf bootstrap completed but the install could not be verified")
	}
	bashPath, bashErr := exec.LookPath("bash")
	if bashErr != nil {
		return nil, errors.New("asdf was installed but bash is not available to initialize it")
	}
	return &envASDF{path: bashPath, dir: installDir, shellInit: true, commandName: "asdf"}, nil
}

func (a *envASDF) output(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := a.command(args...)
	if a != nil && a.shellInit {
		cmd = exec.CommandContext(ctx, a.path, "-lc", envASDFShellScript(a.dir, a.commandName, args...))
	} else {
		cmd = exec.CommandContext(ctx, a.path, args...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("asdf command timed out: %s", strings.Join(args, " "))
		}
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = strings.TrimSpace(stdout.String())
		}
		if text != "" {
			return "", fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return stdout.String(), nil
}

func (a *envASDF) run(stdout io.Writer, args ...string) error {
	cmd := a.command(args...)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}

func (a *envASDF) command(args ...string) *exec.Cmd {
	if a != nil && a.shellInit {
		return exec.Command(a.path, "-lc", envASDFShellScript(a.dir, a.commandName, args...))
	}
	return exec.Command(a.path, args...)
}

func resolveEnvTool(name string) (envToolSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, item := range envToolCatalog {
		if key == item.Name || key == item.Plugin || key == strings.ToLower(item.Executable) {
			return item, true
		}
		for _, alias := range item.Aliases {
			if key == strings.ToLower(strings.TrimSpace(alias)) {
				return item, true
			}
		}
	}
	return envToolSpec{}, false
}

func mustResolveEnvTool(name string) envToolSpec {
	spec, ok := resolveEnvTool(name)
	if !ok {
		panic("unknown env tool: " + name)
	}
	return spec
}

func envEnsureToolInstalled(stdout io.Writer, ui termUI, asdf *envASDF, spec envToolSpec, scope string, dir string) (string, error) {
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("checking %s plugin", spec.Plugin))); err != nil {
		return "", err
	}
	if err := envEnsurePlugin(stdout, ui, asdf, spec); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("resolving the latest stable %s version", spec.Plugin))); err != nil {
		return "", err
	}
	version, err := envLatestStableVersion(asdf, spec)
	if err != nil {
		return "", err
	}
	installed, err := envHasInstalledVersion(asdf, spec, version)
	if err != nil {
		return "", err
	}
	if !installed {
		if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("installing %s %s", spec.Plugin, version))); err != nil {
			return "", err
		}
		if err := asdf.run(stdout, "install", spec.Plugin, version); err != nil {
			return "", err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("%s %s is already installed", spec.Plugin, version))); err != nil {
			return "", err
		}
	}
	if err := envSetVersion(stdout, ui, asdf, spec, version, scope, dir); err != nil {
		return "", err
	}
	if err := asdf.run(stdout, "reshim", spec.Plugin, version); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(stdout, ui.success(fmt.Sprintf("%s is ready", strings.Title(spec.Name)))); err != nil {
		return "", err
	}
	return version, nil
}

func envEnsurePlugin(stdout io.Writer, ui termUI, asdf *envASDF, spec envToolSpec) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is required for asdf plugin operations, and automatic git bootstrap is not implemented yet")
	}
	items, err := asdf.output("plugin", "list")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(items, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), spec.Plugin) {
			return nil
		}
	}
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("adding asdf plugin %s", spec.Plugin))); err != nil {
		return err
	}
	return asdf.run(stdout, "plugin", "add", spec.Plugin)
}

func envLatestStableVersion(asdf *envASDF, spec envToolSpec) (string, error) {
	output, err := asdf.output("latest", spec.Plugin)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", fmt.Errorf("could not resolve a stable version for %s", spec.Plugin)
	}
	version := strings.TrimSpace(fields[0])
	if version == "" {
		return "", fmt.Errorf("could not resolve a stable version for %s", spec.Plugin)
	}
	return version, nil
}

func envHasInstalledVersion(asdf *envASDF, spec envToolSpec, version string) (bool, error) {
	output, err := asdf.output("list", spec.Plugin)
	if err != nil {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "no versions installed") || strings.Contains(text, "not installed") {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if trimmed == version {
			return true, nil
		}
	}
	return false, nil
}

func envSetVersion(stdout io.Writer, ui termUI, asdf *envASDF, spec envToolSpec, version, scope, dir string) error {
	if scope == "local" {
		if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("setting local %s version in %s", spec.Plugin, dir))); err != nil {
			return err
		}
		cmd := asdf.command("local", spec.Plugin, version)
		cmd.Dir = dir
		cmd.Stdout = stdout
		cmd.Stderr = stdout
		return cmd.Run()
	}
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("setting global %s version", spec.Plugin))); err != nil {
		return err
	}
	return asdf.run(stdout, "global", spec.Plugin, version)
}

func envVersionScope(dir string) (string, string) {
	if dir == "" {
		return "global", ""
	}
	markers := []string{
		".tool-versions",
		".git",
		"package.json",
		"go.mod",
		"pyproject.toml",
		"Cargo.toml",
	}
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return "local", dir
		}
	}
	return "global", dir
}

func envRecommendations(dir string) []envRecommendation {
	recommendations := make([]envRecommendation, 0, 6)
	if dir == "" {
		return []envRecommendation{
			{Title: "Git", Command: "jot env install git", Reason: "Git is the base prerequisite for most Windows dev setup flows."},
			{Title: "Go toolchain", Command: "jot env setup go", Reason: "Install Git, asdf, and Go so you can build Go CLIs and services locally."},
			{Title: "Web toolchain", Command: "jot env setup web", Reason: "Install Git, asdf, Python, and Node.js for Windows web work."},
		}
	}
	requiredTools := envProjectToolNeeds(dir)
	order := []string{"git", "go", "python", "node"}
	for _, name := range order {
		if _, ok := requiredTools[name]; !ok {
			continue
		}
		spec := mustResolveEnvTool(name)
		command := "jot env install " + spec.Name
		title := strings.Title(spec.Name) + " runtime"
		reason := requiredTools[name]
		if spec.Name == "go" {
			command = "jot env setup go"
			title = "Go toolchain"
		}
		if spec.Name == "node" {
			command = "jot env setup web"
			title = "Web toolchain"
		}
		if spec.Name == "git" {
			command = "jot env install git"
			title = "Git"
		}
		recommendations = append(recommendations, envRecommendation{
			Title:   title,
			Command: command,
			Reason:  reason,
		})
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations,
			envRecommendation{Title: "Git", Command: "jot env install git", Reason: "Git is the first Windows prerequisite for most dev machines."},
			envRecommendation{Title: "Go toolchain", Command: "jot env setup go", Reason: "Useful for local CLIs, services, and this repo's own build flow."},
			envRecommendation{Title: "Web toolchain", Command: "jot env setup web", Reason: "Install Python and Node.js for Windows web work."},
		)
	}
	return recommendations
}

func envProjectToolNeeds(dir string) map[string]string {
	needs := map[string]string{}
	add := func(name, reason string) {
		if _, ok := needs[name]; !ok {
			needs[name] = reason
		}
	}
	add("git", "Git is required by the current Windows-focused jot env bootstrap flow.")
	if fileExists(filepath.Join(dir, "go.mod")) {
		add("go", "This directory has a Go module and needs Go to build or test.")
	}
	if fileExists(filepath.Join(dir, "package.json")) {
		add("node", "This directory has a package.json and needs Node.js to run common JS project commands.")
	}
	if fileExists(filepath.Join(dir, "pyproject.toml")) || fileExists(filepath.Join(dir, "requirements.txt")) {
		add("python", "This directory looks like a Python project.")
	}
	return needs
}

func envProjectToolNames(dir string) []string {
	needs := envProjectToolNeeds(dir)
	names := make([]string, 0, len(needs))
	for name := range needs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func envLocateASDFInstall(home string) (string, bool) {
	if strings.TrimSpace(home) == "" {
		return "", false
	}
	dir := filepath.Join(home, ".asdf")
	script := filepath.Join(dir, "asdf.sh")
	info, err := os.Stat(script)
	if err != nil || info.IsDir() {
		return "", false
	}
	return dir, true
}

func envLocateASDFBinary(home string) (string, bool) {
	path := envPreferredASDFBinaryPath(home)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func envBootstrapASDF(stdout io.Writer, home string) error {
	ui := newTermUI(stdout)
	if _, err := fmt.Fprintln(stdout, ui.tip("asdf is missing; installing it in user space")); err != nil {
		return err
	}
	if err := envInstallASDFFromRelease(stdout, home); err == nil {
		for _, rcPath := range envShellInitFiles(home) {
			if err := envEnsureShellInitFile(rcPath); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout, ui.success("asdf installed")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout, ui.tip("future shells will load asdf automatically")); err != nil {
			return err
		}
		return nil
	} else if stdout != nil {
		_, _ = fmt.Fprintln(stdout, ui.tip("binary download failed; trying a fallback install path"))
	}
	if err := envInstallASDFFromClone(stdout, home); err != nil {
		return err
	}
	for _, rcPath := range envShellInitFiles(home) {
		if err := envEnsureShellInitFile(rcPath); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout, ui.success("asdf installed")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip("future shells will load asdf automatically")); err != nil {
		return err
	}
	return nil
}

func envInstallASDFFromRelease(_ io.Writer, home string) error {
	binPath := envPreferredASDFBinaryPath(home)
	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		return err
	}
	assetURL, err := envResolveASDFReleaseAssetURL()
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "jot-asdf-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archiveName := filepath.Base(assetURL)
	archivePath := filepath.Join(tmpDir, archiveName)
	if err := envDownloadFile(assetURL, archivePath); err != nil {
		return err
	}
	if err := envExtractASDFArchive(archivePath, binPath); err != nil {
		return err
	}
	if err := os.Chmod(binPath, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return nil
}

func envInstallASDFFromClone(stdout io.Writer, home string) error {
	installDir := filepath.Join(home, ".asdf")
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("asdf download fallback requires git, but git is not available")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		return errors.New("asdf download fallback requires bash, but bash is not available")
	}
	if info, err := os.Stat(installDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("cannot install asdf because %s already exists and is not a directory", installDir)
		}
		return fmt.Errorf("cannot bootstrap asdf automatically because %s already exists but is incomplete", installDir)
	}
	cmd := exec.Command("git", "clone", "--depth", "1", "https://github.com/asdf-vm/asdf.git", installDir)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("asdf bootstrap failed: %w", err)
	}
	return nil
}

func envShellInitFiles(home string) []string {
	shell := strings.ToLower(strings.TrimSpace(os.Getenv("SHELL")))
	files := make([]string, 0, 3)
	add := func(name string) {
		path := filepath.Join(home, name)
		for _, existing := range files {
			if strings.EqualFold(existing, path) {
				return
			}
		}
		files = append(files, path)
	}
	switch {
	case strings.Contains(shell, "zsh"):
		add(".zshrc")
	case strings.Contains(shell, "bash"), runtime.GOOS == "windows":
		add(".bashrc")
		add(".bash_profile")
	default:
		add(".profile")
	}
	if len(files) == 0 {
		add(".profile")
	}
	return files
}

func envEnsureShellInitFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	block := envASDFInitSnippet()
	if strings.Contains(existing, "# >>> jot asdf >>>") {
		return nil
	}
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return os.WriteFile(path, []byte(existing+block), 0o600)
}

func envASDFInitSnippet() string {
	return "\n# >>> jot asdf >>>\n" +
		"export ASDF_DATA_DIR=\"${ASDF_DATA_DIR:-$HOME/.asdf}\"\n" +
		"export PATH=\"$HOME/.jot/bin:$ASDF_DATA_DIR/shims:$PATH\"\n" +
		"[ -s \"$HOME/.asdf/asdf.sh\" ] && . \"$HOME/.asdf/asdf.sh\"\n" +
		"# <<< jot asdf <<<\n"
}

func envASDFShellScript(dir string, commandName string, args ...string) string {
	parts := []string{
		"export ASDF_DIR=" + envShellQuote(filepath.ToSlash(strings.TrimSpace(dir))),
		"export PATH=\"$ASDF_DIR/bin:$ASDF_DIR/shims:$PATH\"",
		". \"$ASDF_DIR/asdf.sh\"",
	}
	call := []string{envShellQuote(strings.TrimSpace(commandName))}
	for _, arg := range args {
		call = append(call, envShellQuote(arg))
	}
	parts = append(parts, strings.Join(call, " "))
	return strings.Join(parts, "; ")
}

func envShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func envPreferredASDFBinaryPath(home string) string {
	name := "asdf"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, ".jot", "bin", name)
}

func envResolveASDFReleaseAssetURL() (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/asdf-vm/asdf/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jot-env")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not resolve the latest asdf release: %s", resp.Status)
	}
	var payload struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	targets := envASDFAssetMatchers()
	for _, matcher := range targets {
		for _, asset := range payload.Assets {
			name := strings.ToLower(strings.TrimSpace(asset.Name))
			if strings.Contains(name, matcher) {
				return strings.TrimSpace(asset.URL), nil
			}
		}
	}
	return "", errors.New("no compatible asdf binary release was found for this platform")
}

func envASDFAssetMatchers() []string {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	pairs := []string{
		osName + "-" + archName,
		osName + "_" + archName,
	}
	if osName == "windows" && archName == "amd64" {
		pairs = append([]string{"windows-amd64.zip", "windows_amd64.zip"}, pairs...)
	}
	return pairs
}

func envEnsureWindowsToolInstalled(stdout io.Writer, ui termUI, spec envToolSpec) error {
	path, err := envLocateToolExecutable(spec)
	if err == nil && path != "" {
		if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("%s is already available", spec.Name))); err != nil {
			return err
		}
		return nil
	}
	packageID, err := envWindowsPackageID(spec)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip(fmt.Sprintf("%s is missing; trying a user-scope install with winget", spec.Name))); err != nil {
		return err
	}
	wingetPath, err := exec.LookPath("winget")
	if err != nil {
		return fmt.Errorf("%s is not installed and winget is not available for automatic setup", spec.Name)
	}
	cmd := exec.Command(wingetPath,
		"install",
		"--id", packageID,
		"--exact",
		"--scope", "user",
		"--accept-package-agreements",
		"--accept-source-agreements",
	)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("automatic %s setup failed: %w", spec.Name, err)
	}
	path, err = envLocateToolExecutable(spec)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.success(strings.Title(spec.Name)+" is ready")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, ui.tip("installed at "+path)); err != nil {
		return err
	}
	return nil
}

func envWindowsPackageID(spec envToolSpec) (string, error) {
	switch spec.Name {
	case "git":
		return "Git.Git", nil
	case "go":
		return "GoLang.Go", nil
	case "node":
		return "OpenJS.NodeJS.LTS", nil
	case "python":
		return "Python.Python.3.12", nil
	default:
		return "", fmt.Errorf("automatic Windows setup is not configured for %s", spec.Name)
	}
}

func envLocateToolExecutable(spec envToolSpec) (string, error) {
	if path, err := exec.LookPath(spec.Executable); err == nil && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("%s is not on PATH", spec.Name)
	}
	for _, candidate := range envWindowsExecutableCandidates(spec) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s is not on PATH and was not found in common install locations", spec.Name)
}

func envWindowsExecutableCandidates(spec envToolSpec) []string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	var out []string
	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		out = append(out, path)
	}
	switch spec.Name {
	case "git":
		add(filepath.Join(localAppData, "Programs", "Git", "cmd", "git.exe"))
		add(filepath.Join(localAppData, "Programs", "Git", "bin", "git.exe"))
		add(filepath.Join(programFiles, "Git", "cmd", "git.exe"))
	case "go":
		add(filepath.Join(programFiles, "Go", "bin", "go.exe"))
		add(filepath.Join(localAppData, "Programs", "Go", "bin", "go.exe"))
	case "node":
		add(filepath.Join(programFiles, "nodejs", "node.exe"))
		add(filepath.Join(localAppData, "Programs", "nodejs", "node.exe"))
	case "python":
		for _, base := range []string{
			filepath.Join(localAppData, "Programs", "Python"),
			filepath.Join(userProfile, "AppData", "Local", "Programs", "Python"),
		} {
			if strings.TrimSpace(base) == "" {
				continue
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				add(filepath.Join(base, entry.Name(), "python.exe"))
			}
		}
	}
	return out
}

func envDownloadFile(url, path string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "jot-env")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func envExtractASDFArchive(archivePath, binPath string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return envExtractASDFZip(archivePath, binPath)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return envExtractASDFTarGz(archivePath, binPath)
	default:
		return fmt.Errorf("unsupported asdf archive format: %s", archivePath)
	}
}

func envExtractASDFZip(archivePath, binPath string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		name := strings.ToLower(filepath.Base(file.Name))
		if name != "asdf" && name != "asdf.exe" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(binPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("asdf archive did not contain the asdf binary")
}

func envExtractASDFTarGz(archivePath, binPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := strings.ToLower(filepath.Base(header.Name))
		if name != "asdf" && name != "asdf.exe" {
			continue
		}
		out, err := os.Create(binPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("asdf archive did not contain the asdf binary")
}

func renderEnvHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot env", "Install Windows dev prerequisites from a clean machine with a task-first flow.")
	writeUsageSection(&b, style, []string{
		"jot env install <tool>",
		"jot env run <tool> [args...]",
		"jot env setup web",
		"jot env setup go",
		"jot env recommend",
	}, []string{
		"`jot env` is currently Windows-only and focused on git, python, go, and node.",
		"`jot env` manages machine-level runtimes and tools, not project npm dependencies.",
		"`jot env` uses native Windows installers for these tools on a clean machine.",
		"`jot env run` installs a missing runtime before executing it.",
	})
	writeExamplesSection(&b, style, []string{
		"jot env install git",
		"jot env install python",
		"jot env install node",
		"jot env install go",
		"jot env setup web",
		"jot env setup go",
		"jot env recommend",
	})
	return b.String()
}
