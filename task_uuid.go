package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

const defaultUUIDNanoidAlphabet = "_-0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const defaultUUIDStringAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
const defaultUUIDNanoidLength = 21
const defaultUUIDStringLength = 21

type uuidOptions struct {
	Type      string
	Count     int
	Length    int
	Alphabet  string
	Upper     bool
	Lower     bool
	Quiet     bool
	lengthSet bool
}

func jotUUID(w io.Writer, args []string) error {
	options, err := parseUUIDArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, writeErr := io.WriteString(w, renderUUIDHelp(isTTY(w)))
			return writeErr
		}
		return err
	}

	values, err := generateUUIDValues(options)
	if err != nil {
		return err
	}

	for _, value := range values {
		if _, err := fmt.Fprintln(w, value); err != nil {
			return err
		}
	}
	return nil
}

func renderUUIDHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot uuid", "Generate UUIDs and other local random identifiers from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot uuid",
		"jot uuid --type nanoid --count 5",
		"jot uuid --type string --length 24 --alphabet abc123",
		"jot task uuid",
	}, []string{
		"`jot uuid` prints one UUID v4 by default.",
		"`jot task uuid` is the guided terminal flow for the same command.",
	})
	writeCommandSection(&b, style, []helpCommand{
		{name: "--type uuid|nanoid|string", description: "Pick the identifier family."},
		{name: "--count N", description: "Emit multiple identifiers, one per line."},
		{name: "--length N", description: "Set the output length for nanoid or string modes."},
		{name: "--alphabet TEXT", description: "Override the random-string alphabet."},
		{name: "--upper", description: "Uppercase the generated values."},
		{name: "--lower", description: "Lowercase the generated values."},
		{name: "--quiet", description: "Keep the output machine-friendly."},
	})
	writeExamplesSection(&b, style, []string{
		"jot uuid",
		"jot uuid --type nanoid --count 5",
		"jot uuid --type string --length 16 --alphabet ab12",
		"jot task uuid",
	})
	return b.String()
}

func runUUIDTask(stdin io.Reader, w io.Writer, dir string) error {
	_ = dir
	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("UUID")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("task")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "uuid", "Generate RFC 4122 v4 identifiers", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "nanoid", "Generate short URL-safe identifiers", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "string", "Generate random strings with a custom alphabet", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	kind, err := promptLine(reader, w, ui.styledPrompt("Select type", "1"))
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "1", "uuid":
		kind = "uuid"
	case "2", "nanoid":
		kind = "nanoid"
	case "3", "string", "random string":
		kind = "string"
	default:
		return fmt.Errorf("unknown UUID type %q", kind)
	}

	countText, err := promptLine(reader, w, ui.styledPrompt("Count", "1"))
	if err != nil {
		return err
	}
	count := 1
	if strings.TrimSpace(countText) != "" {
		count, err = parsePositiveInt(countText, "count")
		if err != nil {
			return err
		}
	}

	length := 0
	if kind == "nanoid" {
		length = defaultUUIDNanoidLength
	}
	if kind == "string" {
		length = defaultUUIDStringLength
	}
	if kind == "nanoid" || kind == "string" {
		lengthText, err := promptLine(reader, w, ui.styledPrompt("Length", fmt.Sprintf("%d", length)))
		if err != nil {
			return err
		}
		if strings.TrimSpace(lengthText) != "" {
			length, err = parsePositiveInt(lengthText, "length")
			if err != nil {
				return err
			}
		}
	}

	alphabet := ""
	if kind == "nanoid" || kind == "string" {
		alphabetHint := "default"
		if kind == "nanoid" {
			alphabetHint = "default nanoid alphabet"
		}
		alphabet, err = promptLine(reader, w, ui.styledPrompt("Alphabet", alphabetHint))
		if err != nil {
			return err
		}
		alphabet = strings.TrimSpace(alphabet)
	}

	options := uuidOptions{
		Type:     kind,
		Count:    count,
		Length:   length,
		Alphabet: alphabet,
	}
	values, err := generateUUIDValues(options)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.success(fmt.Sprintf("%d value(s) generated", len(values)))); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintln(w, value); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ui.tip(renderUUIDTaskTip(options))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func renderUUIDTaskTip(options uuidOptions) string {
	args := []string{"jot", "uuid"}
	if options.Type != "" && options.Type != "uuid" {
		args = append(args, "--type", options.Type)
	}
	if options.Count > 1 {
		args = append(args, "--count", fmt.Sprintf("%d", options.Count))
	}
	if options.Length > 0 && (options.Type == "nanoid" || options.Type == "string") {
		args = append(args, "--length", fmt.Sprintf("%d", options.Length))
	}
	if options.Alphabet != "" {
		args = append(args, "--alphabet", options.Alphabet)
	}
	return "next time: " + strings.Join(args, " ")
}

func parseUUIDArgs(args []string) (uuidOptions, error) {
	for _, arg := range args {
		if isHelpFlag(arg) {
			return uuidOptions{}, flag.ErrHelp
		}
	}

	fs := flag.NewFlagSet("uuid", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var options uuidOptions
	fs.StringVar(&options.Type, "type", "uuid", "")
	fs.IntVar(&options.Count, "count", 1, "")
	fs.IntVar(&options.Length, "length", 0, "")
	fs.StringVar(&options.Alphabet, "alphabet", "", "")
	fs.BoolVar(&options.Upper, "upper", false, "")
	fs.BoolVar(&options.Lower, "lower", false, "")
	fs.BoolVar(&options.Quiet, "quiet", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return uuidOptions{}, flag.ErrHelp
		}
		return uuidOptions{}, err
	}

	if fs.NArg() != 0 {
		return uuidOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	options.Type = canonicalUUIDType(options.Type)
	if !isSupportedUUIDType(options.Type) {
		return uuidOptions{}, fmt.Errorf("unsupported type %q; use `uuid`, `nanoid`, or `string`", options.Type)
	}
	if options.Count < 1 {
		return uuidOptions{}, errors.New("count must be at least 1")
	}
	if options.Upper && options.Lower {
		return uuidOptions{}, errors.New("choose only one of --upper or --lower")
	}
	if options.Length < 0 {
		return uuidOptions{}, errors.New("length must be at least 1")
	}
	if options.Type == "uuid" {
		if options.Length != 0 {
			return uuidOptions{}, errors.New("--length only applies to nanoid or string")
		}
		if options.Alphabet != "" {
			return uuidOptions{}, errors.New("--alphabet only applies to nanoid or string")
		}
	}
	if options.Type == "nanoid" && options.Length == 0 {
		options.Length = defaultUUIDNanoidLength
	}
	if options.Type == "string" && options.Length == 0 {
		options.Length = defaultUUIDStringLength
	}
	if (options.Type == "nanoid" || options.Type == "string") && options.Length < 1 {
		return uuidOptions{}, errors.New("length must be at least 1")
	}

	return options, nil
}

func canonicalUUIDType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "uuid", "nanoid", "string":
		return value
	case "random":
		return "string"
	default:
		return value
	}
}

func isSupportedUUIDType(value string) bool {
	switch value {
	case "uuid", "nanoid", "string":
		return true
	default:
		return false
	}
}

func generateUUIDValues(options uuidOptions) ([]string, error) {
	values := make([]string, 0, options.Count)
	for i := 0; i < options.Count; i++ {
		value, err := generateUUIDValue(options)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func generateUUIDValue(options uuidOptions) (string, error) {
	var value string
	var err error
	switch options.Type {
	case "uuid":
		value, err = newUUIDv4()
	case "nanoid":
		value, err = newNanoID(options.Length, options.Alphabet)
	case "string":
		value, err = newRandomString(options.Length, options.Alphabet)
	default:
		return "", fmt.Errorf("unsupported type %q", options.Type)
	}
	if err != nil {
		return "", err
	}
	if options.Upper {
		value = strings.ToUpper(value)
	}
	if options.Lower {
		value = strings.ToLower(value)
	}
	return value, nil
}

func newUUIDv4() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}

func newNanoID(length int, alphabet string) (string, error) {
	if alphabet == "" {
		alphabet = defaultUUIDNanoidAlphabet
	}
	return newRandomIdentifier(length, alphabet)
}

func newRandomString(length int, alphabet string) (string, error) {
	if alphabet == "" {
		alphabet = defaultUUIDStringAlphabet
	}
	return newRandomIdentifier(length, alphabet)
}

func newRandomIdentifier(length int, alphabet string) (string, error) {
	if length < 1 {
		return "", errors.New("length must be at least 1")
	}
	if alphabet == "" {
		return "", errors.New("alphabet must not be empty")
	}
	letters := []rune(alphabet)
	if len(letters) == 0 {
		return "", errors.New("alphabet must not be empty")
	}
	var b strings.Builder
	b.Grow(length)
	max := big.NewInt(int64(len(letters)))
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteRune(letters[idx.Int64()])
	}
	return b.String(), nil
}

func parsePositiveInt(text, name string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be at least 1", name)
	}
	return n, nil
}
