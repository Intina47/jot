package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderUUIDHelpContainsGuidance(t *testing.T) {
	help := renderUUIDHelp(false)
	for _, snippet := range []string{
		"jot uuid",
		"jot task uuid",
		"--type uuid|nanoid|string",
		"--count N",
		"--alphabet TEXT",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestJotUUIDDefaultsToSingleUUID(t *testing.T) {
	var out bytes.Buffer
	if err := jotUUID(&out, nil); err != nil {
		t.Fatalf("jotUUID returned error: %v", err)
	}

	lines := filteredLines(out.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d (%q)", len(lines), out.String())
	}
	if !looksLikeUUIDv4(lines[0]) {
		t.Fatalf("expected UUID v4, got %q", lines[0])
	}
}

func TestJotUUIDNanoidCountAndLength(t *testing.T) {
	var out bytes.Buffer
	if err := jotUUID(&out, []string{"--type", "nanoid", "--count", "3", "--length", "12"}); err != nil {
		t.Fatalf("jotUUID returned error: %v", err)
	}

	lines := filteredLines(out.String())
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d (%q)", len(lines), out.String())
	}
	for _, line := range lines {
		if len(line) != 12 {
			t.Fatalf("expected length 12, got %d for %q", len(line), line)
		}
	}
}

func TestJotUUIDStringAlphabetAndCase(t *testing.T) {
	var out bytes.Buffer
	if err := jotUUID(&out, []string{"--type", "string", "--count", "2", "--length", "8", "--alphabet", "ab12", "--upper"}); err != nil {
		t.Fatalf("jotUUID returned error: %v", err)
	}

	lines := filteredLines(out.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d (%q)", len(lines), out.String())
	}
	for _, line := range lines {
		if len(line) != 8 {
			t.Fatalf("expected length 8, got %d for %q", len(line), line)
		}
		if line != strings.ToUpper(line) {
			t.Fatalf("expected uppercase output, got %q", line)
		}
		if strings.Trim(line, "AB12") != "" {
			t.Fatalf("expected only characters from alphabet, got %q", line)
		}
	}
}

func TestJotUUIDHelpFlagReturnsHelp(t *testing.T) {
	var out bytes.Buffer
	if err := jotUUID(&out, []string{"--help"}); err != nil {
		t.Fatalf("jotUUID returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "jot uuid") {
		t.Fatalf("expected help output, got %q", got)
	}
}

func TestJotUUIDRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "bad type", args: []string{"--type", "weird"}, want: "unsupported type"},
		{name: "bad count", args: []string{"--count", "0"}, want: "count must be at least 1"},
		{name: "bad length", args: []string{"--type", "string", "--length", "-1"}, want: "length must be at least 1"},
		{name: "upper and lower", args: []string{"--upper", "--lower"}, want: "choose only one"},
		{name: "irrelevant length for uuid", args: []string{"--length", "10"}, want: "--length only applies"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := jotUUID(&out, tc.args)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRunUUIDTaskGuidedFlow(t *testing.T) {
	var out bytes.Buffer
	input := strings.NewReader("\n\n")
	if err := runUUIDTask(input, &out, t.TempDir()); err != nil {
		t.Fatalf("runUUIDTask returned error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "UUID") {
		t.Fatalf("expected task header, got %q", text)
	}
	if !strings.Contains(text, "next time: jot uuid") {
		t.Fatalf("expected direct-command tip, got %q", text)
	}

	lines := filteredLines(text)
	var values []string
	for _, line := range lines {
		if looksLikeUUIDv4(line) {
			values = append(values, line)
		}
	}
	if len(values) != 1 {
		t.Fatalf("expected one UUID value in output, got %d (%q)", len(values), text)
	}
}

func filteredLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	var lines []string
	for _, line := range raw {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func stripANSI(text string) string {
	var out strings.Builder
	inEscape := false
	for i := 0; i < len(text); i++ {
		switch {
		case inEscape:
			if text[i] >= '@' && text[i] <= '~' {
				inEscape = false
			}
		case text[i] == 0x1b:
			inEscape = true
		default:
			out.WriteByte(text[i])
		}
	}
	return out.String()
}

func looksLikeUUIDv4(value string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "-")
	if len(parts) != 5 {
		return false
	}
	lengths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != lengths[i] {
			return false
		}
		for _, r := range part {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	if parts[2][0] != '4' {
		return false
	}
	switch parts[3][0] {
	case '8', '9', 'a', 'b':
		return true
	default:
		return false
	}
}
