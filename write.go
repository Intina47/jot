package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

func jotWrite(w io.Writer, args []string) error {
	if len(args) == 0 || (len(args) == 1 && isHelpFlag(args[0])) {
		return writeHelp(w, "write")
	}
	if len(args) > 1 {
		return fmt.Errorf("jot write takes one file - try: jot write <file.md>")
	}
	path := strings.TrimSpace(args[0])
	if path == "" {
		return fmt.Errorf("file path must be provided")
	}
	var initial string
	if data, err := os.ReadFile(path); err == nil {
		initial = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("jot write requires an interactive terminal")
	}
	saved, err := runInlineEditor(path, initial)
	if err != nil {
		return err
	}
	if saved != nil {
		if err := os.WriteFile(path, []byte(*saved), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(w, fg(wColSaved)+"  saved"+wrst+" -> "+path)
	} else {
		fmt.Fprintln(w, fg(wColMuted)+"  quit without saving"+wrst)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Palette — warm, minimal, Notion-adjacent
// ---------------------------------------------------------------------------
const (
	wColBg      = "#0f0f0d"
	wColBarBg   = "#161614"
	wColCurBg   = "#1a1a17"
	wColAccent  = "#c4a882"
	wColMuted   = "#44443c"
	wColSubtle  = "#585850"
	wColDimMrk  = "#252521"
	wColText    = "#d4d0c6"
	wColH1      = "#e8c97a"
	wColH2      = "#c4a882"
	wColH3      = "#9a7e5e"
	wColCode    = "#5aaf59"
	wColBold    = "#ece6d6"
	wColItalic  = "#968676"
	wColBullet  = "#b89a72"
	wColSaved   = "#5aaf59"
	wColUnsaved = "#d4896a"
	wrst        = "\x1b[0m"
)

func fg(h string) string { return "\x1b[38;2;" + wRGB(h) + "m" }
func bg(h string) string { return "\x1b[48;2;" + wRGB(h) + "m" }
func wBold() string      { return "\x1b[1m" }
func wItal() string      { return "\x1b[3m" }

func wRGB(h string) string {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return "180;180;180"
	}
	x := func(s string) int {
		n := 0
		for _, c := range s {
			n <<= 4
			switch {
			case c >= '0' && c <= '9':
				n |= int(c - '0')
			case c >= 'a' && c <= 'f':
				n |= int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				n |= int(c-'A') + 10
			}
		}
		return n
	}
	return fmt.Sprintf("%d;%d;%d", x(h[0:2]), x(h[2:4]), x(h[4:6]))
}

// ---------------------------------------------------------------------------
// ANSI helpers
// ---------------------------------------------------------------------------

func wStripANSI(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); {
		if r[i] == '\x1b' && i+1 < len(r) && r[i+1] == '[' {
			i += 2
			for i < len(r) && r[i] != 'm' && r[i] != 'H' && r[i] != 'K' && r[i] != 'J' {
				i++
			}
			i++
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String()
}

func wVisLen(s string) int { return utf8.RuneCountInString(wStripANSI(s)) }

func wTruncANSI(s string, max int) string {
	if max <= 0 {
		return wrst
	}
	var b strings.Builder
	r := []rune(s)
	i, count := 0, 0
	for i < len(r) && count < max {
		if r[i] == '\x1b' && i+1 < len(r) && r[i+1] == '[' {
			b.WriteRune(r[i])
			i++
			for i < len(r) {
				b.WriteRune(r[i])
				if r[i] == 'm' || r[i] == 'H' || r[i] == 'K' {
					i++
					break
				}
				i++
			}
			continue
		}
		b.WriteRune(r[i])
		i++
		count++
	}
	b.WriteString(wrst)
	return b.String()
}

// wRow pads/truncates content to exactly `width` visible cols on bgCol background
func wRow(content, bgCol string, width int) string {
	v := wVisLen(content)
	if v >= width {
		return wTruncANSI(content, width)
	}
	return content + bg(bgCol) + strings.Repeat(" ", width-v) + wrst
}

// ---------------------------------------------------------------------------
// Editor
// ---------------------------------------------------------------------------

type wEditor struct {
	path    string
	lines   []string
	cx, cy  int
	scrollY int
	saved   bool
	W, H    int
	visH    int
}

func wNewEditor(path, content string) *wEditor {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	W, H, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		W, H = 100, 30
	}
	// layout rows: header(1) + visH + status(1) = visH+2
	visH := H - 2
	if visH < 3 {
		visH = 3
	}
	return &wEditor{path: path, lines: lines, saved: true, W: W, H: H, visH: visH}
}

func (e *wEditor) resize() {
	W, H, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	e.W, e.H = W, H
	e.visH = H - 2
	if e.visH < 3 {
		e.visH = 3
	}
}

func (e *wEditor) gutW() int {
	switch {
	case len(e.lines) >= 1000:
		return 6
	case len(e.lines) >= 100:
		return 5
	default:
		return 4
	}
}

// contentW: text columns = W minus gutter minus left padding minus right padding
func (e *wEditor) contentW() int {
	// gutter(gutW) + "  " padding left + text + "  " padding right
	w := e.W - e.gutW() - 4
	if w < 8 {
		w = 8
	}
	return w
}

func (e *wEditor) line() string {
	if e.cy >= len(e.lines) {
		return ""
	}
	return e.lines[e.cy]
}
func (e *wEditor) lineLen() int { return utf8.RuneCountInString(e.line()) }
func (e *wEditor) clampCx() {
	if e.cx > e.lineLen() {
		e.cx = e.lineLen()
	}
}

func (e *wEditor) scroll() {
	if e.cy < e.scrollY {
		e.scrollY = e.cy
	}
	if e.cy >= e.scrollY+e.visH {
		e.scrollY = e.cy - e.visH + 1
	}
}

// ---------------------------------------------------------------------------
// Editing
// ---------------------------------------------------------------------------

func (e *wEditor) insert(r rune) {
	line := []rune(e.line())
	cx := wMin(e.cx, len(line))
	e.lines[e.cy] = string(append(line[:cx:cx], append([]rune{r}, line[cx:]...)...))
	e.cx++
	e.saved = false
}

func (e *wEditor) newline() {
	line := []rune(e.line())
	cx := wMin(e.cx, len(line))
	before, after := string(line[:cx]), string(line[cx:])
	prefix := wListPrefix(before)
	e.lines[e.cy] = before
	rest := append([]string{prefix + after}, e.lines[e.cy+1:]...)
	e.lines = append(e.lines[:e.cy+1], rest...)
	e.cy++
	e.cx = utf8.RuneCountInString(prefix)
	e.saved = false
}

func wListPrefix(s string) string {
	t := strings.TrimLeft(s, " \t")
	indent := s[:len(s)-len(t)]
	if (strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")) && strings.TrimSpace(t[2:]) != "" {
		return indent + t[:2]
	}
	return ""
}

func (e *wEditor) backspace() {
	if e.cx == 0 {
		if e.cy == 0 {
			return
		}
		prev := e.lines[e.cy-1]
		e.cx = utf8.RuneCountInString(prev)
		e.lines[e.cy-1] = prev + e.lines[e.cy]
		e.lines = append(e.lines[:e.cy], e.lines[e.cy+1:]...)
		e.cy--
	} else {
		r := []rune(e.line())
		cx := wMin(e.cx, len(r))
		e.lines[e.cy] = string(append(r[:cx-1:cx-1], r[cx:]...))
		e.cx--
	}
	e.saved = false
}

func (e *wEditor) deleteForward() {
	r := []rune(e.line())
	if e.cx < len(r) {
		e.lines[e.cy] = string(append(r[:e.cx:e.cx], r[e.cx+1:]...))
		e.saved = false
	} else if e.cy < len(e.lines)-1 {
		e.lines[e.cy] = e.line() + e.lines[e.cy+1]
		e.lines = append(e.lines[:e.cy+1], e.lines[e.cy+2:]...)
		e.saved = false
	}
}

func (e *wEditor) insertHeading(level int) {
	prefix := strings.Repeat("#", level) + " "
	stripped := strings.TrimLeft(e.line(), "# ")
	e.lines[e.cy] = prefix + stripped
	e.cx = utf8.RuneCountInString(e.lines[e.cy])
	e.saved = false
}

func (e *wEditor) wrapWord(open, close string) {
	r := []rune(e.line())
	cx := wMin(e.cx, len(r))
	e.lines[e.cy] = string(r[:cx]) + open + close + string(r[cx:])
	e.cx = cx + utf8.RuneCountInString(open)
	e.saved = false
}

func (e *wEditor) insertBelow(text string) {
	rest := append([]string{text}, e.lines[e.cy+1:]...)
	e.lines = append(e.lines[:e.cy+1], rest...)
	e.cy++
	e.cx = 0
	e.saved = false
}

func (e *wEditor) prependLine(prefix string) {
	if !strings.HasPrefix(e.line(), prefix) {
		e.lines[e.cy] = prefix + e.line()
		e.cx += utf8.RuneCountInString(prefix)
		e.saved = false
	}
}

func wMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Rendering — no box, Notion-style
// ---------------------------------------------------------------------------

func (e *wEditor) render() {
	var sb strings.Builder
	W := e.W
	gw := e.gutW()
	cw := e.contentW()

	// ── Header ────────────────────────────────────────────────────────────
	// Minimal: "jot  filename  ● unsaved        ctrl+s  ctrl+q"
	fname := filepath.Base(e.path)
	var saveMark string
	if e.saved {
		saveMark = fg(wColSaved) + "✓" + wrst + bg(wColBarBg) + fg(wColMuted) + " saved"
	} else {
		saveMark = fg(wColUnsaved) + "●" + wrst + bg(wColBarBg) + fg(wColMuted) + " unsaved"
	}

	leftH := bg(wColBarBg) + "  " +
		fg(wColMuted) + "jot" +
		fg(wColSubtle) + "  ·  " +
		fg(wColAccent) + wBold() + fname + wrst + bg(wColBarBg) +
		"  " + saveMark

	rightH := bg(wColBarBg) + fg(wColMuted) + "ctrl+s  ctrl+q  " + wrst

	lw := wVisLen(leftH)
	rw := wVisLen(rightH)
	gap := W - lw - rw
	if gap < 1 {
		// Narrow: just show save state on right, truncate left
		gap = 1
		leftH = wTruncANSI(leftH, W-rw-1)
	}
	headerRow := leftH + bg(wColBarBg) + strings.Repeat(" ", gap) + rightH
	sb.WriteString(wRow(headerRow, wColBarBg, W) + "\r\n")

	// ── Text rows — no border, Notion-style ───────────────────────────────
	// Each row: [gutter][  ][content padded to cw][  ]
	// No box lines. Just clean background fill.

	for rowIdx := 0; rowIdx < e.visH; rowIdx++ {
		lineIdx := e.scrollY + rowIdx
		isCur := lineIdx == e.cy
		lineBg := wColBg
		if isCur {
			lineBg = wColCurBg
		}

		var rowBuf strings.Builder

		// Gutter — subtle line numbers flush right
		if lineIdx < len(e.lines) {
			numStr := fmt.Sprintf("%*d", gw-1, lineIdx+1)
			if isCur {
				rowBuf.WriteString(bg(lineBg) + fg(wColAccent) + numStr + " " + wrst)
			} else {
				rowBuf.WriteString(bg(wColBg) + fg(wColMuted) + numStr + " " + wrst)
			}
		} else {
			// Empty line marker — very dim, just a dot
			rowBuf.WriteString(bg(wColBg) + fg(wColDimMrk) + fmt.Sprintf("%*s ", gw-1, "·") + wrst)
		}

		// Left padding (2 spaces) — creates the comfortable Notion-like margin
		rowBuf.WriteString(bg(lineBg) + "  ")

		// Content
		if lineIdx < len(e.lines) {
			highlighted := wHighlightLine(e.lines[lineIdx], lineBg)
			rawLen := wVisLen(highlighted)
			if rawLen > cw {
				// Long line — truncate with › indicator, no hard wrap
				highlighted = wTruncANSI(highlighted, cw-1) +
					wrst + bg(wColBarBg) + fg(wColMuted) + "›"
				rawLen = cw
			}
			rowBuf.WriteString(highlighted)
			if rawLen < cw {
				rowBuf.WriteString(bg(lineBg) + strings.Repeat(" ", cw-rawLen))
			}
		} else {
			rowBuf.WriteString(bg(wColBg) + strings.Repeat(" ", cw))
		}

		// Right padding
		rowBuf.WriteString(bg(lineBg) + "  " + wrst)

		sb.WriteString(wRow(rowBuf.String(), wColBg, W) + "\r\n")
	}

	// ── Status bar ────────────────────────────────────────────────────────
	words := len(strings.Fields(strings.Join(e.lines, "\n")))
	pct := 0
	if len(e.lines) > 1 {
		pct = (e.cy + 1) * 100 / len(e.lines)
	}
	sLeft := bg(wColBarBg) + fg(wColMuted) +
		fmt.Sprintf("  %d words · %d lines", words, len(e.lines))
	sRight := bg(wColBarBg) + fg(wColMuted) +
		fmt.Sprintf("  %d%%  ln %d  col %d  ", pct, e.cy+1, e.cx+1)
	slw := wVisLen(sLeft)
	srw := wVisLen(sRight)
	sGap := W - slw - srw
	if sGap < 0 {
		sGap = 0
	}
	statusRow := sLeft + bg(wColBarBg) + strings.Repeat(" ", sGap) + sRight
	sb.WriteString(wRow(statusRow, wColBarBg, W) + "\r\n")

	fmt.Print(sb.String())

	// After printing, cursor is at the end of the status bar (bottom of render).
	// Move up to the correct text row, then to the correct column.
	// Rows from bottom: status(1) + rows below cursor in visible area
	rowsFromBottom := 1 + (e.visH - 1 - (e.cy - e.scrollY))
	fmt.Printf("\x1b[%dA", rowsFromBottom) // move up to cursor line
	// Now move to correct column using carriage return + forward
	curC := gw + 2 + e.cx // 0-indexed
	fmt.Print("\r")       // go to column 0
	if curC > 0 {
		fmt.Printf("\x1b[%dC", curC) // move right curC columns
	}
}

// ---------------------------------------------------------------------------
// Syntax highlighting
// ---------------------------------------------------------------------------

func wHighlightLine(line, bgCol string) string {
	b := bg(bgCol)
	switch {
	case strings.HasPrefix(line, "# "):
		return b + fg(wColH1) + wBold() + line + wrst
	case strings.HasPrefix(line, "## "):
		return b + fg(wColH2) + wBold() + line + wrst
	case strings.HasPrefix(line, "### "):
		return b + fg(wColH3) + wBold() + line + wrst
	case strings.HasPrefix(line, "####"):
		return b + fg(wColH3) + line + wrst
	case strings.HasPrefix(line, "```"):
		return b + fg(wColCode) + line + wrst
	case strings.HasPrefix(line, "> "):
		return b + fg(wColItalic) + wItal() + line + wrst
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
		r := []rune(line)
		return b + fg(wColBullet) + string(r[:2]) + fg(wColText) + string(r[2:]) + wrst
	case strings.TrimSpace(line) == "---":
		return b + fg(wColMuted) + line + wrst
	default:
		return b + wHighlightInline(line, bgCol)
	}
}

func wHighlightInline(s, bgCol string) string {
	base := bg(bgCol) + fg(wColText)
	var b strings.Builder
	b.WriteString(base)
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, "**"):
			if end := strings.Index(s[2:], "**"); end >= 0 {
				b.WriteString(fg(wColBold) + wBold() + "**" + s[2:2+end] + "**" + wrst + base)
				s = s[2+end+2:]
				continue
			}
		case strings.HasPrefix(s, "_") && len(s) > 2:
			if end := strings.Index(s[1:], "_"); end > 0 {
				b.WriteString(fg(wColItalic) + wItal() + "_" + s[1:1+end] + "_" + wrst + base)
				s = s[1+end+1:]
				continue
			}
		case strings.HasPrefix(s, "`"):
			if end := strings.Index(s[1:], "`"); end >= 0 {
				b.WriteString(fg(wColCode) + "`" + s[1:1+end] + "`" + wrst + base)
				s = s[1+end+1:]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s)
		b.WriteRune(r)
		s = s[size:]
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Main loop with resize detection
// ---------------------------------------------------------------------------

func runInlineEditor(path, initial string) (*string, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("error making raw: %w", err)
	}
	defer term.Restore(fd, old)

	e := wNewEditor(path, initial)
	// Reserve exactly visH+2 rows (header + text + status)
	totalRows := e.visH + 2
	for i := 0; i < totalRows; i++ {
		fmt.Print("\r\n")
	}
	fmt.Printf("\x1b[%dA", totalRows)

	// Track last known size for resize detection
	lastW, lastH := e.W, e.H

	redraw := func() {
		fmt.Print("\x1b[?25l")
		// Move up exactly the number of rows we rendered last time (visH+2),
		// not totalRows (which is full terminal height). This keeps the
		// header from drifting as the cursor moves.
		fmt.Printf("\x1b[%dA", e.visH+2)
		fmt.Print("\x1b[0J") // clear from here down
		e.scroll()
		e.render()
		fmt.Print("\x1b[?25h")
	}
	redraw()

	// Resize detection via goroutine — works on all platforms including Windows
	resizeCh := make(chan struct{}, 1)
	go func() {
		for {
			time.Sleep(150 * time.Millisecond)
			W, H, err := term.GetSize(fd)
			if err != nil {
				continue
			}
			if W != lastW || H != lastH {
				resizeCh <- struct{}{}
			}
		}
	}()

	keysCh := make(chan []byte, 32)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			b, err := wReadKey(reader)
			if err != nil {
				close(keysCh)
				return
			}
			keysCh <- b
		}
	}()

	for {
		select {
		case <-resizeCh:
			// Terminal was resized — measure new size and redraw
			W, H, err := term.GetSize(fd)
			if err == nil && (W != lastW || H != lastH) {
				// Re-reserve space for new height
				newTotal := H
				if newTotal > totalRows {
					// Terminal got taller — print extra blank lines
					extra := newTotal - totalRows
					for i := 0; i < extra; i++ {
						fmt.Print("\r\n")
					}
				}
				totalRows = newTotal
				lastW, lastH = W, H
				e.resize()
				redraw()
			}

		case b, ok := <-keysCh:
			if !ok {
				return nil, nil
			}

			switch {
			case b[0] == 19: // ctrl+s
				term.Restore(fd, old)
				fmt.Print("\x1b[?25h")
				fmt.Printf("\x1b[%d;0H", totalRows+1)
				c := strings.Join(e.lines, "\n")
				return &c, nil

			case b[0] == 17: // ctrl+q
				term.Restore(fd, old)
				fmt.Print("\x1b[?25h")
				fmt.Printf("\x1b[%d;0H", totalRows+1)
				return nil, nil

			case len(b) >= 3 && b[0] == 27 && b[1] == '[':
				switch b[2] {
				case 'A':
					if e.cy > 0 {
						e.cy--
						e.clampCx()
					}
				case 'B':
					if e.cy < len(e.lines)-1 {
						e.cy++
						e.clampCx()
					}
				case 'C':
					if e.cx < e.lineLen() {
						e.cx++
					} else if e.cy < len(e.lines)-1 {
						e.cy++
						e.cx = 0
					}
				case 'D':
					if e.cx > 0 {
						e.cx--
					} else if e.cy > 0 {
						e.cy--
						e.cx = e.lineLen()
					}
				case 'H':
					e.cx = 0
				case 'F':
					e.cx = e.lineLen()
				case '3':
					e.deleteForward()
				case '5':
					for i := 0; i < e.visH && e.cy > 0; i++ {
						e.cy--
					}
					e.clampCx()
				case '6':
					for i := 0; i < e.visH && e.cy < len(e.lines)-1; i++ {
						e.cy++
					}
					e.clampCx()
				}

			case b[0] == 13:
				e.newline()
			case b[0] == 127 || b[0] == 8:
				e.backspace()
			case b[0] == 9:
				e.insert(' ')
				e.insert(' ')
			case b[0] == 2:
				e.wrapWord("**", "**")
			case b[0] == 5:
				e.wrapWord("_", "_")
			case b[0] == 11:
				e.wrapWord("`", "`")
			case b[0] == 7:
				e.insertBelow("```")
				e.insertBelow("")
				e.insertBelow("```")
				e.cy -= 2
			case b[0] == 12:
				e.prependLine("- ")
			case b[0] == 18:
				e.insertBelow("---")
			case b[0] == 28:
				e.insertHeading(1)
			case b[0] == 29:
				e.insertHeading(2)
			case b[0] == 30:
				e.insertHeading(3)
			default:
				if b[0] >= 32 {
					if r, _ := utf8.DecodeRune(b); r != utf8.RuneError {
						e.insert(r)
					}
				}
			}

			redraw()
		}
	}
}

func wReadKey(r *bufio.Reader) ([]byte, error) {
	b := make([]byte, 1)
	if _, err := r.Read(b); err != nil {
		return nil, err
	}
	if b[0] != 27 {
		return b, nil
	}
	buf := []byte{27}
	next, err := r.ReadByte()
	if err != nil {
		return buf, nil
	}
	buf = append(buf, next)
	if next != '[' {
		return buf, nil
	}
	c, err := r.ReadByte()
	if err != nil {
		return buf, nil
	}
	buf = append(buf, c)
	if c >= '0' && c <= '9' {
		if final, err := r.ReadByte(); err == nil {
			buf = append(buf, final)
		}
	}
	return buf, nil
}
