# jot task minify

## Goal

`jot task minify` should make JSON cleanup a one-step terminal action.

The task should support two common workflows:

- shrink a JSON file for transport, fixtures, config snapshots, or payloads
- pretty-print JSON so it is readable again after minification or minified output from other tools

The core UX should stay terminal-first and local-only. No browser detours, no upload flows, no remote processing.

## Primary Direct-Command UX

The direct command should be the habit after first use:

```bash
jot minify data.json
jot minify data.json --pretty
jot minify --text '{"name":"jot"}'
jot minify --text '{"name":"jot"}' --pretty
```

Recommended behavior:

- If the input is a file, read it from disk and write the transformed content back to a new file by default.
- If the input is inline text, emit the transformed result to stdout by default.
- Default mode should be minify, because that is the common "make it smaller" case.
- Pretty-print should be opt-in and explicit.

Suggested output naming:

- `data.json` -> `data.min.json` for minify
- `data.min.json` -> `data.pretty.json` or `data.format.json` for pretty-print if writing to disk
- stdout is fine for inline text or `--stdout`

## `jot task` Guided UX

`jot task` should surface `minify` as a focused task entry.

The guided path should:

1. Ask whether the user wants to work on a file or paste inline JSON.
2. If file-based, pick a file from the current folder first, with the current terminal UI and no browser/file-picker detour.
3. Ask for mode if it is not obvious from context:
   - `minify`
   - `pretty-print`
4. Run the transform.
5. Show the equivalent direct command as the next-step hint.

The guided path should not become a dead-end wizard. It should teach the direct command immediately after success.

## Inputs

- JSON files, typically `.json`
- Inline JSON text passed on the command line
- Piped stdin for scripting and editor integration

Accepted content should be valid JSON only. This task is not a generic formatter for arbitrary text.

## Outputs

- Minified JSON with insignificant whitespace removed
- Pretty-printed JSON with stable indentation
- Files written next to the original when the user chooses file output
- Stdout output for inline and piped input

The output should preserve semantic content exactly. Only whitespace and formatting should change.

## Flags and Options

Recommended flags:

- `--pretty` to pretty-print instead of minify
- `--text <json>` to accept inline JSON
- `--stdin` to read from stdin
- `--out <path>` to choose an explicit output path
- `--overwrite` to allow replacing an existing output file
- `--stdout` to force stdout output
- `--indent <n>` for pretty-print indentation, defaulting to the current Jot style

Possible convenience aliases:

- `jot minify` = minify mode
- `jot pretty` could be a future alias, but only if it does not fragment the UX

## Error Handling

The task should fail clearly and locally when input is invalid.

Expected errors:

- invalid JSON syntax
- missing file
- unreadable file
- output path collision without overwrite permission
- stdin requested but no data is present

Error messages should point to the exact problem and avoid dumping raw parser internals unless they are useful.

If the user asks for pretty-printing invalid JSON, the command should fail rather than guessing.

## Non-Goals

- Not a generic code formatter
- Not a YAML, TOML, XML, or HTML formatter in this task
- Not a browser-based beautifier
- Not a networked pastebin or remote transform service
- Not a replacement for dedicated formatters like `jq`, only a faster local first-step for common JSON cleanup

## Implementation Notes

- Use a real JSON parser/encoder instead of string replacement.
- Preserve key order as parsed if the implementation naturally does so; do not reorder keys unless that is a deliberate, documented choice.
- Use stable indentation for pretty-print mode so diffs stay predictable.
- For file inputs, prefer writing a sibling output file unless the user explicitly chooses overwrite.
- For inline or stdin input, keep stdout as the default path so the command composes well in pipes.
- Keep this task separate from any broader "format anything" ambitions. Its scope should stay clearly JSON-focused.
- Preserve the current `jot task` UI language and styling. This feature should slot into the existing discovery flow, not replace it.

## Test Plan

- Minify a small JSON file and confirm output removes insignificant whitespace.
- Pretty-print the same JSON and confirm indentation is stable.
- Verify inline `--text` input works for both modes.
- Verify stdin input works for both modes.
- Verify invalid JSON returns a clear failure.
- Verify missing-file and permission failures are surfaced cleanly.
- Verify output naming does not overwrite the source unless explicitly allowed.
- Verify `jot task minify` shows the same direct command guidance after success.
