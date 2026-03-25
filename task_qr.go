package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type qrFormat int

const (
	qrFormatPNG qrFormat = iota + 1
	qrFormatSVG
	qrFormatASCII
)

type qrLevel int

const (
	qrLevelM qrLevel = iota
	qrLevelL
	qrLevelH
	qrLevelQ
)

type qrRequest struct {
	payload      string
	payloadSet   bool
	fromStdin    bool
	format       qrFormat
	outPath      string
	outSet       bool
	force        bool
	size         int
	margin       int
	level        qrLevel
	hasFormat    bool
	explicitPNG  bool
	explicitSVG  bool
	explicitText bool
}

type qrParsedRequest struct {
	qrRequest
	help bool
}

const (
	qrDefaultSize   = 256
	qrDefaultMargin  = 4
	qrDefaultOutput  = "qr.png"
	qrDefaultLevel   = qrLevelM
	qrHelpDefaultFmt = "png"
)

var qrLevelNames = map[string]qrLevel{
	"m": qrLevelM,
	"medium": qrLevelM,
	"l": qrLevelL,
	"low": qrLevelL,
	"h": qrLevelH,
	"high": qrLevelH,
	"q": qrLevelQ,
	"quartile": qrLevelQ,
}

var qrLevelToFormatBits = map[qrLevel]int{
	qrLevelL: 1,
	qrLevelM: 0,
	qrLevelQ: 3,
	qrLevelH: 2,
}

var qrByteCapacity = map[int]map[qrLevel]int{
	1: {
		qrLevelL: 17,
		qrLevelM: 14,
		qrLevelQ: 11,
		qrLevelH: 7,
	},
	2: {
		qrLevelL: 32,
		qrLevelM: 26,
		qrLevelQ: 20,
		qrLevelH: 14,
	},
}

var qrDataCodewords = map[int]map[qrLevel]int{
	1: {
		qrLevelL: 19,
		qrLevelM: 16,
		qrLevelQ: 13,
		qrLevelH: 9,
	},
	2: {
		qrLevelL: 34,
		qrLevelM: 28,
		qrLevelQ: 22,
		qrLevelH: 16,
	},
}

var qrEccCodewords = map[int]map[qrLevel]int{
	1: {
		qrLevelL: 7,
		qrLevelM: 10,
		qrLevelQ: 13,
		qrLevelH: 17,
	},
	2: {
		qrLevelL: 10,
		qrLevelM: 16,
		qrLevelQ: 22,
		qrLevelH: 28,
	},
}

var qrAlignmentCenters = map[int][]int{
	1: nil,
	2: []int{18},
}

var gfExpTable [512]int
var gfLogTable [256]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExpTable[i] = x
		gfLogTable[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < len(gfExpTable); i++ {
		gfExpTable[i] = gfExpTable[i-255]
	}
}

func jotQR(w io.Writer, args []string) error {
	return jotQRWithInput(os.Stdin, w, args, os.Getwd)
}

func jotQRWithInput(stdin io.Reader, w io.Writer, args []string, getwd func() (string, error)) error {
	req, help, err := parseQRArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, writeErr := io.WriteString(w, renderQRHelp(isTTY(w)))
			return writeErr
		}
		return err
	}
	if help {
		_, err := io.WriteString(w, renderQRHelp(isTTY(w)))
		return err
	}
	if getwd == nil {
		getwd = os.Getwd
	}
	cwd, err := getwd()
	if err != nil {
		return err
	}
	return executeQRRequest(stdin, w, cwd, req.qrRequest)
}

func renderQRHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot qr", "Generate a local QR code from text or a URL without leaving the terminal.")
	writeUsageSection(&b, style, []string{
		"jot qr https://example.com",
		"jot qr --text \"hello world\" --ascii",
		"jot qr --text \"hello world\" --svg",
		"jot qr --stdin --png --out qr.png",
	}, []string{
		"`jot qr` defaults to a PNG file when writing to disk.",
		"`--ascii` prints a terminal preview instead of writing an image.",
		"`jot task qr` is the guided discovery flow for the same command.",
	})
	writeCommandSection(&b, style, []helpCommand{
		{name: "--text VALUE", description: "Use inline text instead of a positional payload."},
		{name: "--stdin", description: "Read the payload from stdin."},
		{name: "--out PATH", description: "Write the QR image to a specific file."},
		{name: "--force", description: "Replace an existing output file."},
		{name: "--png", description: "Force PNG output."},
		{name: "--svg", description: "Force SVG output."},
		{name: "--ascii", description: "Render a terminal preview instead of writing an image."},
		{name: "--size PX", description: "Set the raster or SVG canvas size in pixels."},
		{name: "--margin N", description: "Set the quiet-zone margin in modules."},
		{name: "--level L|M|Q|H", description: "Choose the error-correction level."},
	})
	writeExamplesSection(&b, style, []string{
		"jot qr https://example.com",
		`jot qr --text "hello world" --ascii`,
		`jot qr --text "hello world" --svg`,
		`jot qr --stdin --out qr.png`,
	})
	return b.String()
}

func runQRTask(stdin io.Reader, w io.Writer, dir string) error {
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("QR")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("payload")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "url", "Encode a URL as a QR code", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "text", "Encode arbitrary text as a QR code", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	payloadKind, err := promptLine(reader, w, ui.styledPrompt("Select payload type", "1"))
	if err != nil {
		return err
	}
	_ = payloadKind

	if _, err := fmt.Fprint(w, ui.sectionLabel("value")); err != nil {
		return err
	}
	payload, err := promptLine(reader, w, ui.styledPrompt("Enter payload", "text or URL"))
	if err != nil {
		return err
	}
	if payload == "" {
		return errors.New("payload must be provided")
	}

	if _, err := fmt.Fprint(w, ui.sectionLabel("output format")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "png", "Write a PNG image", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "svg", "Write a scalable SVG", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "ascii", "Render an ASCII preview", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	formatSel, err := promptLine(reader, w, ui.styledPrompt("Select format", "1"))
	if err != nil {
		return err
	}
	format := qrFormatPNG
	switch strings.ToLower(strings.TrimSpace(formatSel)) {
	case "", "1", "png":
		format = qrFormatPNG
	case "2", "svg":
		format = qrFormatSVG
	case "3", "ascii":
		format = qrFormatASCII
	default:
		return fmt.Errorf("unknown format %q", formatSel)
	}

	req := qrRequest{
		payload:    payload,
		payloadSet: true,
		format:     format,
		size:       qrDefaultSize,
		margin:     qrDefaultMargin,
		level:      qrDefaultLevel,
	}
	if format != qrFormatASCII {
		defaultOut := qrDefaultOutput
		if format == qrFormatSVG {
			defaultOut = "qr.svg"
		}
		if _, err := fmt.Fprint(w, ui.sectionLabel("output")); err != nil {
			return err
		}
		outPath, err := promptLine(reader, w, ui.styledPrompt("Output path", defaultOut))
		if err != nil {
			return err
		}
		if strings.TrimSpace(outPath) == "" {
			outPath = defaultOut
		}
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(dir, outPath)
		}
		req.outPath = outPath
		req.outSet = true
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if err := executeQRRequest(reader, w, dir, req); err != nil {
		return err
	}

	tip := qrTaskTip(payload, format, req.outPath, false)
	if tip != "" {
		if _, err := fmt.Fprintln(w, ui.tip(tip)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func parseQRArgs(args []string) (qrParsedRequest, bool, error) {
	req := qrParsedRequest{
		qrRequest: qrRequest{
			format: qrFormatPNG,
			size:   qrDefaultSize,
			margin: qrDefaultMargin,
			level:  qrDefaultLevel,
		},
	}
	var positional []string
	formatCount := 0

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if isHelpFlag(arg) {
			req.help = true
			return req, true, nil
		}
		switch arg {
		case "--text":
			if req.fromStdin || req.payloadSet {
				return qrParsedRequest{}, false, errors.New("choose one input source: <text>, --text, or --stdin")
			}
			i++
			if i >= len(args) {
				return qrParsedRequest{}, false, errors.New("missing value for --text")
			}
			req.payload = args[i]
			req.payloadSet = true
			req.explicitText = true
		case "--stdin":
			if req.payloadSet || req.fromStdin || len(positional) > 0 {
				return qrParsedRequest{}, false, errors.New("choose one input source: <text>, --text, or --stdin")
			}
			req.fromStdin = true
		case "--out":
			i++
			if i >= len(args) {
				return qrParsedRequest{}, false, errors.New("missing value for --out")
			}
			req.outPath = args[i]
			req.outSet = true
		case "--force":
			req.force = true
		case "--png":
			formatCount++
			req.format = qrFormatPNG
			req.hasFormat = true
			req.explicitPNG = true
		case "--svg":
			formatCount++
			req.format = qrFormatSVG
			req.hasFormat = true
			req.explicitSVG = true
		case "--ascii":
			formatCount++
			req.format = qrFormatASCII
			req.hasFormat = true
		case "--size":
			i++
			if i >= len(args) {
				return qrParsedRequest{}, false, errors.New("missing value for --size")
			}
			size, parseErr := strconv.Atoi(strings.TrimSpace(args[i]))
			if parseErr != nil || size <= 0 {
				return qrParsedRequest{}, false, fmt.Errorf("invalid size %q", args[i])
			}
			req.size = size
		case "--margin":
			i++
			if i >= len(args) {
				return qrParsedRequest{}, false, errors.New("missing value for --margin")
			}
			margin, parseErr := strconv.Atoi(strings.TrimSpace(args[i]))
			if parseErr != nil || margin < 0 {
				return qrParsedRequest{}, false, fmt.Errorf("invalid margin %q", args[i])
			}
			req.margin = margin
		case "--level":
			i++
			if i >= len(args) {
				return qrParsedRequest{}, false, errors.New("missing value for --level")
			}
			level, ok := qrLevelNames[strings.ToLower(strings.TrimSpace(args[i]))]
			if !ok {
				return qrParsedRequest{}, false, fmt.Errorf("invalid level %q", args[i])
			}
			req.level = level
		default:
			if strings.HasPrefix(arg, "-") {
				return qrParsedRequest{}, false, fmt.Errorf("unsupported flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	if formatCount > 1 {
		return qrParsedRequest{}, false, errors.New("--png, --svg, and --ascii are mutually exclusive")
	}
	if req.fromStdin && len(positional) > 0 {
		return qrParsedRequest{}, false, errors.New("choose one input source: <text>, --text, or --stdin")
	}
	if req.payloadSet && len(positional) > 0 {
		return qrParsedRequest{}, false, errors.New("choose one input source: <text>, --text, or --stdin")
	}
	if !req.payloadSet && !req.fromStdin && len(positional) > 0 {
		req.payload = strings.Join(positional, " ")
		req.payloadSet = true
	}
	if !req.payloadSet && !req.fromStdin {
		return qrParsedRequest{}, false, errors.New("payload must be provided")
	}
	if req.format == qrFormatASCII && req.outSet {
		return qrParsedRequest{}, false, errors.New("--out cannot be combined with --ascii")
	}
	return req, false, nil
}

func executeQRRequest(stdin io.Reader, w io.Writer, cwd string, req qrRequest) error {
	payload, err := readQRPayload(stdin, req)
	if err != nil {
		return err
	}
	if payload == "" {
		return errors.New("payload must be provided")
	}

	version, err := chooseQRVersion(len([]byte(payload)), req.level)
	if err != nil {
		return err
	}

	matrix, err := buildQRMatrix(payload, version, req.level)
	if err != nil {
		return err
	}

	switch req.format {
	case qrFormatASCII:
		_, err = io.WriteString(w, renderQRASCII(matrix, req.margin))
		return err
	case qrFormatPNG, qrFormatSVG:
		outPath := req.outPath
		if outPath == "" {
			if req.format == qrFormatSVG {
				outPath = qrDefaultOutput[:len(qrDefaultOutput)-4] + ".svg"
			} else {
				outPath = qrDefaultOutput
			}
		}
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(cwd, outPath)
		}
		if err := writeQROutputFile(outPath, matrix, req); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "wrote %s\n", outPath); err != nil {
			return err
		}
		if tip := qrTaskTip(payload, req.format, outPath, false); tip != "" {
			if _, err := fmt.Fprintln(w, tip); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format")
	}
}

func readQRPayload(stdin io.Reader, req qrRequest) (string, error) {
	switch {
	case req.fromStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		if len(data) == 0 {
			return "", errors.New("stdin input is empty")
		}
		return string(data), nil
	case req.payloadSet:
		return req.payload, nil
	default:
		return "", errors.New("payload must be provided")
	}
}

func qrTaskTip(payload string, format qrFormat, outPath string, fromStdin bool) string {
	switch format {
	case qrFormatASCII:
		if fromStdin {
			return "next time: jot qr --stdin --ascii"
		}
		return fmt.Sprintf("next time: jot qr --text %q --ascii", payload)
	case qrFormatSVG:
		if fromStdin {
			if outPath != "" {
				return fmt.Sprintf("next time: jot qr --stdin --svg --out %q", filepath.Base(outPath))
			}
			return "next time: jot qr --stdin --svg"
		}
		if outPath != "" {
			return fmt.Sprintf("next time: jot qr --text %q --svg --out %q", payload, filepath.Base(outPath))
		}
		return fmt.Sprintf("next time: jot qr --text %q --svg", payload)
	default:
		if fromStdin {
			if outPath != "" {
				return fmt.Sprintf("next time: jot qr --stdin --png --out %q", filepath.Base(outPath))
			}
			return "next time: jot qr --stdin --png"
		}
		if outPath != "" {
			return fmt.Sprintf("next time: jot qr --text %q --png --out %q", payload, filepath.Base(outPath))
		}
		return fmt.Sprintf("next time: jot qr --text %q --png", payload)
	}
}

func chooseQRVersion(payloadBytes int, level qrLevel) (int, error) {
	for version := 1; version <= 2; version++ {
		if cap, ok := qrByteCapacity[version][level]; ok && payloadBytes <= cap {
			return version, nil
		}
	}
	return 0, fmt.Errorf("payload too large for selected QR level %s", qrLevelString(level))
}

func qrLevelString(level qrLevel) string {
	switch level {
	case qrLevelL:
		return "L"
	case qrLevelH:
		return "H"
	case qrLevelQ:
		return "Q"
	default:
		return "M"
	}
}

func buildQRMatrix(payload string, version int, level qrLevel) (*qrMatrix, error) {
	data, err := encodeQRPayload([]byte(payload), version, level)
	if err != nil {
		return nil, err
	}
	eccLen := qrEccCodewords[version][level]
	ecc := rsEncode(data, eccLen)
	codewords := append(append([]byte{}, data...), ecc...)
	base := newQRMatrix(version*4 + 17)
	base.drawFunctionPatterns(version)

	bestMask := 0
		bestScore := int(^uint(0) >> 1)
	var best *qrMatrix
	for mask := 0; mask < 8; mask++ {
		candidate := base.clone()
		candidate.placeCodewords(codewords, mask)
		score := candidate.penaltyScore()
		if score < bestScore {
			bestScore = score
			bestMask = mask
			best = candidate
		}
	}
	if best == nil {
		return nil, errors.New("failed to build QR matrix")
	}
	best.drawFormatBits(level, bestMask)
	return best, nil
}

func encodeQRPayload(payload []byte, version int, level qrLevel) ([]byte, error) {
	capacity := qrDataCodewords[version][level]
	if len(payload) > qrByteCapacity[version][level] {
		return nil, fmt.Errorf("payload too large for selected QR level %s", qrLevelString(level))
	}
	var bits qrBitBuffer
	bits.append(0b0100, 4)
	bits.append(len(payload), 8)
	for _, b := range payload {
		bits.append(int(b), 8)
	}
	if bits.bitLen()+4 > capacity*8 {
		bits.append(0, capacity*8-bits.bitLen())
	} else {
		bits.append(0, 4)
	}
	for bits.bitLen()%8 != 0 {
		bits.append(0, 1)
	}
	data := bits.pack()
	padBytes := []byte{0xEC, 0x11}
	for len(data) < capacity {
		data = append(data, padBytes[len(data)%2])
	}
	return data, nil
}

type qrBitBuffer struct {
	bits []byte
}

func (b *qrBitBuffer) append(value int, count int) {
	for i := count - 1; i >= 0; i-- {
		b.bits = append(b.bits, byte((value>>i)&1))
	}
}

func (b *qrBitBuffer) bitLen() int {
	return len(b.bits)
}

func (b *qrBitBuffer) pack() []byte {
	if len(b.bits) == 0 {
		return nil
	}
	out := make([]byte, 0, (len(b.bits)+7)/8)
	var current byte
	for i, bit := range b.bits {
		current = (current << 1) | bit
		if i%8 == 7 {
			out = append(out, current)
			current = 0
		}
	}
	if len(b.bits)%8 != 0 {
		current <<= uint(8 - len(b.bits)%8)
		out = append(out, current)
	}
	return out
}

func newQRMatrix(size int) *qrMatrix {
	cells := make([][]int8, size)
	reserved := make([][]bool, size)
	for i := range cells {
		cells[i] = make([]int8, size)
		reserved[i] = make([]bool, size)
	}
	return &qrMatrix{size: size, cells: cells, reserved: reserved}
}

type qrMatrix struct {
	size     int
	cells    [][]int8
	reserved [][]bool
}

func (m *qrMatrix) clone() *qrMatrix {
	clone := newQRMatrix(m.size)
	for y := 0; y < m.size; y++ {
		copy(clone.cells[y], m.cells[y])
		copy(clone.reserved[y], m.reserved[y])
	}
	return clone
}

func (m *qrMatrix) set(x, y int, value int8, reserve bool) {
	if y < 0 || y >= m.size || x < 0 || x >= m.size {
		return
	}
	m.cells[y][x] = value
	if reserve {
		m.reserved[y][x] = true
	}
}

func (m *qrMatrix) drawFunctionPatterns(version int) {
	size := m.size
	coords := [][2]int{{0, 0}, {size - 7, 0}, {0, size - 7}}
	for _, c := range coords {
		m.drawFinderPattern(c[0], c[1])
	}
	for i := 0; i < size; i++ {
		if !m.reserved[6][i] {
			m.set(i, 6, int8((i+1)%2), true)
		}
		if !m.reserved[i][6] {
			m.set(6, i, int8((i+1)%2), true)
		}
	}
	if version >= 2 {
		for _, cy := range qrAlignmentCenters[version] {
			for _, cx := range qrAlignmentCenters[version] {
				if (cx == 6 && cy == 6) || (cx == 6 && (cy == 0 || cy == size-7)) || (cy == 6 && (cx == 0 || cx == size-7)) {
					continue
				}
				m.drawAlignmentPattern(cx, cy)
			}
		}
	}
	m.set(8, 4*version+9, 1, true)
	m.reserveFormatInfoAreas()
}

func (m *qrMatrix) drawFinderPattern(x, y int) {
	for dy := -1; dy <= 7; dy++ {
		for dx := -1; dx <= 7; dx++ {
			xx, yy := x+dx, y+dy
			if xx < 0 || yy < 0 || xx >= m.size || yy >= m.size {
				continue
			}
			value := int8(0)
			reserve := true
			if dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6 {
				if dx == 0 || dx == 6 || dy == 0 || dy == 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4) {
					value = 1
				}
			}
			m.set(xx, yy, value, reserve)
		}
	}
}

func (m *qrMatrix) drawAlignmentPattern(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= m.size || y >= m.size {
				continue
			}
			value := int8(0)
			if dx == -2 || dx == 2 || dy == -2 || dy == 2 || (dx == 0 && dy == 0) {
				value = 1
			}
			m.set(x, y, value, true)
		}
	}
}

func (m *qrMatrix) reserveFormatInfoAreas() {
	size := m.size
	formatCoords := [][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8}, {7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8}, {size - 5, 8}, {size - 6, 8}, {size - 7, 8}, {size - 8, 8},
		{8, size - 7}, {8, size - 6}, {8, size - 5}, {8, size - 4}, {8, size - 3}, {8, size - 2}, {8, size - 1},
	}
	for _, c := range formatCoords {
		if c[0] >= 0 && c[1] >= 0 && c[0] < size && c[1] < size {
			m.reserved[c[1]][c[0]] = true
		}
	}
}

func (m *qrMatrix) drawFormatBits(level qrLevel, mask int) {
	info := ((qrLevelToFormatBits[level] << 3) | mask) & 0x1F
	bits := formatBits(info)
	size := m.size
	coordsA := [][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8}, {7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8},
	}
	coordsB := [][2]int{
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8}, {size - 5, 8}, {size - 6, 8}, {size - 7, 8}, {size - 8, 8},
		{8, size - 7}, {8, size - 6}, {8, size - 5}, {8, size - 4}, {8, size - 3}, {8, size - 2}, {8, size - 1},
	}
	for i, c := range coordsA {
		m.set(c[0], c[1], bitAt(bits, 14-i), true)
	}
	for i, c := range coordsB {
		m.set(c[0], c[1], bitAt(bits, 14-i), true)
	}
}

func bitAt(bits int, index int) int8 {
	if (bits>>index)&1 == 1 {
		return 1
	}
	return 0
}

func formatBits(info int) int {
	// BCH code with generator polynomial 0x537, then XOR mask 0x5412.
	data := info << 10
	for i := 14; i >= 10; i-- {
		if (data>>i)&1 == 1 {
			data ^= 0x537 << (i - 10)
		}
	}
	return ((info << 10) | data) ^ 0x5412
}

func (m *qrMatrix) placeCodewords(codewords []byte, mask int) {
	totalBits := len(codewords) * 8
	bitIndex := 0
	upward := true
	for col := m.size - 1; col > 0; col -= 2 {
		if col == 6 {
			col--
		}
		for i := 0; i < m.size; i++ {
			row := i
			if upward {
				row = m.size - 1 - i
			}
			for dx := 0; dx < 2; dx++ {
				x := col - dx
				y := row
				if m.reserved[y][x] {
					continue
				}
				if bitIndex < totalBits {
					byteIndex := bitIndex / 8
					bitShift := 7 - (bitIndex % 8)
					bit := int8((codewords[byteIndex] >> bitShift) & 1)
					if qrMaskApplies(mask, x, y) {
						bit ^= 1
					}
					m.cells[y][x] = bit
					bitIndex++
				}
			}
		}
		upward = !upward
	}
}

func qrMaskApplies(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return ((y/2)+(x/3))%2 == 0
	case 5:
		return ((x*y)%2)+((x*y)%3) == 0
	case 6:
		return (((x*y)%2)+((x*y)%3))%2 == 0
	case 7:
		return (((x+y)%2)+((x*y)%3))%2 == 0
	default:
		return false
	}
}

func (m *qrMatrix) penaltyScore() int {
	score := 0
	// Rule 1: consecutive modules in rows and columns.
	for y := 0; y < m.size; y++ {
		score += penaltyRuns(m.cells[y])
	}
	for x := 0; x < m.size; x++ {
		col := make([]int8, m.size)
		for y := 0; y < m.size; y++ {
			col[y] = m.cells[y][x]
		}
		score += penaltyRuns(col)
	}
	// Rule 2: 2x2 blocks.
	for y := 0; y < m.size-1; y++ {
		for x := 0; x < m.size-1; x++ {
			c := m.cells[y][x]
			if c == m.cells[y][x+1] && c == m.cells[y+1][x] && c == m.cells[y+1][x+1] {
				score += 3
			}
		}
	}
	// Rule 3: finder-like patterns.
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size-10; x++ {
			if qrMatchesFinderLike(m.cells[y][x : x+11]) {
				score += 40
			}
		}
	}
	for x := 0; x < m.size; x++ {
		col := make([]int8, m.size)
		for y := 0; y < m.size; y++ {
			col[y] = m.cells[y][x]
		}
		for y := 0; y < m.size-10; y++ {
			if qrMatchesFinderLike(col[y : y+11]) {
				score += 40
			}
		}
	}
	// Rule 4: balance of dark modules.
	dark := 0
	total := m.size * m.size
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if m.cells[y][x] == 1 {
				dark++
			}
		}
	}
	diff := dark*20 - total*10
	if diff < 0 {
		diff = -diff
	}
	k := diff / total
	score += (k / 5) * 10
	return score
}

func penaltyRuns(line []int8) int {
	if len(line) == 0 {
		return 0
	}
	score := 0
	runColor := line[0]
	runLength := 1
	for i := 1; i < len(line); i++ {
		if line[i] == runColor {
			runLength++
			continue
		}
		if runLength >= 5 {
			score += 3 + (runLength - 5)
		}
		runColor = line[i]
		runLength = 1
	}
	if runLength >= 5 {
		score += 3 + (runLength - 5)
	}
	return score
}

func qrMatchesFinderLike(line []int8) bool {
	pattern := []int8{1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0}
	if len(line) != len(pattern) {
		return false
	}
	for i, v := range pattern {
		if line[i] != v {
			return false
		}
	}
	return true
}

func rsEncode(data []byte, eccLen int) []byte {
	if eccLen == 0 {
		return nil
	}
	gen := rsGeneratorPoly(eccLen)
	remainder := make([]int, len(data)+eccLen)
	for i, b := range data {
		remainder[i] = int(b)
	}
	for i := 0; i < len(data); i++ {
		factor := remainder[i]
		if factor == 0 {
			continue
		}
		for j := 0; j < len(gen); j++ {
			remainder[i+j] ^= gfMul(gen[j], factor)
		}
	}
	ecc := make([]byte, eccLen)
	for i := 0; i < eccLen; i++ {
		ecc[i] = byte(remainder[len(data)+i])
	}
	return ecc
}

func rsGeneratorPoly(degree int) []int {
	coeffs := []int{1}
	for i := 0; i < degree; i++ {
		coeffs = polyMul(coeffs, []int{1, gfExpTable[i]})
	}
	return coeffs
}

func polyMul(a, b []int) []int {
	out := make([]int, len(a)+len(b)-1)
	for i, av := range a {
		for j, bv := range b {
			out[i+j] ^= gfMul(av, bv)
		}
	}
	return out
}

func gfMul(x, y int) int {
	if x == 0 || y == 0 {
		return 0
	}
	return gfExpTable[(gfLogTable[x]+gfLogTable[y])%255]
}

func writeQROutputFile(path string, matrix *qrMatrix, req qrRequest) error {
	if !req.force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("output already exists: %s", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch req.format {
	case qrFormatSVG:
		return writeQRSVG(path, matrix, req)
	default:
		return writeQRPNG(path, matrix, req)
	}
}

func writeQRPNG(path string, matrix *qrMatrix, req qrRequest) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	img := renderQRImage(matrix, req.size, req.margin)
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return nil
}

func renderQRImage(matrix *qrMatrix, size, margin int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			canvas.Set(x, y, white)
		}
	}
	totalModules := matrix.size + margin*2
	moduleSize := size / totalModules
	if moduleSize < 1 {
		moduleSize = 1
	}
	drawSize := moduleSize * totalModules
	offset := (size - drawSize) / 2
	for y := 0; y < matrix.size; y++ {
		for x := 0; x < matrix.size; x++ {
			if matrix.cells[y][x] != 1 {
				continue
			}
			startX := offset + (x+margin)*moduleSize
			startY := offset + (y+margin)*moduleSize
			for py := 0; py < moduleSize; py++ {
				for px := 0; px < moduleSize; px++ {
					canvas.Set(startX+px, startY+py, black)
				}
			}
		}
	}
	return canvas
}

func writeQRSVG(path string, matrix *qrMatrix, req qrRequest) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	totalModules := matrix.size + req.margin*2
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, req.size, req.size, totalModules, totalModules)
	b.WriteString("\n")
	b.WriteString("<title>")
	b.WriteString(template.HTMLEscapeString(req.payload))
	b.WriteString("</title>\n")
	b.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="#fff"/>`+"\n", totalModules, totalModules))
	for y := 0; y < matrix.size; y++ {
		for x := 0; x < matrix.size; x++ {
			if matrix.cells[y][x] != 1 {
				continue
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" fill="#000"/>`+"\n", x+req.margin, y+req.margin)
		}
	}
	b.WriteString("</svg>\n")
	if _, err := io.WriteString(file, b.String()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return nil
}

func renderQRASCII(matrix *qrMatrix, margin int) string {
	var b strings.Builder
	total := matrix.size + margin*2
	for y := 0; y < total; y++ {
		for x := 0; x < total; x++ {
			cell := 0
			if x >= margin && y >= margin && x < margin+matrix.size && y < margin+matrix.size {
				cell = int(matrix.cells[y-margin][x-margin])
			}
			if cell == 1 {
				b.WriteString("##")
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
