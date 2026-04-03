package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type BrowserComputerOptions struct {
	BrowserPath         string
	UserDataDir         string
	DebugAddress        string
	RemoteDebuggingPort int
	StartURL            string
	Visible             bool
	Headless            bool
	KeepOpen            bool
}

// BrowserFillAction is a lightweight fill request used by higher-level flows.
type BrowserFillAction struct {
	Label string
	Value string
}

// BrowserActionResult reports the result of one browser action.
type BrowserActionResult struct {
	OK       bool
	Selector string
	Label    string
	Error    string
}

type BrowserRuntime struct {
	opts BrowserComputerOptions

	mu         sync.Mutex
	cmd        *exec.Cmd
	conn       *cdpClient
	currentURL string
	managed    bool
	closed     bool
}

var _ BrowserComputer = (*BrowserRuntime)(nil)

type browserPageState struct {
	URL        string               `json:"url"`
	Title      string               `json:"title"`
	Text       string               `json:"text"`
	HTML       string               `json:"html"`
	Elements   []browserPageElement `json:"elements"`
	CapturedAt time.Time            `json:"capturedAt"`
}

type browserPageElement struct {
	Selector    string   `json:"selector"`
	TagName     string   `json:"tagName"`
	Type        string   `json:"type"`
	Role        string   `json:"role"`
	Label       string   `json:"label"`
	GroupLabel  string   `json:"groupLabel"`
	Context     string   `json:"context"`
	Name        string   `json:"name"`
	Placeholder string   `json:"placeholder"`
	Value       string   `json:"value"`
	Required    bool     `json:"required"`
	Disabled    bool     `json:"disabled"`
	Visible     bool     `json:"visible"`
	Options     []string `json:"options"`
}

type browserVersionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type browserTargetInfo struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cdpClient struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	nextID  int64
	pending map[int64]chan cdpResponse
	pmu     sync.Mutex
	done    chan struct{}
	err     chan error
}

type cdpResponse struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpEvalResponse struct {
	Result struct {
		Value json.RawMessage `json:"value,omitempty"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text string `json:"text"`
	} `json:"exceptionDetails,omitempty"`
}

func NewBrowserComputer(opts BrowserComputerOptions) (*BrowserRuntime, error) {
	if strings.TrimSpace(opts.BrowserPath) == "" {
		exe, err := findBrowserExecutable()
		if err != nil {
			return nil, err
		}
		opts.BrowserPath = exe
	}
	if strings.TrimSpace(opts.UserDataDir) == "" {
		dir, err := defaultBrowserProfileDir()
		if err != nil {
			return nil, err
		}
		opts.UserDataDir = dir
	}
	if opts.RemoteDebuggingPort <= 0 {
		port, err := freeTCPPort()
		if err != nil {
			return nil, err
		}
		opts.RemoteDebuggingPort = port
	}
	if strings.TrimSpace(opts.DebugAddress) != "" {
		return attachBrowserRuntime(opts)
	}
	return launchBrowserRuntime(opts)
}

// NewAssistantBrowserComputer is a compatibility wrapper for the assistant
// runtime. Keeping it separate makes the browser core easy to replace later
// without touching higher-level flow code.
func NewAssistantBrowserComputer(opts BrowserComputerOptions) (*BrowserRuntime, error) {
	return NewBrowserComputer(opts)
}

func launchBrowserRuntime(opts BrowserComputerOptions) (*BrowserRuntime, error) {
	if err := os.MkdirAll(opts.UserDataDir, 0o755); err != nil {
		return nil, err
	}
	args := []string{
		"--remote-debugging-port=" + strconv.Itoa(opts.RemoteDebuggingPort),
		"--user-data-dir=" + opts.UserDataDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	} else if opts.Visible {
		args = append(args, "--new-window")
	}
	if strings.TrimSpace(opts.StartURL) != "" {
		args = append(args, opts.StartURL)
	}
	cmd := exec.Command(opts.BrowserPath, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", opts.RemoteDebuggingPort)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitForBrowserReady(ctx, baseURL); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	wsURL, err := chooseBrowserPage(ctx, baseURL)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	conn, err := dialCDPWebSocket(ctx, wsURL)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	client := newCDPClient(conn)
	if err := client.call(ctx, "Page.enable", nil, nil); err != nil {
		_ = client.close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	if err := client.call(ctx, "Runtime.enable", nil, nil); err != nil {
		_ = client.close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	br := &BrowserRuntime{opts: opts, cmd: cmd, conn: client, managed: true, currentURL: opts.StartURL}
	if strings.TrimSpace(opts.StartURL) != "" {
		if err := br.Open(opts.StartURL); err != nil {
			_ = br.Close()
			return nil, err
		}
	}
	return br, nil
}

func attachBrowserRuntime(opts BrowserComputerOptions) (*BrowserRuntime, error) {
	base := strings.TrimSpace(opts.DebugAddress)
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := waitForBrowserReady(ctx, baseURL); err != nil {
		return nil, err
	}
	wsURL, err := chooseBrowserPage(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	conn, err := dialCDPWebSocket(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	client := newCDPClient(conn)
	if err := client.call(ctx, "Page.enable", nil, nil); err != nil {
		_ = client.close()
		return nil, err
	}
	if err := client.call(ctx, "Runtime.enable", nil, nil); err != nil {
		_ = client.close()
		return nil, err
	}
	return &BrowserRuntime{opts: opts, conn: client}, nil
}

func (b *BrowserRuntime) Open(rawURL string) error {
	if b == nil {
		return errors.New("browser runtime is nil")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errors.New("url is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("browser runtime is closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := b.conn.call(ctx, "Page.navigate", map[string]any{"url": rawURL}, nil); err != nil {
		return err
	}
	if err := b.waitForReady(ctx, 60*time.Second); err != nil {
		return err
	}
	b.currentURL = rawURL
	return nil
}

func (b *BrowserRuntime) Snapshot() (BrowserPageSnapshot, error) {
	if b == nil {
		return BrowserPageSnapshot{}, errors.New("browser runtime is nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return BrowserPageSnapshot{}, errors.New("browser runtime is closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var resp cdpEvalResponse
	if err := b.conn.evaluate(ctx, browserSnapshotExpression(), &resp); err != nil {
		return BrowserPageSnapshot{}, err
	}
	if resp.ExceptionDetails != nil {
		return BrowserPageSnapshot{}, errors.New(resp.ExceptionDetails.Text)
	}
	var state browserPageState
	if len(resp.Result.Value) == 0 {
		return BrowserPageSnapshot{}, errors.New("browser snapshot returned no data")
	}
	if err := json.Unmarshal(resp.Result.Value, &state); err != nil {
		return BrowserPageSnapshot{}, err
	}
	return convertBrowserSnapshot(state), nil
}

func (b *BrowserRuntime) Type(target, value string) error { return b.fillField(target, value) }
func (b *BrowserRuntime) Submit() error                   { return b.Click("Submit") }
func (b *BrowserRuntime) Fill(actions []BrowserFillAction) ([]BrowserActionResult, error) {
	results := make([]BrowserActionResult, 0, len(actions))
	for _, action := range actions {
		if err := b.fillField(action.Label, action.Value); err != nil {
			results = append(results, BrowserActionResult{
				OK:    false,
				Label: action.Label,
				Error: err.Error(),
			})
			return results, err
		}
		results = append(results, BrowserActionResult{
			OK:    true,
			Label: action.Label,
		})
	}
	return results, nil
}
func (b *BrowserRuntime) Select(target, value string) error {
	return b.interact("select", target, value)
}
func (b *BrowserRuntime) Click(target string) error { return b.interact("click", target, "") }

func (b *BrowserRuntime) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.conn != nil {
		_ = b.conn.close()
		b.conn = nil
	}
	if b.managed && !b.opts.KeepOpen && b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_, _ = b.cmd.Process.Wait()
	}
	return nil
}

func (b *BrowserRuntime) URL() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentURL
}

func (b *BrowserRuntime) interact(kind, target, value string) error {
	if b == nil {
		return errors.New("browser runtime is nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("browser runtime is closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var expr string
	switch kind {
	case "fill":
		expr = browserFillExpression(target, value)
	case "select":
		expr = browserSelectExpression(target, value)
	case "click":
		expr = browserClickExpression(target)
	default:
		return fmt.Errorf("unknown browser action %q", kind)
	}
	var resp cdpEvalResponse
	if err := b.conn.evaluate(ctx, expr, &resp); err != nil {
		return err
	}
	if resp.ExceptionDetails != nil {
		return errors.New(resp.ExceptionDetails.Text)
	}
	if kind == "click" {
		_ = b.waitForReady(ctx, 10*time.Second)
	}
	return nil
}

func (b *BrowserRuntime) fillField(target, value string) error {
	return b.interact("fill", target, value)
}

func (b *BrowserRuntime) waitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var resp cdpEvalResponse
		if err := b.conn.evaluate(ctx, `(() => document.readyState || "loading")()`, &resp); err == nil && resp.ExceptionDetails == nil {
			var state string
			if json.Unmarshal(resp.Result.Value, &state) == nil && (state == "complete" || state == "interactive") {
				time.Sleep(250 * time.Millisecond)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("browser page did not become ready in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func convertBrowserSnapshot(in browserPageState) BrowserPageSnapshot {
	out := BrowserPageSnapshot{
		URL:        in.URL,
		Title:      in.Title,
		Text:       in.Text,
		HTML:       in.HTML,
		CapturedAt: in.CapturedAt,
	}
	out.Elements = make([]BrowserPageElement, 0, len(in.Elements))
	for _, el := range in.Elements {
		sel := el.Selector
		if sel == "" {
			switch {
			case el.Name != "":
				sel = `[name="` + cssEscapeAttr(el.Name) + `"]`
			default:
				sel = "input"
			}
		}
		out.Elements = append(out.Elements, BrowserPageElement{
			Selector:    sel,
			Role:        el.Role,
			Label:       el.Label,
			GroupLabel:  el.GroupLabel,
			Context:     el.Context,
			Name:        el.Name,
			Placeholder: el.Placeholder,
			Value:       el.Value,
			AriaLabel:   el.Label,
			Required:    el.Required,
			Disabled:    el.Disabled,
			Visible:     el.Visible,
			Options:     el.Options,
		})
	}
	return out
}

func findBrowserExecutable() (string, error) {
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
			"chrome",
			"msedge",
		} {
			if strings.Contains(candidate, string(os.PathSeparator)) {
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
				continue
			}
			if resolved, err := exec.LookPath(candidate); err == nil {
				return resolved, nil
			}
		}
		return "", errors.New("could not find Chrome or Edge on this machine")
	}
	for _, candidate := range []string{"google-chrome", "chrome", "chromium", "microsoft-edge", "msedge"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("could not find Chrome or Edge on this machine")
}

func defaultBrowserProfileDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "jot", "browser-profile"), nil
}

func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("could not determine a free TCP port")
	}
	return addr.Port, nil
}

func browserFillExpression(target, value string) string {
	tmpl := `(async function() {
		function sleep(ms) { return new Promise(function(resolve) { setTimeout(resolve, ms); }); }
		function textOf(node) { return node ? String(node.innerText || node.textContent || '').replace(/\s+/g, ' ').trim() : ''; }
		function visible(el) {
			if (!el || !el.getBoundingClientRect) return false;
			var s = getComputedStyle(el);
			if (!s || s.visibility === 'hidden' || s.display === 'none' || parseFloat(s.opacity || '1') === 0) return false;
			var r = el.getBoundingClientRect();
			return r.width > 0 && r.height > 0;
		}
		function attr(el, name) { var v = el.getAttribute(name); return v ? String(v).trim() : ''; }
		function labelFor(el) {
			var direct = attr(el, 'aria-label') || attr(el, 'title') || attr(el, 'placeholder');
			if (direct) return direct;
			var ids = attr(el, 'aria-labelledby');
			if (ids) { var parts = []; ids.split(/\s+/).forEach(function(id) { var node = document.getElementById(id); if (node) parts.push(textOf(node)); }); var joined = parts.join(' ').trim(); if (joined) return joined; }
			var id = attr(el, 'id');
			if (id) { var escaped = (window.CSS && CSS.escape) ? CSS.escape(id) : id.replace(/["\\]/g, '\\$&'); var lab = document.querySelector('label[for="' + escaped + '"]'); if (lab) { var t = textOf(lab); if (t) return t; } }
			var wrap = el.closest('label');
			if (wrap) { var wrapped = textOf(wrap); if (wrapped) return wrapped; }
			if (el.name) return String(el.name).trim();
			var inline = textOf(el);
			if (inline) return inline;
			return '';
		}
		function contextText(el) {
			var parts = [];
			var seen = new Set();
			var current = el;
			for (var depth = 0; current && depth < 4; depth++) {
				var sibling = current.previousElementSibling;
				while (sibling) {
					var t = textOf(sibling);
					if (t && t.toLowerCase() !== 'your answer' && !seen.has(t)) {
						parts.push(t);
						seen.add(t);
					}
					sibling = sibling.previousElementSibling;
				}
				current = current.parentElement;
				if (current) {
					var own = textOf(current);
					if (own && own.toLowerCase() !== 'your answer' && !seen.has(own)) {
						parts.push(own);
						seen.add(own);
					}
				}
			}
			return parts.join(' ').trim();
		}
		function score(el) {
			var hay = (labelFor(el) + ' ' + contextText(el) + ' ' + attr(el, 'name') + ' ' + attr(el, 'id') + ' ' + attr(el, 'placeholder') + ' ' + attr(el, 'title') + ' ' + textOf(el)).toLowerCase();
			var target = __TARGET__.toLowerCase().trim();
			var score = 0;
			if (!target) return -1;
			if (hay === target) score += 1000;
			if (hay.indexOf(target) >= 0) score += 500;
			target.split(/[^a-z0-9]+/).forEach(function(part) { if (part.length > 1 && hay.indexOf(part) >= 0) score += 18; });
			if (visible(el)) score += 10;
			if (!el.disabled) score += 10;
			return score;
		}
		function flash(el) {
			var oldOutline = el.style.outline;
			var oldTransition = el.style.transition;
			el.style.transition = 'outline 120ms ease';
			el.style.outline = '2px solid #c4a882';
			return function() {
				el.style.outline = oldOutline;
				el.style.transition = oldTransition;
			};
		}
		async function setValue(el, value) {
			var tag = el.tagName.toLowerCase();
			el.focus();
			el.click();
			await sleep(90);
			if (tag === 'input') {
				var type = (el.type || 'text').toLowerCase();
				if (type === 'checkbox' || type === 'radio' || type === 'button' || type === 'submit' || type === 'reset' || type === 'file') return false;
				Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, '');
				el.dispatchEvent(new Event('input', { bubbles: true }));
				for (var i = 0; i < value.length; i++) {
					Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, value.slice(0, i + 1));
					el.dispatchEvent(new Event('input', { bubbles: true }));
					await sleep(28);
				}
			} else if (tag === 'textarea') {
				Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(el, '');
				el.dispatchEvent(new Event('input', { bubbles: true }));
				for (var j = 0; j < value.length; j++) {
					Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(el, value.slice(0, j + 1));
					el.dispatchEvent(new Event('input', { bubbles: true }));
					await sleep(28);
				}
			} else if ((attr(el, 'role') || '').toLowerCase() === 'textbox') {
				el.focus();
				el.textContent = '';
				for (var k = 0; k < value.length; k++) {
					el.textContent = value.slice(0, k + 1);
					el.dispatchEvent(new Event('input', { bubbles: true }));
					await sleep(28);
				}
			} else if (el.isContentEditable) {
				el.innerText = '';
				for (var n = 0; n < value.length; n++) {
					el.innerText = value.slice(0, n + 1);
					el.dispatchEvent(new Event('input', { bubbles: true }));
					await sleep(28);
				}
			} else {
				return false;
			}
			el.dispatchEvent(new Event('change', { bubbles: true }));
			el.dispatchEvent(new Event('blur', { bubbles: true }));
			return true;
		}
		var candidates = Array.from(document.querySelectorAll('input, textarea, [role="textbox"], [contenteditable="true"], [contenteditable=""], div[contenteditable]'));
		var best = null;
		var bestScore = -1;
		for (var i = 0; i < candidates.length; i++) {
			var el = candidates[i];
			if (!visible(el) || el.disabled) continue;
			var type = (el.type || 'text').toLowerCase();
			if (type === 'button' || type === 'submit' || type === 'reset' || type === 'checkbox' || type === 'radio' || type === 'file') continue;
			var s = score(el);
			if (s > bestScore) { bestScore = s; best = el; }
		}
		if (!best || bestScore < 0) throw new Error('could not find a fillable field for target ' + __TARGET__);
		best.scrollIntoView({ block: 'center', inline: 'center' });
		var restore = flash(best);
		await sleep(160);
		if (!await setValue(best, __VALUE__)) throw new Error('matched element was not fillable for target ' + __TARGET__);
		await sleep(140);
		restore();
		return true;
	})()`
	return strings.NewReplacer("__TARGET__", jsStringLiteral(target), "__VALUE__", jsStringLiteral(value)).Replace(tmpl)
}

func browserSelectExpression(target, value string) string {
	tmpl := `(async function() {
		function sleep(ms) { return new Promise(function(resolve) { setTimeout(resolve, ms); }); }
		function textOf(node) { return node ? String(node.innerText || node.textContent || '').replace(/\s+/g, ' ').trim() : ''; }
		function visible(el) {
			if (!el || !el.getBoundingClientRect) return false;
			var s = getComputedStyle(el);
			if (!s || s.visibility === 'hidden' || s.display === 'none' || parseFloat(s.opacity || '1') === 0) return false;
			var r = el.getBoundingClientRect();
			return r.width > 0 && r.height > 0;
		}
		function attr(el, name) { var v = el.getAttribute(name); return v ? String(v).trim() : ''; }
		function labelFor(el) {
			var direct = attr(el, 'aria-label') || attr(el, 'title') || attr(el, 'placeholder');
			if (direct) return direct;
			var ids = attr(el, 'aria-labelledby');
			if (ids) { var parts = []; ids.split(/\s+/).forEach(function(id) { var node = document.getElementById(id); if (node) parts.push(textOf(node)); }); var joined = parts.join(' ').trim(); if (joined) return joined; }
			var id = attr(el, 'id');
			if (id) { var escaped = (window.CSS && CSS.escape) ? CSS.escape(id) : id.replace(/["\\]/g, '\\$&'); var lab = document.querySelector('label[for="' + escaped + '"]'); if (lab) { var t = textOf(lab); if (t) return t; } }
			var wrap = el.closest('label');
			if (wrap) { var wrapped = textOf(wrap); if (wrapped) return wrapped; }
			if (el.name) return String(el.name).trim();
			return '';
		}
		function score(el) {
			var hay = (labelFor(el) + ' ' + attr(el, 'name') + ' ' + attr(el, 'id') + ' ' + attr(el, 'placeholder') + ' ' + attr(el, 'title') + ' ' + textOf(el)).toLowerCase();
			var target = __TARGET__.toLowerCase().trim();
			var score = 0;
			if (!target) return -1;
			if (hay === target) score += 1000;
			if (hay.indexOf(target) >= 0) score += 500;
			target.split(/[^a-z0-9]+/).forEach(function(part) { if (part.length > 1 && hay.indexOf(part) >= 0) score += 18; });
			if (visible(el)) score += 10;
			if (!el.disabled) score += 10;
			return score;
		}
		function flash(el) {
			var oldOutline = el.style.outline;
			el.style.outline = '2px solid #c4a882';
			return function() { el.style.outline = oldOutline; };
		}
		var selects = Array.from(document.querySelectorAll('select'));
		var best = null;
		var bestScore = -1;
		for (var i = 0; i < selects.length; i++) {
			var el = selects[i];
			if (!visible(el) || el.disabled) continue;
			var s = score(el);
			if (s > bestScore) { bestScore = s; best = el; }
		}
		if (!best || bestScore < 0) throw new Error('could not find a select field for target ' + __TARGET__);
		best.scrollIntoView({ block: 'center', inline: 'center' });
		var restore = flash(best);
		best.focus();
		best.click();
		await sleep(120);
		var wanted = __VALUE__.toLowerCase().trim();
		var options = Array.from(best.options || []);
		var chosen = null;
		for (var j = 0; j < options.length; j++) {
			var opt = options[j];
			var hay = (String(opt.text || '') + ' ' + String(opt.value || '')).toLowerCase();
			if (!wanted || hay === wanted || hay.indexOf(wanted) >= 0) { chosen = opt; break; }
		}
		if (!chosen) throw new Error('no select option matched value ' + __VALUE__);
		best.value = chosen.value;
		best.dispatchEvent(new Event('input', { bubbles: true }));
		best.dispatchEvent(new Event('change', { bubbles: true }));
		await sleep(120);
		restore();
		return true;
	})()`
	return strings.NewReplacer("__TARGET__", jsStringLiteral(target), "__VALUE__", jsStringLiteral(value)).Replace(tmpl)
}

func browserClickExpression(target string) string {
	tmpl := `(async function() {
		function sleep(ms) { return new Promise(function(resolve) { setTimeout(resolve, ms); }); }
		function textOf(node) { return node ? String(node.innerText || node.textContent || '').replace(/\s+/g, ' ').trim() : ''; }
		function visible(el) {
			if (!el || !el.getBoundingClientRect) return false;
			var s = getComputedStyle(el);
			if (!s || s.visibility === 'hidden' || s.display === 'none' || parseFloat(s.opacity || '1') === 0) return false;
			var r = el.getBoundingClientRect();
			return r.width > 0 && r.height > 0;
		}
		function attr(el, name) { var v = el.getAttribute(name); return v ? String(v).trim() : ''; }
		function labelFor(el) {
			var direct = attr(el, 'aria-label') || attr(el, 'title') || attr(el, 'placeholder');
			if (direct) return direct;
			var ids = attr(el, 'aria-labelledby');
			if (ids) { var parts = []; ids.split(/\s+/).forEach(function(id) { var node = document.getElementById(id); if (node) parts.push(textOf(node)); }); var joined = parts.join(' ').trim(); if (joined) return joined; }
			var id = attr(el, 'id');
			if (id) { var escaped = (window.CSS && CSS.escape) ? CSS.escape(id) : id.replace(/["\\]/g, '\\$&'); var lab = document.querySelector('label[for="' + escaped + '"]'); if (lab) { var t = textOf(lab); if (t) return t; } }
			var wrap = el.closest('label');
			if (wrap) { var wrapped = textOf(wrap); if (wrapped) return wrapped; }
			if (el.name) return String(el.name).trim();
			return '';
		}
		function score(el) {
			var hay = (labelFor(el) + ' ' + attr(el, 'name') + ' ' + attr(el, 'id') + ' ' + attr(el, 'placeholder') + ' ' + attr(el, 'title') + ' ' + textOf(el)).toLowerCase();
			var target = __TARGET__.toLowerCase().trim();
			var score = 0;
			if (!target) return -1;
			if (hay === target) score += 1000;
			if (hay.indexOf(target) >= 0) score += 500;
			if (hay.startsWith(target)) score += 120;
			target.split(/[^a-z0-9]+/).forEach(function(part) { if (part.length > 1 && hay.indexOf(part) >= 0) score += 18; });
			if (visible(el)) score += 10;
			if (!el.disabled) score += 10;
			var role = (attr(el, 'role') || '').toLowerCase();
			if (role === 'radio' || role === 'checkbox') score += 50;
			return score;
		}
		function flash(el) {
			var oldOutline = el.style.outline;
			el.style.outline = '2px solid #c4a882';
			return function() { el.style.outline = oldOutline; };
		}
		var candidates = Array.from(document.querySelectorAll('button, a, [role="button"], [role="radio"], [role="checkbox"], input[type="button"], input[type="submit"], input[type="reset"], label, [tabindex]'));
		var best = null;
		var bestScore = -1;
		for (var i = 0; i < candidates.length; i++) {
			var el = candidates[i];
			if (!visible(el) || el.disabled) continue;
			var s = score(el);
			if (s > bestScore) { bestScore = s; best = el; }
		}
		if (!best || bestScore < 0) throw new Error('could not find a clickable element for target ' + __TARGET__);
		best.scrollIntoView({ block: 'center', inline: 'center' });
		var restore = flash(best);
		best.focus();
		await sleep(150);
		best.click();
		await sleep(160);
		restore();
		return true;
	})()`
	return strings.NewReplacer("__TARGET__", jsStringLiteral(target)).Replace(tmpl)
}

func jsStringLiteral(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func cssEscapeAttr(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func waitForBrowserReady(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/json/version", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return errors.New("browser remote debugging endpoint did not become ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func chooseBrowserPage(ctx context.Context, baseURL string) (string, error) {
	var targets []browserTargetInfo
	if err := fetchJSON(ctx, strings.TrimRight(baseURL, "/")+"/json/list", &targets); err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketDebuggerURL != "" {
			return target.WebSocketDebuggerURL, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(baseURL, "/")+"/json/new?about:blank", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var created browserTargetInfo
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	if created.WebSocketDebuggerURL == "" {
		return "", errors.New("browser did not expose a page websocket")
	}
	return created.WebSocketDebuggerURL, nil
}

func fetchJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if len(body) == 0 {
			return fmt.Errorf("unexpected browser endpoint status %s", resp.Status)
		}
		return fmt.Errorf("unexpected browser endpoint status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func dialCDPWebSocket(ctx context.Context, rawURL string) (net.Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		_ = conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	var req bytes.Buffer
	req.WriteString("GET " + path + " HTTP/1.1\r\n")
	req.WriteString("Host: " + u.Host + "\r\n")
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	req.WriteString("Sec-WebSocket-Key: " + key + "\r\n")
	req.WriteString("Sec-WebSocket-Version: 13\r\n\r\n")
	if _, err := conn.Write(req.Bytes()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != websocketAcceptValue(key) {
		_ = conn.Close()
		return nil, errors.New("websocket accept mismatch")
	}
	return conn, nil
}

func websocketAcceptValue(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func newCDPClient(conn net.Conn) *cdpClient {
	c := &cdpClient{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		pending: map[int64]chan cdpResponse{},
		done:    make(chan struct{}),
		err:     make(chan error, 1),
	}
	go c.readLoop()
	return c
}

func (c *cdpClient) close() error {
	if c == nil {
		return nil
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *cdpClient) call(ctx context.Context, method string, params any, out any) error {
	id := atomic.AddInt64(&c.nextID, 1)
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	ch := make(chan cdpResponse, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	if err := c.writeFrame(websocketTextFrame, payload); err != nil {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return errors.New("browser websocket closed")
		}
		if resp.Error != nil {
			return fmt.Errorf("cdp %s failed: %s", method, resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case err := <-c.err:
		if err == nil {
			err = errors.New("browser websocket closed")
		}
		return err
	case <-ctx.Done():
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return ctx.Err()
	}
}

func (c *cdpClient) evaluate(ctx context.Context, expression string, out any) error {
	params := map[string]any{
		"expression":            expression,
		"returnByValue":         true,
		"awaitPromise":          true,
		"includeCommandLineAPI": true,
		"userGesture":           true,
	}
	return c.call(ctx, "Runtime.evaluate", params, out)
}

func (c *cdpClient) readLoop() {
	defer func() {
		c.pmu.Lock()
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		c.pmu.Unlock()
		select {
		case c.err <- errors.New("browser websocket closed"):
		default:
		}
		_ = c.close()
	}()
	for {
		opcode, payload, err := readWebSocketFrame(c.reader)
		if err != nil {
			select {
			case c.err <- err:
			default:
			}
			return
		}
		switch opcode {
		case websocketTextFrame:
			var msg cdpResponse
			if err := json.Unmarshal(payload, &msg); err != nil {
				continue
			}
			if msg.ID <= 0 {
				continue
			}
			c.pmu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.pmu.Unlock()
			if ch != nil {
				ch <- msg
				close(ch)
			}
		case websocketPingFrame:
			_ = writeWebSocketFrame(c.conn, &c.writeMu, websocketPongFrame, payload)
		case websocketCloseFrame:
			return
		}
	}
}

const (
	websocketContinuationFrame = 0x0
	websocketTextFrame         = 0x1
	websocketBinaryFrame       = 0x2
	websocketCloseFrame        = 0x8
	websocketPingFrame         = 0x9
	websocketPongFrame         = 0xA
)

func (c *cdpClient) writeFrame(opcode byte, payload []byte) error {
	return writeWebSocketFrame(c.conn, &c.writeMu, opcode, payload)
}

func writeWebSocketFrame(conn net.Conn, mu *sync.Mutex, opcode byte, payload []byte) error {
	mu.Lock()
	defer mu.Unlock()
	var header [14]byte
	header[0] = 0x80 | (opcode & 0x0F)
	maskKey := [4]byte{}
	if _, err := rand.Read(maskKey[:]); err != nil {
		return err
	}
	length := len(payload)
	header[1] = 0x80
	n := 2
	switch {
	case length < 126:
		header[1] |= byte(length)
	case length <= 0xFFFF:
		header[1] |= 126
		header[2] = byte(length >> 8)
		header[3] = byte(length)
		n = 4
	default:
		header[1] |= 127
		for i := 0; i < 8; i++ {
			header[2+i] = byte(uint64(length) >> uint(56-8*i))
		}
		n = 10
	}
	copy(header[n:], maskKey[:])
	n += 4
	if _, err := conn.Write(header[:n]); err != nil {
		return err
	}
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ maskKey[i%4]
	}
	if len(masked) > 0 {
		_, err := conn.Write(masked)
		return err
	}
	return nil
}

func readWebSocketFrame(br *bufio.Reader) (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return 0, nil, err
	}
	fin := hdr[0]&0x80 != 0
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(ext[0])<<8 | uint64(ext[1])
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		for _, b := range ext[:] {
			length = (length << 8) | uint64(b)
		}
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(br, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	if !fin && opcode == websocketContinuationFrame {
	}
	return opcode, payload, nil
}

func browserSnapshotExpression() string {
	return `(function() {
		function textOf(node) { return node ? String(node.innerText || node.textContent || '').replace(/\s+/g, ' ').trim() : ''; }
		function visible(el) {
			if (!el || !el.getBoundingClientRect) return false;
			var s = getComputedStyle(el);
			if (!s || s.visibility === 'hidden' || s.display === 'none' || parseFloat(s.opacity || '1') === 0) return false;
			var r = el.getBoundingClientRect();
			return r.width > 0 && r.height > 0;
		}
		function attr(el, name) { var v = el.getAttribute(name); return v ? String(v).trim() : ''; }
		function labelFor(el) {
			var direct = attr(el, 'aria-label') || attr(el, 'title') || attr(el, 'placeholder');
			if (direct) return direct;
			var ids = attr(el, 'aria-labelledby');
			if (ids) { var parts = []; ids.split(/\s+/).forEach(function(id) { var node = document.getElementById(id); if (node) parts.push(textOf(node)); }); var joined = parts.join(' ').trim(); if (joined) return joined; }
			var id = attr(el, 'id');
			if (id) { var escaped = (window.CSS && CSS.escape) ? CSS.escape(id) : id.replace(/["\\]/g, '\\$&'); var lab = document.querySelector('label[for="' + escaped + '"]'); if (lab) { var t = textOf(lab); if (t) return t; } }
			var wrap = el.closest('label');
			if (wrap) { var wrapped = textOf(wrap); if (wrapped) return wrapped; }
			if (el.name) return String(el.name).trim();
			return '';
		}
		function contextText(el) {
			var parts = [];
			var seen = new Set();
			var current = el;
			for (var depth = 0; current && depth < 5; depth++) {
				var parent = current.parentElement;
				if (!parent) break;
				var children = Array.from(parent.children || []);
				for (var i = 0; i < children.length; i++) {
					var child = children[i];
					if (child === current || child.contains(current)) continue;
					if (child.querySelector && child.querySelector('input, textarea, select, button, [role="radio"], [role="checkbox"], [role="textbox"], [role="button"]')) continue;
					var t = textOf(child);
					if (t && t.toLowerCase() !== 'your answer' && !seen.has(t)) {
						parts.push(t);
						seen.add(t);
					}
				}
				current = parent;
			}
			return parts.join(' | ').trim();
		}
		function groupLabelFor(el) {
			var current = el;
			for (var depth = 0; current && depth < 6; depth++) {
				current = current.parentElement;
				if (!current) break;
				var heads = Array.from(current.querySelectorAll('[role="heading"], legend, h1, h2, h3, h4, h5, h6')).filter(visible).map(textOf).filter(Boolean);
				if (heads.length) return heads[0];
				var siblingText = contextText(current);
				if (siblingText) {
					var first = siblingText.split('|')[0].trim();
					if (first && first.toLowerCase() !== 'your answer') return first;
				}
			}
			return '';
		}
		function selectorFor(el) { if (el.id) return '#' + el.id.replace(/(["\\:.#[\] ])/g, '\\$1'); if (el.name) return el.tagName.toLowerCase() + '[name="' + String(el.name).replace(/["\\]/g, '\\$&') + '"]'; return el.tagName.toLowerCase(); }
		var nodes = Array.from(document.querySelectorAll('input, textarea, select, [role="textbox"], [role="radio"], [role="checkbox"], [role="radiogroup"], [role="group"], [contenteditable="true"], [contenteditable=""], div[contenteditable]'));
		var items = [];
		for (var i = 0; i < nodes.length; i++) {
			var el = nodes[i];
			if (!visible(el)) continue;
			var tag = el.tagName.toLowerCase();
			var role = attr(el, 'role') || (tag === 'select' ? 'select' : tag === 'textarea' ? 'textbox' : tag === 'input' ? 'input' : '');
			items.push({
				selector: selectorFor(el),
				tagName: tag,
				type: String(el.type || ''),
				role: role,
				label: labelFor(el),
				groupLabel: groupLabelFor(el),
				context: contextText(el),
				name: attr(el, 'name'),
				placeholder: attr(el, 'placeholder'),
				value: ('value' in el) ? String(el.value || '') : '',
				required: !!el.required,
				disabled: !!el.disabled,
				visible: true,
				options: tag === 'select' ? Array.from(el.options || []).map(function(opt) { return String(opt.text || opt.value || '').trim(); }) : []
			});
		}
		return { url: location.href || '', title: document.title || '', text: document.body ? String(document.body.innerText || '').trim() : '', html: document.documentElement ? String(document.documentElement.outerHTML || '') : '', elements: items, capturedAt: new Date().toISOString() };
	})()`
}
