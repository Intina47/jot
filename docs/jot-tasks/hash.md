# `jot task hash`

## Goal

`jot task hash` should make file and string hashing feel like a normal terminal utility that a developer reaches for without thinking.

The v1 scope should cover:

- computing common digests for files, text, and stdin
- verifying a file or text payload against an expected digest
- using `jot task` as the discovery layer for people who do not remember the exact algorithm flags yet

The feature should stay terminal-first, local-only, and predictable. It should not become a broad checksum management suite.

## Primary Direct-Command UX

The direct command should be the shortest path once the user knows the job:

```bash
jot hash package.zip
jot hash package.zip --algo sha1
jot hash --text "hello world" --algo sha256
jot hash --stdin --algo sha512
jot hash package.zip --verify "SHA256: 3a7bd3e2360a3d80..."
```

Recommended command shape:

- `jot hash <path>` for file input
- `jot hash --text <value>` for inline string input
- `jot hash --stdin` for piped content and shell composition
- `--algo` selects the hash algorithm
- `--verify` switches the command into verification mode

Default behavior:

- Default algorithm should be `sha256`
- Default mode should be compute, not verify
- File input should print a compact one-line digest summary to stdout by default
- Inline text and stdin should also print to stdout by default
- If the user asks for verification, the command should return success or failure instead of a digest-only result

Suggested default output format:

```text
SHA256  package.zip  7c3f7e...
```

The direct command should remain the primary path. `jot task hash` exists to teach and discover the feature, not replace the normal CLI path.

## `jot task` Guided UX

`jot task` should surface `hash` as a focused task entry in the existing task shell.

The guided flow should stay small:

1. Ask whether the user wants to compute a hash or verify one.
2. Ask for the input source: file, inline text, or stdin if the current UI can support it cleanly.
3. If file-based, pick a file from the current folder using the existing terminal task flow.
4. Ask for the algorithm if it is not obvious from the default.
5. If verifying, ask for the expected digest to compare against.
6. Run the operation.
7. Show the equivalent direct command after success.

Guided flow defaults:

- file input should default to `sha256`
- text input should default to `sha256`
- verify mode should default to a single expected digest first, not a manifest workflow
- the task should not introduce a different UI model from the current `jot task` direction

The guided flow should not introduce:

- browser pickers
- upload flows
- remote verification services
- a new navigation pattern outside the current `jot task` shell

## Inputs

v1 inputs:

- a single file path
- inline text via `--text`
- stdin via `--stdin`

Supported algorithms in v1:

- `md5`
- `sha1`
- `sha256`
- `sha512`

The spec should treat file input as raw bytes. Hashing is a byte transform first, not a text formatter.

Inline text rules:

- `--text` input is interpreted as literal bytes from the CLI string
- hashing should use the exact bytes provided by the terminal input
- verification with `--text` should compare the computed digest of that string against the expected digest

Stdin rules:

- stdin should read all bytes until EOF
- stdin is valid for both compute and verify modes
- `--stdin` should be explicit in interactive sessions so the command does not appear to hang unexpectedly

Verification input rules:

- `--verify` should accept a single expected digest string in the form `ALGO: HEXDIGEST`
- the algorithm prefix should be optional if `--algo` is already set
- hex input should ignore surrounding whitespace
- verification should be case-insensitive for the hex digest
- if the algorithm in the expected digest does not match the requested algorithm, the command should fail clearly

Non-goal for v1 input scope:

- no directory recursion
- no checksum manifest parsing beyond a single expected digest string
- no URL, clipboard, or browser-driven input

## Outputs

Output rules should be predictable and mode-aware.

Compute mode:

- print a single digest line to stdout by default
- if the user chooses a file output path, write the digest text there
- if the user requests multiple algorithms, print one line per algorithm in a stable order

Suggested compute output format:

```text
SHA256  package.zip  7c3f7e2360a3d80f...
```

If multiple algorithms are requested:

```text
MD5     package.zip  9e107d9d372bb682...
SHA1    package.zip  2fd4e1c67a2d28fc...
SHA256  package.zip  7c3f7e2360a3d80f...
```

Verification mode:

- success should print a compact `verified` message or stay quiet with `--quiet`
- failure should print a clear mismatch message and return a non-zero exit code
- verification should not emit a digest-only result unless the user also asked to compute one

Suggested verification summary:

```text
verified SHA256 package.zip
```

or on failure:

```text
verification failed: expected SHA256 7c3f7e..., got 3a7bd3...
```

If the command is writing to a file:

- default output name for compute mode should be `<name>.<algo>.hash.txt`
- `package.zip` with `sha256` should become `package.zip.sha256.hash.txt`
- explicit `--out` should always win over the default name

## Flags and Options

Recommended flags:

- `--algo <name>` to choose the algorithm
- `--text <value>` for inline input
- `--stdin` to read from stdin
- `--out <path>` to choose an explicit output path
- `--overwrite` to allow replacing an existing digest output file
- `--verify <digest>` to compare the computed hash against an expected digest
- `--quiet` to suppress summary text when writing files or when verification succeeds
- `--all` to compute all supported algorithms in one pass

Behavior expectations:

- exactly one input source should be allowed: path, `--text`, or `--stdin`
- `--out` should only matter when writing a digest file instead of stdout
- `--overwrite` should only matter when the command is writing a digest file
- `--verify` should switch the command into verification mode
- `--all` should be compatible with file, text, and stdin input
- `--algo` should be required only when `--all` is not used and the default `sha256` is not sufficient

Intentionally not in v1:

- HMAC
- password hashing
- salt generation
- checksum manifest globbing
- recursive folder hashing
- output formatting knobs beyond the stable default line format

The first version should solve common developer hash and verify jobs before adding checksum management features.

## Error Handling

The command should fail clearly and early on:

- no input provided
- more than one input source provided
- missing file
- unreadable file
- unsupported algorithm
- malformed expected digest
- mismatch during verification
- output collision when overwrite is not allowed
- trying to use `--verify` without a digest
- trying to use conflicting input flags

Error messages should be task-oriented and specific.

Examples of expected guidance:

- `hash failed: unsupported algorithm "sha224"`
- `verification failed: expected SHA256 7c3f..., got 3a7b...`
- `choose one input source: <path>, --text, or --stdin`
- `output already exists: package.zip.sha256.hash.txt`

Verification failures should not produce partial output files. Write through a temporary file and move into place only after the operation succeeds.

## Non-Goals

This task should not expand into a generic cryptography suite.

Out of scope for v1:

- HMAC or keyed hashing
- password storage or password verification
- salt generation
- checksum manifest parsing for entire directories
- recursive folder hashing
- content signing or encryption
- browser-based tools

The goal is a dependable hash utility, not a crypto toolbox.

## Implementation Notes

- Use standard library hash implementations for `md5`, `sha1`, `sha256`, and `sha512`.
- Treat file inputs as byte streams, not text files.
- Keep algorithm selection centralized so direct command and guided task flows stay identical.
- Make the verify path share the same digest computation code as the compute path.
- Keep output formatting stable so users can paste digests into docs, scripts, or checksum files.
- Keep the task entry aligned with the existing `jot task` shell:
  - small guided flow
  - local file selection from the current folder
  - direct command hint after success
- Do not change unrelated `jot task` styling or navigation to accommodate this task.

Practical implementation split:

- input resolution layer for path vs `--text` vs `--stdin`
- digest computation layer for algorithm selection
- verification layer that normalizes expected digests and compares them safely
- output resolution layer for stdout vs explicit file vs default sibling file
- task-flow adapter that gathers the same inputs and then calls the same command implementation

This keeps the guided flow thin and makes the direct command the source of truth.

## Test Plan

Minimum coverage should include both direct command behavior and guided task flow behavior.

CLI cases:

- hash a file with the default algorithm and confirm the output format
- hash a file with `--algo sha1`, `sha256`, and `sha512`
- hash inline text to stdout
- hash stdin to stdout
- compute all supported algorithms with `--all`
- write a digest file with the expected default output name
- respect `--out` for compute mode
- refuse to overwrite an existing file without `--overwrite`
- allow overwrite with `--overwrite`
- verify a file against a matching expected digest
- fail verification on a mismatched digest
- reject malformed expected digest input
- reject unsupported algorithms
- reject conflicting input flags

Guided task cases:

- `jot task hash` offers compute vs verify in the current task UI
- guided file hashing uses the same default algorithm and output formatting as the direct command
- guided verification shows the direct command hint after success or failure
- guided hash verification steers the user toward a clean expected-digest input instead of inventing a checksum-management workflow

The tests should verify that the task remains terminal-first and that the direct command stays the primary reusable path after the first run.
