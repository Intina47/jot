# `jot task encode`

## Goal

`jot task encode` should make base64 work feel like a normal local terminal utility instead of something users reach for a browser or ad hoc script to do.

The v1 scope is intentionally narrow:

- base64 encode file contents or inline strings
- base64 decode file contents or inline strings
- keep the direct command short enough to become the default habit
- use `jot task` as the guided on-ramp for people who do not remember the flags yet

The feature should stay terminal-first, local-only, and predictable.

## Primary Direct-Command UX

The direct command should be the fastest path once the user knows the job:

```bash
jot encode --text "hello world"
jot encode --text "aGVsbG8gd29ybGQ=" --decode
jot encode secret.txt
jot encode secret.txt --decode
jot encode payload.bin --out payload.b64.txt
```

Recommended command shape:

- `jot encode <path>` for file input
- `jot encode --text <value>` for inline string input
- `jot encode --stdin` for piped content and shell composition
- `--decode` switches the direction from encode to decode

Default behavior:

- Default mode is encode.
- Inline text defaults to stdout.
- Stdin defaults to stdout.
- File input defaults to writing a sibling output file.
- File input never overwrites the source unless the user explicitly asks for it.

The command should print compact, useful output:

- stdout only when stdout is the chosen destination
- otherwise a short success summary with source, mode, and output path
- for batch or scripted usage, `--quiet` should suppress extra summary text

The direct command should remain more important than the guided flow. `jot task encode` exists to teach and discover the feature, not to replace the normal CLI path.

## `jot task` Guided UX

`jot task` should surface `encode` as another focused task entry in the existing task shell.

The guided flow should stay small:

1. Ask whether the user wants to work with a file or inline text.
2. Ask whether the operation is `encode` or `decode`.
3. If file-based, pick a file from the current folder using the existing terminal task flow.
4. If text-based, accept pasted inline content in the current terminal UI.
5. Ask where the result should go only when it is not obvious from the defaults.
6. Run the operation.
7. Show the equivalent direct command after success.

Guided flow defaults:

- file input should default to writing a sibling output file
- inline text should default to stdout or an in-terminal result view
- decode mode should warn clearly when the output may be binary and suggest a file destination

The guided flow should not introduce:

- browser pickers
- web previews
- remote storage
- a different task UI model from the current `jot task` direction

## Inputs

v1 inputs:

- a single file path
- inline text via `--text`
- stdin via `--stdin`

The spec should treat file input as raw bytes. Base64 is a byte transform first, not a text formatter. Files may be binary; the codec should not try to interpret them as text before encoding or after decoding.

Inline text rules:

- `--text` input is interpreted as literal bytes from the CLI string
- for encode mode, the string is encoded exactly as provided
- for decode mode, the string must be valid base64 after whitespace normalization rules are applied

Stdin rules:

- stdin should read all bytes until EOF
- stdin is valid for both encode and decode
- `--stdin` should be explicit in non-piped interactive sessions so the command does not appear to hang unexpectedly

Base64 acceptance rules for decode:

- accept standard base64 with `=` padding
- accept content split across lines
- trim surrounding whitespace
- reject invalid alphabet characters and malformed padding with a clear error

Non-goal for v1 input scope:

- no directory or recursive batch mode
- no data URL parsing
- no URL-safe base64 variant in the first pass unless added explicitly with a dedicated flag later

## Outputs

Output rules should be predictable and mode-aware.

For inline text:

- encode mode defaults to stdout
- decode mode defaults to stdout when the decoded bytes are valid UTF-8 text
- decode mode should require `--out` or `--stdout --force-text` if the decoded bytes are not valid UTF-8

For stdin:

- same defaults as inline text
- if the decoded result is binary, prefer a clear error that tells the user to add `--out <path>`

For file input:

- encode mode writes a sibling text file by default
- decode mode writes a sibling decoded file by default
- the command prints the output path after success

Suggested default output naming:

- `secret.txt` encode -> `secret.txt.b64.txt`
- `secret.txt.b64.txt` decode -> `secret.txt`
- `archive.tar` encode -> `archive.tar.b64.txt`
- `archive.tar.b64.txt` decode -> `archive.tar`
- `payload` encode -> `payload.b64.txt`
- `payload.b64` decode -> `payload`

Output naming rules:

- if the source file ends in `.b64.txt`, decode removes that suffix first
- otherwise, if the source ends in `.b64`, decode removes that suffix
- otherwise, decode writes `<name>.decoded`
- encode always appends `.b64.txt` unless `--out` is provided

These rules avoid guessing original binary extensions when the encoded file name does not carry enough information.

## Flags and Options

Recommended flags:

- `--decode` to decode instead of encode
- `--text <value>` for inline input
- `--stdin` to read from stdin
- `--out <path>` to choose an explicit output path
- `--stdout` to force stdout output
- `--overwrite` to allow replacing an existing output file
- `--quiet` to suppress summary text when writing files
- `--force-text` to print decoded bytes to stdout even when they are not valid UTF-8

Behavior expectations:

- exactly one input source should be allowed: path, `--text`, or `--stdin`
- `--stdout` and `--out` are mutually exclusive
- `--overwrite` only matters when writing a file
- `--force-text` only matters for decode-to-stdout cases

Intentionally not in v1:

- `--urlsafe`
- `--wrap`
- `--no-padding`
- directory recursion
- clipboard flags

The first version should solve the common local encode/decode jobs before adding format variants.

## Error Handling

The command should fail clearly and early on:

- no input provided
- more than one input source provided
- missing file
- unreadable file
- invalid base64 during decode
- output collision when overwrite is not allowed
- trying to decode binary bytes to stdout without `--force-text`
- trying to use `--stdout` and `--out` together

Error messages should be task-oriented and specific.

Examples of expected guidance:

- `decode failed: input is not valid base64`
- `output already exists: payload.bin`
- `decoded output is binary; use --out <path> to write a file`
- `choose one input source: <path>, --text, or --stdin`

Decode failures should not produce partial output files. Write through a temporary file and move into place only after decode succeeds.

## Non-Goals

This task should not expand into a generic encoding suite.

Out of scope for v1:

- hex, base32, base85, quoted-printable, or URL encoding
- JWT inspection
- data URL parsing and generation
- URL-safe base64 variants
- line wrapping controls for email or PEM-style output
- directory-wide encode/decode jobs
- clipboard integrations
- browser-based tools

The goal is a dependable base64 utility, not a kitchen-sink codec tool.

## Implementation Notes

- Use a real base64 encoder/decoder from the standard library.
- Treat file inputs as byte streams, not text files.
- For encode mode, file output should be ASCII text with no extra prose in the file body.
- For decode mode, preserve raw bytes exactly.
- For stdout decode, detect valid UTF-8 before printing unless `--force-text` is present.
- Keep output naming logic centralized so direct command and guided task flows stay identical.
- Keep the task entry aligned with the existing `jot task` shell:
  - small guided flow
  - local file selection from the current folder
  - direct command hint after success
- Do not change unrelated `jot task` styling or navigation to accommodate this task.

Practical implementation split:

- input resolution layer for path vs `--text` vs `--stdin`
- transform layer for encode/decode
- output resolution layer for stdout vs explicit file vs default sibling file
- task-flow adapter that gathers the same inputs and then calls the same command implementation

This keeps the guided flow thin and makes the direct command the real source of truth.

## Test Plan

Minimum coverage should include both direct command behavior and guided task flow behavior.

CLI cases:

- encode inline text to stdout
- decode inline text to stdout
- encode stdin to stdout
- decode stdin to stdout for valid UTF-8 text
- encode a file and write the expected sibling `.b64.txt` output
- decode a `.b64.txt` file and restore the expected sibling output name
- decode a `.b64` file and remove the suffix
- decode a file without a `.b64` suffix and write `<name>.decoded`
- respect `--out` for encode
- respect `--out` for decode
- refuse to overwrite an existing file without `--overwrite`
- allow overwrite with `--overwrite`
- reject invalid base64 with a clear error
- reject conflicting input flags
- reject `--stdout` together with `--out`
- reject binary decode to stdout without `--force-text`
- allow binary decode to stdout with `--force-text`

Guided task cases:

- `jot task encode` offers file vs text input in the current task UI
- guided encode-from-file uses the same default sibling naming as the direct command
- guided decode-from-text shows the direct command hint after success
- guided decode of binary output steers the user toward a file destination instead of dumping unreadable bytes blindly

The tests should verify that the task remains terminal-first and that the direct command stays the primary reusable path after the first run.
