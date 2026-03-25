package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixedTimestampNow() func() time.Time {
	return func() time.Time {
		return time.Date(2025, 3, 25, 12, 34, 56, 0, time.UTC)
	}
}

func TestJotTimestampFromReaderDefaultsToDualOutput(t *testing.T) {
	var out bytes.Buffer
	err := jotTimestampFromReader(strings.NewReader(""), &out, []string{"1710000000", "--utc"}, fixedTimestampNow())
	if err != nil {
		t.Fatalf("jotTimestampFromReader returned error: %v", err)
	}

	wantHuman := time.Unix(1710000000, 0).In(time.UTC).Format(timestampDefaultLayout)
	got := out.String()
	for _, snippet := range []string{
		"Unix: 1710000000",
		"Human: " + wantHuman,
		"Timezone: UTC",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
}

func TestJotTimestampFromReaderSupportsMillisecondsAndHumanOnly(t *testing.T) {
	var out bytes.Buffer
	err := jotTimestampFromReader(strings.NewReader(""), &out, []string{"1710000000000", "--ms", "--human", "--utc"}, fixedTimestampNow())
	if err != nil {
		t.Fatalf("jotTimestampFromReader returned error: %v", err)
	}

	wantHuman := time.Unix(0, 1710000000000*int64(time.Millisecond)).In(time.UTC).Format(timestampDefaultLayout)
	got := out.String()
	for _, snippet := range []string{
		"Human: " + wantHuman,
		"Timezone: UTC",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
	if strings.Contains(got, "Unix:") {
		t.Fatalf("expected human-only output, got %q", got)
	}
}

func TestJotTimestampFromReaderHonorsCustomFormat(t *testing.T) {
	var out bytes.Buffer
	err := jotTimestampFromReader(strings.NewReader(""), &out, []string{"1710000000", "--utc", "--format", "2006/01/02 15:04"}, fixedTimestampNow())
	if err != nil {
		t.Fatalf("jotTimestampFromReader returned error: %v", err)
	}

	wantHuman := time.Unix(1710000000, 0).In(time.UTC).Format("2006/01/02 15:04")
	got := out.String()
	if !strings.Contains(got, "Human: "+wantHuman) {
		t.Fatalf("expected custom format output, got %q", got)
	}
}

func TestJotTimestampFromReaderParsesHumanInputInTimezone(t *testing.T) {
	var out bytes.Buffer
	err := jotTimestampFromReader(strings.NewReader(""), &out, []string{"2025-03-25 12:00:00", "--tz", "Europe/London"}, fixedTimestampNow())
	if err != nil {
		t.Fatalf("jotTimestampFromReader returned error: %v", err)
	}

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location failed: %v", err)
	}
	wantUnix := time.Date(2025, 3, 25, 12, 0, 0, 0, loc).Unix()
	wantHuman := time.Date(2025, 3, 25, 12, 0, 0, 0, loc).Format(timestampDefaultLayout)
	got := out.String()
	for _, snippet := range []string{
		"Unix: " + strconv.FormatInt(wantUnix, 10),
		"Human: " + wantHuman,
		"Timezone: Europe/London",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
}

func TestJotTimestampFromReaderUsesStdinInput(t *testing.T) {
	var out bytes.Buffer
	err := jotTimestampFromReader(strings.NewReader("1710000000\n"), &out, []string{"--stdin", "--utc", "--unix"}, fixedTimestampNow())
	if err != nil {
		t.Fatalf("jotTimestampFromReader returned error: %v", err)
	}

	want := "Unix: 1710000000\n"
	if out.String() != want {
		t.Fatalf("expected %q, got %q", want, out.String())
	}
}

func TestJotTimestampFromReaderRejectsConflictsAndMissingInput(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		stdin   string
		wantErr string
	}{
		{name: "missing input", args: nil, wantErr: "timestamp: a timestamp value"},
		{name: "numeric unit conflict", args: []string{"1710000000", "--ms", "--seconds"}, wantErr: "timestamp: choose one numeric unit"},
		{name: "output mode conflict", args: []string{"1710000000", "--unix", "--human"}, wantErr: "timestamp: choose one output mode"},
		{name: "timezone conflict", args: []string{"1710000000", "--utc", "--tz", "UTC"}, wantErr: "timestamp: choose one timezone source"},
		{name: "unknown timezone", args: []string{"1710000000", "--tz", "No/Such_Zone"}, wantErr: "timestamp: unknown timezone"},
		{name: "invalid human input", args: []string{"not-a-time"}, wantErr: "timestamp: unsupported date format"},
		{name: "stdin empty", args: []string{"--stdin"}, wantErr: "timestamp: stdin input is empty"},
		{name: "unit flag with human input", args: []string{"2025-03-25 12:00:00", "--ms"}, wantErr: "timestamp: --ms and --seconds apply to numeric Unix input only"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if tc.stdin == "" {
				tc.stdin = ""
			}
			err := jotTimestampFromReader(strings.NewReader(tc.stdin), &out, tc.args, fixedTimestampNow())
			if err == nil {
				t.Fatalf("expected error, got output %q", out.String())
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRenderTimestampHelpIncludesUsageAndFlags(t *testing.T) {
	help := renderTimestampHelp(false)
	for _, snippet := range []string{
		"jot timestamp",
		"jot task timestamp",
		"--ms",
		"--utc",
		"--format <layout>",
		"jot timestamp now --utc",
	} {
		if !strings.Contains(help, snippet) {
			t.Fatalf("expected help to contain %q, got %q", snippet, help)
		}
	}
}

func TestRunTimestampTaskGuidesUnixConversion(t *testing.T) {
	var out bytes.Buffer
	input := strings.NewReader("1\n1710000000\nUTC\n")
	err := runTimestampTask(input, &out, ".", fixedTimestampNow())
	if err != nil {
		t.Fatalf("runTimestampTask returned error: %v", err)
	}

	wantHuman := time.Unix(1710000000, 0).In(time.UTC).Format(timestampDefaultLayout)
	got := out.String()
	for _, snippet := range []string{
		"Timestamp",
		"Unix to human",
		"Unix: 1710000000",
		"Human: " + wantHuman,
		"Timezone: UTC",
		"next time: jot timestamp 1710000000 --utc",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
}

func TestRunTimestampTaskGuidesHumanConversion(t *testing.T) {
	var out bytes.Buffer
	input := strings.NewReader("2\n2025-03-25 12:00:00\nEurope/London\n")
	err := runTimestampTask(input, &out, ".", fixedTimestampNow())
	if err != nil {
		t.Fatalf("runTimestampTask returned error: %v", err)
	}

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load location failed: %v", err)
	}
	wantUnix := time.Date(2025, 3, 25, 12, 0, 0, 0, loc).Unix()
	wantHuman := time.Date(2025, 3, 25, 12, 0, 0, 0, loc).Format(timestampDefaultLayout)
	got := out.String()
	for _, snippet := range []string{
		"Human to Unix",
		"Unix: " + strconv.FormatInt(wantUnix, 10),
		"Human: " + wantHuman,
		"Timezone: Europe/London",
		"next time: jot timestamp \"2025-03-25 12:00:00\" --tz Europe/London",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
}

func TestRunTimestampTaskGuidesNow(t *testing.T) {
	var out bytes.Buffer
	input := strings.NewReader("3\nUTC\n")
	err := runTimestampTask(input, &out, ".", fixedTimestampNow())
	if err != nil {
		t.Fatalf("runTimestampTask returned error: %v", err)
	}

	got := out.String()
	for _, snippet := range []string{
		"Now",
		"Unix: 1742906096",
		"Human: 2025-03-25 12:34:56",
		"Timezone: UTC",
		"next time: jot timestamp now --utc",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, got)
		}
	}
}
