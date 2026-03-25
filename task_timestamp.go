package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type timestampDisplayMode int

const (
	timestampDisplayAuto timestampDisplayMode = iota
	timestampDisplayUnixOnly
	timestampDisplayHumanOnly
)

type timestampUnitMode int

const (
	timestampUnitAuto timestampUnitMode = iota
	timestampUnitSeconds
	timestampUnitMilliseconds
)

type timestampSourceKind int

const (
	timestampSourceUnknown timestampSourceKind = iota
	timestampSourceNumeric
	timestampSourceHuman
	timestampSourceNow
)

type timestampOptions struct {
	input       string
	useStdin    bool
	displayMode timestampDisplayMode
	unitMode    timestampUnitMode
	tzName      string
	useUTC      bool
	format      string
	help        bool
}

type timestampEvaluation struct {
	sourceKind      timestampSourceKind
	instant         time.Time
	displayLocation *time.Location
	humanLayout     string
	humanText       string
	utcSeconds      int64
}

const timestampDefaultLayout = "2006-01-02 15:04:05"

func jotTimestamp(w io.Writer, args []string, now func() time.Time) error {
	return jotTimestampFromReader(os.Stdin, w, args, now)
}

func jotTimestampFromReader(stdin io.Reader, w io.Writer, args []string, now func() time.Time) error {
	if now == nil {
		now = time.Now
	}

	opts, err := parseTimestampArgs(args)
	if err != nil {
		return err
	}
	if opts.help {
		_, err := io.WriteString(w, renderTimestampHelp(isTTY(w)))
		return err
	}

	eval, err := evaluateTimestamp(stdin, opts, now)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, formatTimestampOutput(eval, opts))
	return err
}

func parseTimestampArgs(args []string) (timestampOptions, error) {
	var opts timestampOptions
	var unitFlagSet bool
	var displayFlagSet bool
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "", "--":
			continue
		case "-h", "--help":
			opts.help = true
		case "--stdin":
			opts.useStdin = true
		case "--utc":
			opts.useUTC = true
		case "--ms":
			if unitFlagSet && opts.unitMode != timestampUnitMilliseconds {
				return timestampOptions{}, errors.New("timestamp: choose one numeric unit: --ms or --seconds")
			}
			opts.unitMode = timestampUnitMilliseconds
			unitFlagSet = true
		case "--seconds":
			if unitFlagSet && opts.unitMode != timestampUnitSeconds {
				return timestampOptions{}, errors.New("timestamp: choose one numeric unit: --ms or --seconds")
			}
			opts.unitMode = timestampUnitSeconds
			unitFlagSet = true
		case "--unix":
			if displayFlagSet && opts.displayMode != timestampDisplayUnixOnly {
				return timestampOptions{}, errors.New("timestamp: choose one output mode: --unix or --human")
			}
			opts.displayMode = timestampDisplayUnixOnly
			displayFlagSet = true
		case "--human":
			if displayFlagSet && opts.displayMode != timestampDisplayHumanOnly {
				return timestampOptions{}, errors.New("timestamp: choose one output mode: --unix or --human")
			}
			opts.displayMode = timestampDisplayHumanOnly
			displayFlagSet = true
		case "--tz":
			i++
			if i >= len(args) {
				return timestampOptions{}, errors.New("timestamp: missing value for --tz")
			}
			opts.tzName = strings.TrimSpace(args[i])
		case "--format":
			i++
			if i >= len(args) {
				return timestampOptions{}, errors.New("timestamp: missing value for --format")
			}
			opts.format = args[i]
		default:
			if strings.HasPrefix(arg, "-") {
				return timestampOptions{}, fmt.Errorf("timestamp: unknown option %q", arg)
			}
			if opts.input != "" {
				return timestampOptions{}, fmt.Errorf("timestamp: unexpected extra argument %q", arg)
			}
			opts.input = arg
		}
	}

	if opts.help {
		return opts, nil
	}
	if opts.useStdin && opts.input != "" {
		return timestampOptions{}, errors.New("timestamp: choose one input source: <value> or --stdin")
	}
	if opts.useUTC && opts.tzName != "" {
		return timestampOptions{}, errors.New("timestamp: choose one timezone source: --utc or --tz")
	}
	if !opts.useStdin && opts.input == "" {
		return timestampOptions{}, errors.New("timestamp: a timestamp value, `now`, or --stdin is required")
	}

	return opts, nil
}

func evaluateTimestamp(stdin io.Reader, opts timestampOptions, now func() time.Time) (timestampEvaluation, error) {
	source := strings.TrimSpace(opts.input)
	if opts.useStdin {
		if stdin == nil {
			stdin = os.Stdin
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return timestampEvaluation{}, err
		}
		source = strings.TrimSpace(string(data))
		if source == "" {
			return timestampEvaluation{}, errors.New("timestamp: stdin input is empty")
		}
	}
	if source == "" {
		return timestampEvaluation{}, errors.New("timestamp: a timestamp value is required")
	}

	location, err := resolveTimestampLocation(opts)
	if err != nil {
		return timestampEvaluation{}, err
	}

	evaluation := timestampEvaluation{
		displayLocation: location,
		humanLayout:     timestampHumanLayout(opts.format),
	}

	if strings.EqualFold(source, "now") {
		if opts.unitMode != timestampUnitAuto {
			return timestampEvaluation{}, errors.New("timestamp: --ms and --seconds apply to numeric Unix input only")
		}
		evaluation.sourceKind = timestampSourceNow
		evaluation.instant = now().In(location)
		evaluation.utcSeconds = evaluation.instant.Unix()
		evaluation.humanText = evaluation.instant.Format(evaluation.humanLayout)
		return evaluation, nil
	}

	if numeric, ok := parseTimestampInt(source); ok {
		if opts.unitMode == timestampUnitAuto {
			opts.unitMode = timestampUnitSeconds
		}
		evaluation.sourceKind = timestampSourceNumeric
		evaluation.instant = numericTimestampInstant(numeric, opts.unitMode)
		evaluation.utcSeconds = evaluation.instant.Unix()
		evaluation.humanText = evaluation.instant.In(location).Format(evaluation.humanLayout)
		return evaluation, nil
	}

	if opts.unitMode != timestampUnitAuto {
		return timestampEvaluation{}, errors.New("timestamp: --ms and --seconds apply to numeric Unix input only")
	}

	parsed, parsedLocation, err := parseHumanTimestamp(source, location)
	if err != nil {
		return timestampEvaluation{}, err
	}
	evaluation.sourceKind = timestampSourceHuman
	evaluation.instant = parsed
	evaluation.utcSeconds = parsed.Unix()
	if opts.useUTC || opts.tzName != "" {
		evaluation.displayLocation = location
	} else if parsedLocation != nil {
		evaluation.displayLocation = parsedLocation
	}
	evaluation.humanText = evaluation.instant.In(evaluation.displayLocation).Format(evaluation.humanLayout)
	return evaluation, nil
}

func resolveTimestampLocation(opts timestampOptions) (*time.Location, error) {
	if opts.useUTC {
		return time.UTC, nil
	}
	if opts.tzName != "" {
		location, err := time.LoadLocation(opts.tzName)
		if err != nil {
			return nil, fmt.Errorf("timestamp: unknown timezone %q", opts.tzName)
		}
		return location, nil
	}
	return time.Local, nil
}

func parseTimestampInt(text string) (int64, bool) {
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func numericTimestampInstant(value int64, unit timestampUnitMode) time.Time {
	if unit == timestampUnitMilliseconds {
		return time.Unix(0, value*int64(time.Millisecond))
	}
	return time.Unix(value, 0)
}

type timestampParseLayout struct {
	layout  string
	inLoc   bool
	hasZone bool
}

func parseHumanTimestamp(text string, location *time.Location) (time.Time, *time.Location, error) {
	layouts := []timestampParseLayout{
		{layout: time.RFC3339Nano, hasZone: true},
		{layout: time.RFC3339, hasZone: true},
		{layout: "2006-01-02 15:04:05 MST", hasZone: true},
		{layout: "2006-01-02 15:04:05 -0700", hasZone: true},
		{layout: "2006-01-02T15:04:05Z07:00", hasZone: true},
		{layout: "2006-01-02T15:04:05Z0700", hasZone: true},
		{layout: "2006-01-02T15:04:05Z07", hasZone: true},
		{layout: "2006-01-02 15:04:05", inLoc: true},
		{layout: "2006-01-02 15:04", inLoc: true},
		{layout: "2006-01-02T15:04:05", inLoc: true},
		{layout: "2006-01-02T15:04", inLoc: true},
		{layout: "2006-01-02", inLoc: true},
		{layout: "Jan 2 2006 15:04:05", inLoc: true},
		{layout: "Jan 2 2006 15:04", inLoc: true},
	}

	for _, candidate := range layouts {
		if candidate.inLoc {
			parsed, err := time.ParseInLocation(candidate.layout, text, location)
			if err == nil {
				return parsed, location, nil
			}
			continue
		}
		parsed, err := time.Parse(candidate.layout, text)
		if err == nil {
			return parsed, parsed.Location(), nil
		}
	}

	return time.Time{}, nil, fmt.Errorf("timestamp: unsupported date format %q", text)
}

func timestampHumanLayout(layout string) string {
	if strings.TrimSpace(layout) == "" {
		return timestampDefaultLayout
	}
	return layout
}

func formatTimestampOutput(eval timestampEvaluation, opts timestampOptions) string {
	switch opts.displayMode {
	case timestampDisplayUnixOnly:
		return fmt.Sprintf("Unix: %d\n", eval.utcSeconds)
	case timestampDisplayHumanOnly:
		return fmt.Sprintf("Human: %s\nTimezone: %s\n", eval.humanText, eval.displayLocation.String())
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "Unix: %d\n", eval.utcSeconds)
		fmt.Fprintf(&b, "Human: %s\n", eval.humanText)
		fmt.Fprintf(&b, "Timezone: %s\n", eval.displayLocation.String())
		return b.String()
	}
}

func renderTimestampHelp(color bool) string {
	style := helpStyler{color: color}
	var b strings.Builder
	writeHelpHeader(&b, style, "jot timestamp", "Convert between Unix timestamps and human dates from the terminal.")
	writeUsageSection(&b, style, []string{
		"jot timestamp <value>",
		"jot timestamp now",
		"jot timestamp --stdin",
		"jot task timestamp",
	}, []string{
		"Numeric input defaults to Unix seconds unless `--ms` is provided.",
		"Human output always includes the timezone that was used.",
		"`--format` accepts a Go time layout string.",
	})
	writeFlagSection(&b, style, []helpFlag{
		{name: "--ms", description: "Interpret numeric input as milliseconds."},
		{name: "--seconds", description: "Interpret numeric input as seconds."},
		{name: "--tz <zone>", description: "Use a specific timezone such as `Europe/London`."},
		{name: "--utc", description: "Use UTC instead of the local timezone."},
		{name: "--format <layout>", description: "Format the human-readable line with a Go time layout."},
		{name: "--unix", description: "Print the Unix value only."},
		{name: "--human", description: "Print the human-readable value only."},
		{name: "--stdin", description: "Read the timestamp value from stdin."},
	})
	writeExamplesSection(&b, style, []string{
		"jot timestamp 1710000000",
		"jot timestamp 1710000000 --ms",
		`jot timestamp "2025-03-25 12:00:00" --tz Europe/London`,
		"jot timestamp now --utc",
		"jot timestamp --stdin",
		"jot task timestamp",
	})
	return b.String()
}

func runTimestampTask(stdin io.Reader, w io.Writer, dir string, now func() time.Time) error {
	_ = dir
	if now == nil {
		now = time.Now
	}

	reader := bufio.NewReader(stdin)
	ui := newTermUI(w)

	if _, err := fmt.Fprint(w, ui.header("Timestamp")); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, ui.sectionLabel("modes")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(1, "Unix to human", "Convert a Unix timestamp into a human date", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(2, "Human to Unix", "Convert a human date into Unix seconds", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.listItem(3, "Now", "Capture the current instant in both forms", "")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}

	selection, err := promptLine(reader, w, ui.styledPrompt("Select mode", "1"))
	if err != nil {
		return err
	}

	mode := strings.ToLower(strings.TrimSpace(selection))
	switch mode {
	case "", "1", "unix", "unix to human":
		return runTimestampUnixToHumanTask(reader, w, ui, now)
	case "2", "human", "human to unix":
		return runTimestampHumanToUnixTask(reader, w, ui, now)
	case "3", "now":
		return runTimestampNowTask(reader, w, ui, now)
	default:
		return fmt.Errorf("unknown timestamp task selection %q", selection)
	}
}

func runTimestampUnixToHumanTask(reader *bufio.Reader, w io.Writer, ui termUI, now func() time.Time) error {
	value, err := promptLine(reader, w, ui.styledPrompt("Unix seconds", "1710000000"))
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("timestamp: Unix seconds value is required")
	}

	tzText, err := promptLine(reader, w, ui.styledPrompt("Timezone", "local"))
	if err != nil {
		return err
	}

	opts := timestampOptions{input: value, displayMode: timestampDisplayAuto}
	if tzText = strings.TrimSpace(tzText); tzText != "" {
		if strings.EqualFold(tzText, "utc") {
			opts.useUTC = true
		} else {
			opts.tzName = tzText
		}
	}
	eval, err := evaluateTimestamp(strings.NewReader(""), opts, now)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ui.success("timestamp converted")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Unix: %d", eval.utcSeconds)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Human: %s", eval.humanText)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Timezone: %s", eval.displayLocation.String())); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(timestampTaskTip("unix", value, opts))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func runTimestampHumanToUnixTask(reader *bufio.Reader, w io.Writer, ui termUI, now func() time.Time) error {
	value, err := promptLine(reader, w, ui.styledPrompt("Human date/time", "2025-03-25 12:00:00"))
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("timestamp: human date/time value is required")
	}

	tzText, err := promptLine(reader, w, ui.styledPrompt("Timezone", "local"))
	if err != nil {
		return err
	}

	opts := timestampOptions{input: value, displayMode: timestampDisplayAuto}
	if tzText = strings.TrimSpace(tzText); tzText != "" {
		if strings.EqualFold(tzText, "utc") {
			opts.useUTC = true
		} else {
			opts.tzName = tzText
		}
	}
	eval, err := evaluateTimestamp(strings.NewReader(""), opts, now)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ui.success("timestamp converted")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Unix: %d", eval.utcSeconds)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Human: %s", eval.humanText)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Timezone: %s", eval.displayLocation.String())); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(timestampTaskTip("human", value, opts))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func runTimestampNowTask(reader *bufio.Reader, w io.Writer, ui termUI, now func() time.Time) error {
	tzText, err := promptLine(reader, w, ui.styledPrompt("Timezone", "local"))
	if err != nil {
		return err
	}

	opts := timestampOptions{input: "now", displayMode: timestampDisplayAuto}
	if tzText = strings.TrimSpace(tzText); tzText != "" {
		if strings.EqualFold(tzText, "utc") {
			opts.useUTC = true
		} else {
			opts.tzName = tzText
		}
	}
	eval, err := evaluateTimestamp(strings.NewReader(""), opts, now)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, ui.success("timestamp captured")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Unix: %d", eval.utcSeconds)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Human: %s", eval.humanText)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    "+fmt.Sprintf("Timezone: %s", eval.displayLocation.String())); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ui.tip(timestampTaskTip("now", "now", opts))); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "")
	return err
}

func timestampTaskTip(mode, value string, opts timestampOptions) string {
	switch mode {
	case "now":
		if opts.useUTC {
			return "next time: jot timestamp now --utc"
		}
		if opts.tzName != "" {
			return fmt.Sprintf("next time: jot timestamp now --tz %s", opts.tzName)
		}
		return "next time: jot timestamp now"
	case "unix":
		arg := value
		if strings.ContainsAny(arg, " \t\"'") {
			arg = strconv.Quote(arg)
		}
		switch {
		case opts.useUTC:
			return fmt.Sprintf("next time: jot timestamp %s --utc", arg)
		case opts.tzName != "":
			return fmt.Sprintf("next time: jot timestamp %s --tz %s", arg, opts.tzName)
		default:
			return fmt.Sprintf("next time: jot timestamp %s", arg)
		}
	default:
		arg := value
		if strings.ContainsAny(arg, " \t\"'") {
			arg = strconv.Quote(arg)
		}
		switch {
		case opts.useUTC:
			return fmt.Sprintf("next time: jot timestamp %s --utc", arg)
		case opts.tzName != "":
			return fmt.Sprintf("next time: jot timestamp %s --tz %s", arg, opts.tzName)
		default:
			return fmt.Sprintf("next time: jot timestamp %s", arg)
		}
	}
}
