# jot task timestamp

## Goal

`jot task timestamp` should make Unix timestamp conversion a fast terminal action.

The task should cover the two everyday workflows developers use:

- convert Unix timestamps into human-readable dates
- convert human-readable dates back into Unix timestamps

The feature should stay local, predictable, and terminal-first. It should not introduce a browser dependency or a calendar-style UI.

## Primary Direct-Command UX

The direct command should be short enough to remember:

```bash
jot timestamp 1710000000
jot timestamp "2025-03-24 12:00:00"
jot timestamp "2025-03-24 12:00:00" --tz Europe/London
jot timestamp now
```

Recommended behavior:

- Numeric input should be treated as Unix time by default.
- Human-readable input should be parsed into Unix time when the value is not a plain number.
- `now` should emit the current Unix timestamp and a human-readable rendering.
- The default human-readable output should be clearly formatted and include the timezone used.

Recommended output style:

- Unix to human: print both the original epoch value and the interpreted date/time
- Human to unix: print both the parsed Unix timestamp and the normalized date/time
- If the user asks for one direction explicitly, keep the output focused but still include enough context to avoid ambiguity

## `jot task` Guided UX

`jot task` should expose `timestamp` as a guided conversion task.

The guided path should:

1. Ask whether the user wants to convert from Unix time or from a human date/time.
2. Prompt for the value.
3. Ask for timezone only when it matters or when the user wants a non-default timezone.
4. Show the conversion result in both forms where useful.
5. Show the equivalent direct command as the next-step hint.

The guided path should remain a helper, not a replacement for the direct command.

## Inputs

- Unix timestamps in seconds
- Unix timestamps in milliseconds if explicitly requested or detectable
- Human-readable date/time strings
- `now` for current time
- Optional stdin input for scripting

Accepted human-readable input should be intentionally broad but deterministic. Common formats should be supported without requiring the user to pre-format them.

## Outputs

- Human-readable dates from Unix timestamps
- Unix timestamps from human-readable dates
- Optional dual-format output that shows both forms for verification
- Stable timezone-aware rendering so results are easy to compare

When the input is ambiguous, the output should state the assumption instead of silently guessing.

## Flags and Options

Recommended flags:

- `--tz <zone>` to choose a timezone
- `--utc` as a shorthand for UTC output
- `--ms` to treat numeric input as milliseconds
- `--seconds` to force seconds
- `--format <layout>` to control human-readable formatting
- `--unix` to force Unix output
- `--human` to force human-readable output
- `--stdin` to read the value from stdin

Recommended defaults:

- seconds for numeric Unix input unless `--ms` is provided
- the local timezone when none is specified
- dual-form output for `now` so the user can see both representations immediately

## Timezone Handling

Timezone handling should be explicit and boring.

Rules:

- If the user passes `--utc`, use UTC regardless of local settings.
- If the user passes `--tz`, respect that timezone.
- If no timezone is provided, use the local timezone from the system.
- The output should always say which timezone was used.

The task should not silently reinterpret the input in a different timezone than the user asked for.

## Error Handling

The task should fail clearly when the input cannot be interpreted.

Expected errors:

- invalid timestamp value
- unsupported date format
- unknown timezone
- empty stdin
- ambiguous input that cannot be safely guessed

If a human date is missing timezone context, the task should either use the explicit default timezone or explain the assumption.

## Non-Goals

- Not a calendar app
- Not a timezone database editor
- Not an RFC-style date parsing library wrapper exposed directly to users
- Not a scheduling system or reminder engine
- Not a browser-based time converter

## Implementation Notes

- Use a real date/time parser and timezone library instead of string heuristics.
- Keep the output stable and explicit so conversions can be copied into logs, commit messages, and issue comments.
- Prefer seconds as the default Unix unit because that is the common developer expectation.
- Make the `now` path especially clear so users can quickly capture current time in both representations.
- Preserve the current `jot task` UI direction and styling. This task should slot into the existing guided command flow.
- If the implementation can detect millisecond Unix input, it should do so only when that behavior is documented and unambiguous.

## Test Plan

- Convert a Unix timestamp to human-readable output.
- Convert a human-readable date/time back to Unix output.
- Verify `now` returns both forms.
- Verify timezone selection with `--tz`.
- Verify `--utc` overrides local timezone.
- Verify millisecond handling works when requested.
- Verify invalid timestamps and unknown timezones fail clearly.
- Verify `jot task timestamp` prompts for direction and shows the direct command hint afterward.
